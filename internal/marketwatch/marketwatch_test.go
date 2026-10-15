package marketwatch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saudm6/dfm-be/internal/bus"
	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/dfm"
	"github.com/saudm6/dfm-be/internal/dfm/dfmtest"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/instruments"
	"github.com/saudm6/dfm-be/internal/marketwatch"
	"github.com/saudm6/dfm-be/internal/store/storetest"
)

var dfmHours = calendar.Hours{
	PreOpen:        9*60 + 30,
	Open:           10 * 60,
	ClosingAuction: 14*60 + 45,
	Close:          15 * 60,
	Week:           []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
}

const preOpenLead = 30 * time.Minute

// dubai returns a time on 2026-10-<day> in Dubai; the 15th is a Thursday.
func dubai(day, hour, minute, second int) time.Time {
	return time.Date(2026, time.October, day, hour, minute, second, 0, domain.DubaiTZ)
}

type fixedCalendar struct{ cal *calendar.Calendar }

func (f fixedCalendar) Load(context.Context) (*calendar.Calendar, error) { return f.cal, nil }

func newCalendar(t *testing.T, closed ...time.Time) fixedCalendar {
	t.Helper()
	cal, err := calendar.New(dfmHours, closed)
	if err != nil {
		t.Fatal(err)
	}
	return fixedCalendar{cal}
}

// master is a security master source of fixed instruments.
type master []domain.Instrument

func (m master) SecurityMaster(context.Context, time.Time) (dfm.SecurityMaster, error) {
	return dfm.SecurityMaster{Instruments: m}, nil
}

// recordedInstruments returns the security master `dfm sync instruments`
// stored from the live API right after the market watch was recorded,
// trimmed to the columns a sync needs.
func recordedInstruments(t *testing.T) []domain.Instrument {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dfmtest.MarketWatchDir(), "instruments.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ins []domain.Instrument
	if err := json.Unmarshal(body, &ins); err != nil {
		t.Fatal(err)
	}
	return ins
}

