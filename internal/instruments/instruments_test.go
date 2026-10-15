package instruments_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saudm6/dfm-be/internal/dfm"
	"github.com/saudm6/dfm-be/internal/dfm/dfmtest"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/instruments"
	"github.com/saudm6/dfm-be/internal/store/storetest"
)

// recordedAt is when the fixtures were recorded: 12:00 on 2026-10-15 in Dubai.
var recordedAt = time.Date(2026, time.October, 15, 8, 0, 0, 0, time.UTC)

func newClient(srv *dfmtest.Server) *dfm.Client {
	return &dfm.Client{BaseURL: srv.URL, OverviewInterval: time.Millisecond, RetryDelay: time.Millisecond}
}

var discard = slog.New(slog.DiscardHandler)

func TestSyncStoresTheSecurityMaster(t *testing.T) {
	pool := storetest.NewPool(t)
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())

	got, err := instruments.Sync(t.Context(), pool, newClient(srv), recordedAt, discard)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	want := instruments.Result{Instruments: 31, Written: 31, Boards: 8, BoardsWritten: 8, Skipped: 1}
	if got != want {
		t.Errorf("Sync() = %+v, want %+v", got, want)
	}
	tests := []domain.Instrument{
		{
			Symbol: "EMAAR", Exchange: domain.ExchangeDFM, ISIN: "AEE000301011",
			NameEN: "Emaar Properties PJSC", NameAR: "إعمار العقارية ش.م.ع",
			Class: domain.ClassEquity, Currency: domain.CurrencyAED, Sector: "Real Estate", Board: "510",
			Status: domain.StatusListed, ListedAt: time.Date(2000, time.March, 25, 20, 0, 0, 0, time.UTC),
		},
		{
			Symbol: "DPWDU0926EUR", Exchange: domain.ExchangeNasdaqDubai,
			NameEN: "DP World Ltd 2.375% 25 Sep 2026 - 144A", NameAR: "DP World Ltd 2.375% 25 Sep 2026 - 144A",
			Class: domain.ClassBond, Currency: domain.CurrencyEUR, Board: "202", Status: domain.StatusSuspended,
		},
		{
			Symbol: "AMLAKSK2010", Exchange: domain.ExchangeDFM,
			NameEN: "AMLAK SUKUK  (Matured)", NameAR: "صكوك املاك (تم استحقاقة)",
			Class: domain.ClassSukuk, Currency: domain.CurrencyAED, Sector: "Debt Instrument", Status: domain.StatusMatured,
		},
		{
			Symbol: "DFMGI", Exchange: domain.ExchangeDFM, NameEN: "DFM Index",
			Class: domain.ClassIndex, Currency: domain.CurrencyAED, Status: domain.StatusListed,
		},
	}
	for _, want := range tests {
		got, err := instruments.Get(t.Context(), pool, want.Symbol)
		if err != nil {
			t.Fatalf("Get(%s) error = %v", want.Symbol, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Get(%s) =\n%+v\nwant\n%+v", want.Symbol, got, want)
		}
	}

	boards, err := instruments.Boards(t.Context(), pool)
	if err != nil {
		t.Fatalf("Boards() error = %v", err)
	}
	wantBoards := []dfm.Board{
		{Code: "200", Exchanges: []domain.Exchange{"NASDAQ_DUBAI"}, Classes: []domain.InstrumentClass{"equity", "fund"}, Lines: 3},
		{Code: "202", Exchanges: []domain.Exchange{"NASDAQ_DUBAI"}, Classes: []domain.InstrumentClass{"bond", "sukuk"}, Lines: 6},
		{Code: "203", Exchanges: []domain.Exchange{}, Classes: []domain.InstrumentClass{}, Lines: 1},
		{Code: "210", Exchanges: []domain.Exchange{"NASDAQ_DUBAI"}, Classes: []domain.InstrumentClass{"equity"}, Lines: 1},
		{Code: "510", Exchanges: []domain.Exchange{"DFM"}, Classes: []domain.InstrumentClass{"equity", "reit"}, Lines: 3},
		{Code: "520", Exchanges: []domain.Exchange{"DFM"}, Classes: []domain.InstrumentClass{"equity"}, Lines: 1},
		{Code: "602", Exchanges: []domain.Exchange{}, Classes: []domain.InstrumentClass{"future"}, Lines: 1},
		{Code: "605", Exchanges: []domain.Exchange{}, Classes: []domain.InstrumentClass{"future"}, Lines: 1},
	}
	if !reflect.DeepEqual(boards, wantBoards) {
		t.Errorf("Boards() =\n%+v\nwant\n%+v", boards, wantBoards)
	}
}

func TestSecondSyncWithoutUpstreamChangeWritesNothing(t *testing.T) {
	pool := storetest.NewPool(t)
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())
	if _, err := instruments.Sync(t.Context(), pool, newClient(srv), recordedAt, discard); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}
	before := rowVersions(t, pool)

	got, err := instruments.Sync(t.Context(), pool, newClient(srv), recordedAt.Add(time.Hour), discard)
	if err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}

	if want := (instruments.Result{Instruments: 31, Boards: 8, Skipped: 1}); got != want {
		t.Errorf("second Sync() = %+v, want %+v", got, want)
	}
	if after := rowVersions(t, pool); after != before {
		t.Errorf("row versions changed:\nbefore %s\nafter  %s", before, after)
	}
}

