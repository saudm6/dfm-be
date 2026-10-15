package dfm_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/dfm"
	"github.com/saudm6/dfm-be/internal/dfm/dfmtest"
	"github.com/saudm6/dfm-be/internal/domain"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func dubaiTime(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02T15:04:05.999", s, domain.DubaiTZ)
	if err != nil {
		panic(err)
	}
	return t
}

// quoteText prints q with decimals and times normalized, so equal quotes
// print the same.
func quoteText(q domain.Quote) string {
	at := func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.UTC().Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("%s last=%s bid=%s x %d ask=%s x %d vol=%d trades=%d value=%s prev=%s lastTrade=%s asOf=%s %s/%s",
		q.Symbol, q.Last, q.Bid, q.BidSize, q.Ask, q.AskSize, q.Volume, q.Trades, q.Value, q.PrevClose,
		at(q.LastTradeAt), at(q.AsOf), q.Source, q.Freshness)
}

func TestMarketWatchParsesTheRecorded484Lines(t *testing.T) {
	srv := dfmtest.NewServer(t, dfmtest.MarketWatchDir())

	mw, err := newClient(srv).MarketWatch(t.Context())
	if err != nil {
		t.Fatalf("MarketWatch() error = %v", err)
	}

	if len(mw.Quotes) != 484 || len(mw.Skipped) != 0 {
		t.Fatalf("MarketWatch() = %d quotes, %d skipped %v; want 484 quotes, 0 skipped", len(mw.Quotes), len(mw.Skipped), mw.Skipped)
	}
	bySymbol := make(map[domain.Symbol]domain.Quote, len(mw.Quotes))
	for _, q := range mw.Quotes {
		if _, dup := bySymbol[q.Symbol]; dup {
			t.Errorf("symbol %s appears twice", q.Symbol)
		}
		if q.Source != domain.SourceDFMMarketWatch || q.Freshness != domain.Delayed {
			t.Errorf("%s: source/freshness = %s/%s, want dfm-mw/delayed", q.Symbol, q.Source, q.Freshness)
		}
		bySymbol[q.Symbol] = q
	}
	tests := []domain.Quote{
		{
			Symbol: "EMAAR", Last: dec("11.4"), Bid: dec("11.38"), BidSize: 855864, Ask: dec("11.4"), AskSize: 4455,
			Volume: 13494233, Trades: 1387, Value: dec("153639827.7"), PrevClose: dec("11.4"),
			LastTradeAt: dubaiTime("2026-10-15T14:59:09"), AsOf: dubaiTime("2026-10-15T15:15:04.923"),
			Source: domain.SourceDFMMarketWatch, Freshness: domain.Delayed,
		},
		{
			// No trade today: no last price, no last trade time.
			Symbol: "ABTC", PrevClose: dec("27.47"), AsOf: dubaiTime("2026-10-15T15:00:21.25"),
			Source: domain.SourceDFMMarketWatch, Freshness: domain.Delayed,
		},
	}
	for _, want := range tests {
		got, ok := bySymbol[want.Symbol]
		if !ok {
			t.Errorf("no quote for %s", want.Symbol)
			continue
		}
		if quoteText(got) != quoteText(want) {
			t.Errorf("quote %s =\n%s\nwant\n%s", want.Symbol, quoteText(got), quoteText(want))
		}
	}
	if got := mw.Quotes[0].AsOf.Format(time.RFC3339Nano); got != "2026-10-15T15:00:21.25+04:00" {
		t.Errorf("first line as of %s, want 2026-10-15T15:00:21.25+04:00 (Dubai local time)", got)
	}
}