func seed(t *testing.T, pool *pgxpool.Pool, ins []domain.Instrument) {
	t.Helper()
	if _, err := instruments.Sync(t.Context(), pool, master(ins), dubai(15, 9, 0, 0), slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
}

func instrument(sym domain.Symbol, class domain.InstrumentClass) domain.Instrument {
	return domain.Instrument{Symbol: sym, Exchange: domain.ExchangeDFM, NameEN: string(sym), Class: class,
		Currency: domain.CurrencyAED, Status: domain.StatusListed}
}

func newClient(srv *dfmtest.Server) *dfm.Client {
	return &dfm.Client{BaseURL: srv.URL, RetryDelay: time.Millisecond}
}

// clock is a fake time source that Wait advances.
type clock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
	limit int
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Wait records d and advances the clock by it; it reports false, ending
// Run, once limit waits were recorded.
func (c *clock) Wait(_ context.Context, d time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	c.now = c.now.Add(d)
	return len(c.waits) < c.limit
}

// logLines decodes JSON log lines.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(buf)
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func drain(sub *bus.Subscription[bus.QuoteUpdatedEvent]) []domain.Quote {
	var out []domain.Quote
	for {
		select {
		case e := <-sub.C():
			out = append(out, e.Quote)
		default:
			return out
		}
	}
}

func symbols(qs []domain.Quote) []domain.Symbol {
	out := make([]domain.Symbol, len(qs))
	for i, q := range qs {
		out[i] = q.Symbol
	}
	slices.Sort(out)
	return out
}

func TestOnePollDuringASessionHoldsEveryActiveInstrument(t *testing.T) {
	pool := storetest.NewPool(t)
	ins := recordedInstruments(t)
	seed(t, pool, ins)
	srv := dfmtest.NewServer(t, dfmtest.MarketWatchDir())
	b := bus.New()
	sub := bus.Subscribe(b, bus.QuoteUpdated, 1000)
	defer sub.Close()
	clk := &clock{now: dubai(15, 11, 0, 0), limit: 1}
	var logs bytes.Buffer
	p := marketwatch.New(marketwatch.Options{
		Source: newClient(srv), DB: pool, Bus: b, Calendar: newCalendar(t), PreOpenLead: preOpenLead,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Now: clk.Now, Wait: clk.Wait,
	})

	if err := p.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var active []domain.Symbol
	for _, in := range ins {
		if (in.Status == domain.StatusListed || in.Status == domain.StatusSuspended) &&
			(in.Class != domain.ClassIndex || in.Symbol == dfm.DFMGI) {
			active = append(active, in.Symbol)
		}
	}
	if len(active) != 280 {
		t.Fatalf("recorded security master has %d active instruments, want 280", len(active))
	}
	quotes := p.Quotes()
	held := make(map[domain.Symbol]bool, len(quotes))
	for _, q := range quotes {
		held[q.Symbol] = true
	}
	for _, sym := range active {
		if !held[sym] {
			t.Errorf("snapshot has no quote for active instrument %s", sym)
		}
	}
	// Matured bonds and sukuk still in the market watch are held too.
	if len(quotes) != 358 {
		t.Errorf("snapshot holds %d quotes, want 358 (357 market-watch lines + DFMGI)", len(quotes))
	}
	if got := drain(sub); !slices.Equal(symbols(got), symbols(quotes)) {
		t.Errorf("published %d quotes, want one per snapshot quote (%d)", len(got), len(quotes))
	}

	emaar, ok := p.Quote("EMAAR")
	if !ok || !emaar.Last.Equal(dec("11.4")) || emaar.Freshness != domain.Delayed || emaar.Source != domain.SourceDFMMarketWatch {
		t.Errorf("Quote(EMAAR) = %+v, %v; want last 11.4, delayed, dfm-mw", emaar, ok)
	}
	if gi, ok := p.Quote(dfm.DFMGI); !ok || !gi.Last.Equal(dec("5830.47")) || !gi.PrevClose.Equal(dec("5892.57")) {
		t.Errorf("Quote(DFMGI) = %+v, %v; want last 5830.47, previous close 5892.57", gi, ok)
	}
	if s, ok := p.Status(); !ok || s.Gained != 5 || s.Lost != 35 || s.Stable != 9 || s.Traded != 49 {
		t.Errorf("Status() = %+v, %v; want 49 traded: 5 gained, 35 lost, 9 stable", s, ok)
	}
	if n := len(p.IndexSeries()); n != 292 {
		t.Errorf("IndexSeries() has %d points, want 292", n)
	}
	if h := p.Health(); !h.LastSuccess.Equal(dubai(15, 11, 0, 0)) || h.ConsecutiveFailures != 0 || h.LastError != "" {
		t.Errorf("Health() = %+v, want a success at 11:00 and no failure", h)
	}
	if !slices.Equal(clk.waits, []time.Duration{30 * time.Second}) {
		t.Errorf("waits = %v, want [30s]", clk.waits)
	}

	var finished []map[string]any
	for _, l := range logLines(t, &logs) {
		if l["msg"] == "market watch poll finished" {
			finished = append(finished, l)
		}
	}
	if len(finished) != 1 {
		t.Fatalf("got %d poll summaries, want 1", len(finished))
	}
	l := finished[0]
	got := fmt.Sprintf("level=%v lines=%v changed=%v unchanged=%v unknown=%v skipped=%v",
		l["level"], l["lines"], l["changed"], l["unchanged"], l["unknown"], l["skipped"])
	if want := "level=INFO lines=484 changed=358 unchanged=0 unknown=127 skipped=0"; got != want {
		t.Errorf("poll summary = %s, want %s", got, want)
	}
	if id, _ := l["run_id"].(string); id == "" {
		t.Errorf("poll summary has no run_id: %v", l)
	}

	// A new poller starts from the stored snapshot.
	reloaded := marketwatch.New(marketwatch.Options{DB: pool})
	if err := reloaded.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := reloaded.Quotes(); !slices.Equal(symbols(got), symbols(quotes)) {
		t.Errorf("Load() restored %d quotes, want %d", len(got), len(quotes))
	}
	stored, _ := reloaded.Quote("EMAAR")
	if quoteText(stored) != quoteText(emaar) {
		t.Errorf("stored EMAAR =\n%s\nwant\n%s", quoteText(stored), quoteText(emaar))
	}
}

func TestPollStoresAndPublishesOnlyChangedQuotes(t *testing.T) {
	pool := storetest.NewPool(t)
	seed(t, pool, []domain.Instrument{
		instrument("EMAAR", domain.ClassEquity), instrument("ABTC", domain.ClassFund), instrument(dfm.DFMGI, domain.ClassIndex),
	})
	srv := dfmtest.NewServer(t, dfmtest.MarketWatchDir())
	b := bus.New()
	sub := bus.Subscribe(b, bus.QuoteUpdated, 100)
	defer sub.Close()
	p := marketwatch.New(marketwatch.Options{Source: newClient(srv), DB: pool, Bus: b})
	logger := slog.New(slog.DiscardHandler)

	res, err := p.Poll(t.Context(), logger)
	if err != nil {
		t.Fatalf("first Poll() error = %v", err)
	}
	if want := (marketwatch.Result{Lines: 484, Changed: 3, Unknown: 482}); res != want {
		t.Errorf("first Poll() = %+v, want %+v", res, want)
	}
	if got := symbols(drain(sub)); !slices.Equal(got, []domain.Symbol{"ABTC", "DFMGI", "EMAAR"}) {
		t.Errorf("first poll published %v, want ABTC, DFMGI, EMAAR", got)
	}

	res, err = p.Poll(t.Context(), logger)
	if err != nil {
		t.Fatalf("second Poll() error = %v", err)
	}
	if want := (marketwatch.Result{Lines: 484, Unchanged: 3, Unknown: 482}); res != want {
		t.Errorf("second Poll() = %+v, want %+v", res, want)
	}
	if got := drain(sub); len(got) != 0 {
		t.Errorf("unchanged poll published %v, want nothing", symbols(got))
	}

	srv.Replace("mw-stocks.json", changeLine(t, "EMAAR",
		`"lastradeprice":11.400,`, `"lastradeprice":11.420,`,
		`"totaltrades":1387,`, `"totaltrades":1388,`,
		`"modificationdate":"2026-10-15T15:15:04.923"`, `"modificationdate":"2026-10-15T15:16:00"`))
	res, err = p.Poll(t.Context(), logger)
	if err != nil {
		t.Fatalf("third Poll() error = %v", err)
	}
	if want := (marketwatch.Result{Lines: 484, Changed: 1, Unchanged: 2, Unknown: 482}); res != want {
		t.Errorf("third Poll() = %+v, want %+v", res, want)
	}
	got := drain(sub)
	if len(got) != 1 || got[0].Symbol != "EMAAR" || !got[0].Last.Equal(dec("11.42")) || got[0].Trades != 1388 {
		t.Fatalf("third poll published %+v, want EMAAR at 11.42 with 1388 trades", got)
	}
	reloaded := marketwatch.New(marketwatch.Options{DB: pool})
	if err := reloaded.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	stored, _ := reloaded.Quote("EMAAR")
	if quoteText(stored) != quoteText(got[0]) {
		t.Errorf("stored EMAAR =\n%s\nwant\n%s", quoteText(stored), quoteText(got[0]))
	}
	if stored.AsOf.Format(time.RFC3339) != "2026-10-15T15:16:00+04:00" {
		t.Errorf("stored EMAAR as of %s, want 2026-10-15T15:16:00+04:00", stored.AsOf.Format(time.RFC3339))
	}
}

func TestPollKeepsWhatWasReadWhenOneEndpointFails(t *testing.T) {
	pool := storetest.NewPool(t)
	seed(t, pool, []domain.Instrument{instrument("EMAAR", domain.ClassEquity)})
	srv := dfmtest.NewServer(t, dfmtest.MarketWatchDir())
	srv.FailNext("/mw/v1/indices", 404)
	p := marketwatch.New(marketwatch.Options{Source: newClient(srv), DB: pool, Bus: bus.New()})

	res, err := p.Poll(t.Context(), slog.New(slog.DiscardHandler))

	if err == nil || err.Error() != "dfm: /mw/v1/indices: unexpected status 404" {
		t.Errorf("Poll() error = %v, want the indices failure", err)
	}
	if want := (marketwatch.Result{Lines: 484, Changed: 1, Unknown: 484}); res != want {
		t.Errorf("Poll() = %+v, want %+v", res, want)
	}
	if _, ok := p.Quote("EMAAR"); !ok {
		t.Error("snapshot has no EMAAR quote after the indices failed")
	}
	if _, ok := p.Status(); !ok {
		t.Error("snapshot has no market status after the indices failed")
	}
	if n := len(p.IndexSeries()); n != 0 {
		t.Errorf("IndexSeries() has %d points, want none", n)
	}
}

// fakeSource answers polls with one EMAAR line, failing while fail > 0, and
// records when each poll ran.
type fakeSource struct {
	clk  *clock
	mu   sync.Mutex
	fail int
	at   []time.Time
}

func (f *fakeSource) MarketWatch(context.Context) (dfm.MarketWatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.at = append(f.at, f.clk.Now())
	if f.fail > 0 {
		f.fail--
		return dfm.MarketWatch{}, errors.New("boom")
	}
	return dfm.MarketWatch{Quotes: []domain.Quote{{Symbol: "EMAAR", AsOf: f.clk.Now(),
		Source: domain.SourceDFMMarketWatch, Freshness: domain.Delayed}}}, nil
}

func (f *fakeSource) MarketStatus(context.Context) (dfm.MarketStatus, error) {
	return dfm.MarketStatus{Index: dec("5830.47"), At: dubai(15, 15, 25, 0)}, nil
}

func (f *fakeSource) IndexSeries(context.Context) ([]dfm.IndexPoint, error) { return nil, nil }

func TestRunPollsOnTheCalendar(t *testing.T) {
	pool := storetest.NewPool(t)
	seed(t, pool, []domain.Instrument{instrument("EMAAR", domain.ClassEquity), instrument(dfm.DFMGI, domain.ClassIndex)})
	const s, m, h = time.Second, time.Minute, time.Hour
	tests := []struct {
		name      string
		start     time.Time
		closed    []time.Time
		fail      int
		limit     int
		wantPolls []time.Time
		wantWaits []time.Duration
	}{
		{
			name:  "every 30 s in session, once 30 min after the close, then idle until pre-open",
			start: dubai(15, 14, 59, 0), limit: 5,
			wantPolls: []time.Time{dubai(15, 14, 59, 0), dubai(15, 14, 59, 30), dubai(15, 15, 30, 0), dubai(16, 9, 30, 0)},
			wantWaits: []time.Duration{30 * s, 30 * s, 30 * m, 18 * h, 30 * s},
		},
		{
			name:  "once at start while closed, then idle over the weekend",
			start: dubai(17, 12, 0, 0), limit: 2,
			wantPolls: []time.Time{dubai(17, 12, 0, 0), dubai(19, 9, 30, 0)},
			wantWaits: []time.Duration{45*h + 30*m, 30 * s},
		},
		{
			name:  "pre-open polls from 09:30, the 10:00 open is not the first poll",
			start: dubai(15, 9, 0, 0), limit: 3,
			wantPolls: []time.Time{dubai(15, 9, 0, 0), dubai(15, 9, 30, 0), dubai(15, 9, 30, 30)},
			wantWaits: []time.Duration{30 * m, 30 * s, 30 * s},
		},
		{
			name:  "a closure skips the day",
			start: dubai(15, 14, 59, 30), closed: []time.Time{dubai(16, 0, 0, 0)}, limit: 4,
			wantPolls: []time.Time{dubai(15, 14, 59, 30), dubai(15, 15, 30, 0), dubai(19, 9, 30, 0)},
			wantWaits: []time.Duration{30 * s, 30 * m, 3*24*h + 18*h, 30 * s},
		},
		{
			name:  "failures back off exponentially up to 5 min and recover",
			start: dubai(15, 11, 0, 0), fail: 6, limit: 8,
			wantWaits: []time.Duration{60 * s, 120 * s, 240 * s, 300 * s, 300 * s, 300 * s, 30 * s, 30 * s},
		},
		{
			name:  "a failed closed-market poll is retried until it succeeds",
			start: dubai(17, 12, 0, 0), fail: 2, limit: 3,
			wantPolls: []time.Time{dubai(17, 12, 0, 0), dubai(17, 12, 1, 0), dubai(17, 12, 3, 0)},
			wantWaits: []time.Duration{60 * s, 120 * s, 45*h + 27*m},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := &clock{now: tt.start, limit: tt.limit}
			src := &fakeSource{clk: clk, fail: tt.fail}
			p := marketwatch.New(marketwatch.Options{
				Source: src, DB: pool, Bus: bus.New(), Calendar: newCalendar(t, tt.closed...), PreOpenLead: preOpenLead,
				Logger: slog.New(slog.DiscardHandler), Now: clk.Now, Wait: clk.Wait,
			})

			if err := p.Run(t.Context()); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			if !slices.Equal(clk.waits, tt.wantWaits) {
				t.Errorf("waits = %v, want %v", clk.waits, tt.wantWaits)
			}
			if tt.wantPolls != nil && !slices.EqualFunc(src.at, tt.wantPolls, time.Time.Equal) {
				t.Errorf("polls at %v, want %v", src.at, tt.wantPolls)
			}
		})
	}
}