func TestSyncAppliesUpstreamChanges(t *testing.T) {
	pool := storetest.NewPool(t)
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())
	if _, err := instruments.Sync(t.Context(), pool, newClient(srv), recordedAt, discard); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}
	// DFM suspends EMAAR and stops listing EIB (suspended) and AMLAKSK2010
	// (matured).
	srv.Replace("mw-stocks.json", editFixture(t, "mw-stocks.json", func(s string) string {
		return strings.Replace(s, `"suspended":"A","adjusted":null,"disclosures":null,"lastraded":"2026-10-15T14:59:09"`,
			`"suspended":"S","adjusted":null,"disclosures":null,"lastraded":"2026-10-15T14:59:09"`, 1)
	}))
	srv.Replace("lite-securities-lists-en.json", withoutLines(t, "EIB", "AMLAKSK2010"))

	got, err := instruments.Sync(t.Context(), pool, newClient(srv), recordedAt, discard)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if want := (instruments.Result{Instruments: 29, Written: 1, Delisted: 1, Boards: 8, Skipped: 1}); got != want {
		t.Errorf("Sync() = %+v, want %+v", got, want)
	}
	for sym, want := range map[domain.Symbol]domain.InstrumentStatus{
		"EMAAR":       domain.StatusSuspended,
		"EIB":         domain.StatusDelisted,
		"AMLAKSK2010": domain.StatusMatured,
		"DFMGI":       domain.StatusListed,
	} {
		in, err := instruments.Get(t.Context(), pool, sym)
		if err != nil {
			t.Fatalf("Get(%s) error = %v", sym, err)
		}
		if in.Status != want {
			t.Errorf("%s status = %q, want %q", sym, in.Status, want)
		}
	}
}

func TestSyncLogsSkippedLines(t *testing.T) {
	pool := storetest.NewPool(t)
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil)).With("run_id", "r1")

	if _, err := instruments.Sync(t.Context(), pool, newClient(srv), recordedAt, logger); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	var line struct {
		Level, Msg, Symbol, Reason string
	}
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("decode log %q: %v", logs.String(), err)
	}
	want := `WARN|security master line skipped|DPW|duplicate of an earlier line`
	if got := line.Level + "|" + line.Msg + "|" + line.Symbol + "|" + line.Reason; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
	if !strings.Contains(logs.String(), `"run_id":"r1"`) {
		t.Errorf("log %q lacks the run id", logs.String())
	}
}

func TestSyncFailsWithoutWritingWhenTheSourceFails(t *testing.T) {
	pool := storetest.NewPool(t)
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())
	srv.FailNext("/mw/v1/stocks", http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable)

	_, err := instruments.Sync(t.Context(), pool, newClient(srv), recordedAt, discard)

	if want := "instruments: fetch security master: dfm: /mw/v1/stocks: unexpected status 503"; err == nil || err.Error() != want {
		t.Errorf("Sync() error = %v, want %q", err, want)
	}
	if _, err := instruments.Get(t.Context(), pool, "EMAAR"); !errors.Is(err, instruments.ErrNotFound) {
		t.Errorf("Get(EMAAR) error = %v, want ErrNotFound", err)
	}
}

func TestGetUnknownSymbol(t *testing.T) {
	_, err := instruments.Get(t.Context(), storetest.NewPool(t), "NOSUCH")

	if !errors.Is(err, instruments.ErrNotFound) || err.Error() != "instruments: not found: NOSUCH" {
		t.Errorf("Get(NOSUCH) error = %v, want %q", err, "instruments: not found: NOSUCH")
	}
}

// rowVersions identifies the stored version of every row: xmin changes
// whenever Postgres writes a row, even with identical values.
func rowVersions(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var v string
	err := pool.QueryRow(t.Context(), `
		SELECT (SELECT string_agg(symbol || '=' || xmin::text, ',' ORDER BY symbol) FROM instruments)
		    || ' ' || (SELECT string_agg(code || '=' || xmin::text, ',' ORDER BY code) FROM boards)`).Scan(&v)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func editFixture(t *testing.T, name string, edit func(string) string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dfmtest.SecurityMasterDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	edited := edit(string(b))
	if edited == string(b) {
		t.Fatalf("edit left %s unchanged", name)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// withoutLines returns the English securities lists without the lines of
// symbols.
func withoutLines(t *testing.T, symbols ...string) string {
	t.Helper()
	return editFixture(t, "lite-securities-lists-en.json", func(s string) string {
		var lists map[string][]json.RawMessage
		if err := json.Unmarshal([]byte(s), &lists); err != nil {
			t.Fatal(err)
		}
		for group, lines := range lists {
			kept := lines[:0]
			for _, l := range lines {
				var v struct{ SecuritySymbol string }
				if err := json.Unmarshal(l, &v); err != nil {
					t.Fatal(err)
				}
				if !slices.Contains(symbols, v.SecuritySymbol) {
					kept = append(kept, l)
				}
			}
			lists[group] = kept
		}
		out, err := json.Marshal(lists)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	})
}