func TestMarketWatchSkipsLinesThatDoNotParse(t *testing.T) {
	srv := dfmtest.NewServer(t, dfmtest.MarketWatchDir())
	body := `[
{"id":"EMAAR","lastradeprice":11.400,"lastradetime":"2026-10-15T14:59:09","previousclosingprice":11.400,"bidprice":11.380,"bidvolume":855864,"offerprice":11.400,"offervolume":4455,"totalvolume":13494233,"totalvalue":153639827.700,"totaltrades":1387,"modificationdate":"2026-10-15T15:15:04.923"},
{"id":"bad code!","lastradeprice":1,"modificationdate":"2026-10-15T15:15:04"},
{"id":"NOMOD","lastradeprice":1,"modificationdate":null},
{"id":"BADTIME","lastradeprice":1,"modificationdate":"15 Oct 2026"},
{"id":"FRACVOL","lastradeprice":1,"totalvolume":1.5,"modificationdate":"2026-10-15T15:15:04"},
{"id":"NEGBID","bidprice":-1,"modificationdate":"2026-10-15T15:15:04"}
]`
	srv.Replace("mw-stocks.json", writeFile(t, body))

	mw, err := newClient(srv).MarketWatch(t.Context())
	if err != nil {
		t.Fatalf("MarketWatch() error = %v", err)
	}
	if len(mw.Quotes) != 1 || mw.Quotes[0].Symbol != "EMAAR" {
		t.Errorf("quotes = %v, want EMAAR only", mw.Quotes)
	}
	want := []string{
		`bad code!: domain: invalid symbol: "bad code!" is not a trading code`,
		"NOMOD: modificationdate is missing",
		`BADTIME: modificationdate "15 Oct 2026" is not a local time`,
		"FRACVOL: totalvolume 1.5 is not a whole number",
		"NEGBID: domain: invalid quote: bid -1 is negative",
	}
	var got []string
	for _, s := range mw.Skipped {
		got = append(got, s.Symbol+": "+s.Reason)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("skipped =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestMarketWatchErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		status  []int
		wantErr string
	}{
		{name: "empty market watch", body: "[]", wantErr: "dfm: market watch has no lines"},
		{name: "not JSON", body: "<html>", wantErr: "dfm: decode /mw/v1/stocks"},
		{name: "server errors exhaust the retries", status: []int{500, 502, 503}, wantErr: "unexpected status 503"},
		{name: "not found", status: []int{404}, wantErr: "dfm: /mw/v1/stocks: unexpected status 404"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := dfmtest.NewServer(t, dfmtest.MarketWatchDir())
			if tt.body != "" {
				srv.Replace("mw-stocks.json", writeFile(t, tt.body))
			}
			srv.FailNext("/mw/v1/stocks", tt.status...)
			_, err := newClient(srv).MarketWatch(t.Context())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("MarketWatch() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestMarketWatchRecoversFromOneServerError(t *testing.T) {
	srv := dfmtest.NewServer(t, dfmtest.MarketWatchDir())
	srv.FailNext("/mw/v1/stocks", http.StatusBadGateway)

	mw, err := newClient(srv).MarketWatch(t.Context())
	if err != nil || len(mw.Quotes) != 484 {
		t.Fatalf("MarketWatch() = %d quotes, %v; want 484 after one retry", len(mw.Quotes), err)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 || reqs[1].UserAgent != dfm.UserAgent {
		t.Errorf("requests = %+v, want 2 with User-Agent %q", reqs, dfm.UserAgent)
	}
}

func TestMarketStatusParsesTheRecording(t *testing.T) {
	srv := dfmtest.NewServer(t, dfmtest.MarketWatchDir())

	s, err := newClient(srv).MarketStatus(t.Context())
	if err != nil {
		t.Fatalf("MarketStatus() error = %v", err)
	}

	got := fmt.Sprintf("date=%s trades=%d traded=%d gained=%d lost=%d stable=%d volume=%d value=%s index=%s change=%s pct=%s at=%s",
		s.Date.Format(time.RFC3339), s.Trades, s.Traded, s.Gained, s.Lost, s.Stable, s.Volume, s.Value,
		s.Index, s.Change, s.ChangePercent, s.At.Format(time.RFC3339Nano))
	want := "date=2026-10-15T00:00:00+04:00 trades=10758 traded=49 gained=5 lost=35 stable=9 volume=132767216 " +
		"value=403400839.85 index=5830.47 change=-62.1 pct=-1.054 at=2026-10-15T15:25:00.993+04:00"
	if got != want {
		t.Errorf("MarketStatus() =\n%s\nwant\n%s", got, want)
	}

	q, err := s.IndexQuote()
	if err != nil {
		t.Fatalf("IndexQuote() error = %v", err)
	}
	wantQuote := domain.Quote{
		Symbol: dfm.DFMGI, Last: dec("5830.47"), PrevClose: dec("5892.57"), AsOf: dubaiTime("2026-10-15T15:25:00.993"),
		Source: domain.SourceDFMMarketWatch, Freshness: domain.Delayed,
	}
	if quoteText(q) != quoteText(wantQuote) {
		t.Errorf("IndexQuote() =\n%s\nwant\n%s", quoteText(q), quoteText(wantQuote))
	}
}

func TestIndexSeriesParsesTheRecordingOldestFirst(t *testing.T) {
	srv := dfmtest.NewServer(t, dfmtest.MarketWatchDir())

	pts, err := newClient(srv).IndexSeries(t.Context())
	if err != nil {
		t.Fatalf("IndexSeries() error = %v", err)
	}

	if len(pts) != 292 {
		t.Fatalf("IndexSeries() = %d points, want 292", len(pts))
	}
	str := func(p dfm.IndexPoint) string {
		return fmt.Sprintf("%s value=%s change=%s pct=%s volume=%d",
			p.At.Format(time.RFC3339), p.Value, p.Change, p.ChangePercent, p.Volume)
	}
	for _, tt := range []struct {
		i    int
		want string
	}{
		{0, "2026-10-15T10:00:00+04:00 value=5875.97 change=-16.6 pct=-0.282 volume=1330511"},
		{291, "2026-10-15T15:00:00+04:00 value=5830.47 change=-62.1 pct=-1.054 volume=115328923"},
	} {
		if got := str(pts[tt.i]); got != tt.want {
			t.Errorf("point %d = %s, want %s", tt.i, got, tt.want)
		}
	}
	for i := 1; i < len(pts); i++ {
		if !pts[i].At.After(pts[i-1].At) {
			t.Fatalf("point %d at %s is not after point %d at %s", i, pts[i].At, i-1, pts[i-1].At)
		}
	}
}