func TestHealthCountsFailuresUntilASuccess(t *testing.T) {
	pool := storetest.NewPool(t)
	clk := &clock{now: dubai(15, 11, 0, 0), limit: 3}
	src := &fakeSource{clk: clk, fail: 3}
	var logs bytes.Buffer
	p := marketwatch.New(marketwatch.Options{
		Source: src, DB: pool, Bus: bus.New(), Calendar: newCalendar(t), PreOpenLead: preOpenLead,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Now: clk.Now, Wait: clk.Wait,
	})

	if err := p.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := marketwatch.Health{LastFailure: dubai(15, 11, 3, 0), LastError: "boom", ConsecutiveFailures: 3}
	if got := p.Health(); got != want {
		t.Errorf("Health() after 3 failures = %+v, want %+v", got, want)
	}
	var failed []string
	for _, l := range logLines(t, &logs) {
		if l["msg"] == "market watch poll failed" {
			failed = append(failed, fmt.Sprintf("%v %v %v", l["level"], l["consecutive_failures"], l["retry_in"]))
		}
	}
	if want := []string{"ERROR 1 1m0s", "ERROR 2 2m0s", "ERROR 3 4m0s"}; !slices.Equal(failed, want) {
		t.Errorf("failure logs = %v, want %v", failed, want)
	}

	clk.limit = 4
	if err := p.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want = marketwatch.Health{LastSuccess: dubai(15, 11, 7, 0), LastFailure: dubai(15, 11, 3, 0), LastError: "boom"}
	if got := p.Health(); got != want {
		t.Errorf("Health() after a success = %+v, want %+v", got, want)
	}
}

func TestRunReturnsNilWhenCancelled(t *testing.T) {
	pool := storetest.NewPool(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p := marketwatch.New(marketwatch.Options{
		Source: &fakeSource{clk: &clock{}}, DB: pool, Bus: bus.New(), Calendar: newCalendar(t),
		Logger: slog.New(slog.DiscardHandler),
	})
	if err := p.Run(ctx); err != nil {
		t.Errorf("Run(cancelled) = %v, want nil", err)
	}
}

// changeLine returns the path of a copy of the recorded market watch with
// the line of sym edited by the old, new pairs.
func changeLine(t *testing.T, sym string, oldNew ...string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dfmtest.MarketWatchDir(), "mw-stocks.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	start := strings.Index(text, `{"id":"`+sym+`"`)
	end := start + strings.Index(text[start:], "}")
	line := text[start:end]
	r := strings.NewReplacer(oldNew...)
	if edited := r.Replace(line); edited == line {
		t.Fatalf("changeLine(%s) changed nothing", sym)
	} else {
		text = text[:start] + edited + text[end:]
	}
	p := filepath.Join(t.TempDir(), "mw-stocks.json")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
