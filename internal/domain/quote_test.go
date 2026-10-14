package domain_test

import (
	"database/sql"
	"database/sql/driver"
	"reflect"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/domain"
)

var gst = time.FixedZone("GST", 4*60*60)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// airArabia is AIRARABIA from GET https://api2.dfm.ae/mw/v1/stocks on 14 Oct 2026.
func airArabia() domain.Quote {
	return domain.Quote{
		Symbol:      "AIRARABIA",
		Last:        dec("4.530"),
		Bid:         dec("4.530"),
		Ask:         dec("4.570"),
		BidSize:     17125,
		AskSize:     18785,
		Volume:      1122245,
		Trades:      470,
		Value:       dec("5074031.270"),
		PrevClose:   dec("4.520"),
		LastTradeAt: time.Date(2026, 10, 14, 14, 55, 0, 0, gst),
		AsOf:        time.Date(2026, 10, 14, 15, 0, 24, 753_000_000, gst),
		Source:      domain.SourceDFMMarketWatch,
		Freshness:   domain.Delayed,
	}
}

func TestNewQuote(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*domain.Quote)
		wantErr string
	}{
		{name: "valid", edit: func(*domain.Quote) {}},
		{name: "no prices before the open", edit: func(q *domain.Quote) {
			q.Last, q.Bid, q.Ask, q.Value = decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
			q.BidSize, q.AskSize, q.Volume, q.Trades = 0, 0, 0, 0
			q.LastTradeAt = time.Time{}
		}},
		{name: "trades without a last price", edit: func(q *domain.Quote) { q.Last = decimal.Zero },
			wantErr: `domain: invalid quote: last is zero but trades is 470`},
		{name: "volume without a last price", edit: func(q *domain.Quote) { q.Last, q.Trades = decimal.Zero, 0 },
			wantErr: `domain: invalid quote: last is zero but volume is 1122245`},
		{name: "value without a last price", edit: func(q *domain.Quote) { q.Last, q.Trades, q.Volume = decimal.Zero, 0, 0 },
			wantErr: `domain: invalid quote: last is zero but value is 5074031.27`},
		{name: "zero value", edit: func(q *domain.Quote) { *q = domain.Quote{} },
			wantErr: `domain: invalid quote: symbol "" is not a trading code`},
		{name: "lower-case symbol", edit: func(q *domain.Quote) { q.Symbol = "airarabia" },
			wantErr: `domain: invalid quote: symbol "airarabia" is not a trading code`},
		{name: "unknown source", edit: func(q *domain.Quote) { q.Source = "yahoo" },
			wantErr: `domain: invalid quote: unknown source "yahoo"`},
		{name: "missing source", edit: func(q *domain.Quote) { q.Source = "" },
			wantErr: `domain: invalid quote: unknown source ""`},
		{name: "unknown freshness", edit: func(q *domain.Quote) { q.Freshness = "live" },
			wantErr: `domain: invalid quote: unknown freshness "live"`},
		{name: "missing freshness", edit: func(q *domain.Quote) { q.Freshness = "" },
			wantErr: `domain: invalid quote: unknown freshness ""`},
		{name: "missing as-of", edit: func(q *domain.Quote) { q.AsOf = time.Time{} },
			wantErr: `domain: invalid quote: as-of time is missing`},
		{name: "negative last", edit: func(q *domain.Quote) { q.Last = dec("-4.53") },
			wantErr: `domain: invalid quote: last -4.53 is negative`},
		{name: "negative bid", edit: func(q *domain.Quote) { q.Bid = dec("-0.001") },
			wantErr: `domain: invalid quote: bid -0.001 is negative`},
		{name: "negative ask", edit: func(q *domain.Quote) { q.Ask = dec("-1") },
			wantErr: `domain: invalid quote: ask -1 is negative`},
		{name: "negative value", edit: func(q *domain.Quote) { q.Value = dec("-5") },
			wantErr: `domain: invalid quote: value -5 is negative`},
		{name: "negative previous close", edit: func(q *domain.Quote) { q.PrevClose = dec("-4.52") },
			wantErr: `domain: invalid quote: previous close -4.52 is negative`},
		{name: "negative bid size", edit: func(q *domain.Quote) { q.BidSize = -1 },
			wantErr: `domain: invalid quote: bid size -1 is negative`},
		{name: "negative ask size", edit: func(q *domain.Quote) { q.AskSize = -2 },
			wantErr: `domain: invalid quote: ask size -2 is negative`},
		{name: "negative volume", edit: func(q *domain.Quote) { q.Volume = -3 },
			wantErr: `domain: invalid quote: volume -3 is negative`},
		{name: "negative trades", edit: func(q *domain.Quote) { q.Trades = -4 },
			wantErr: `domain: invalid quote: trades -4 is negative`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := airArabia()
			tt.edit(&in)
			got, err := domain.NewQuote(in)
			checkErr(t, err, tt.wantErr)
			want := in
			if tt.wantErr != "" {
				want = domain.Quote{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("NewQuote() = %+v, want %+v", got, want)
			}
		})
	}
}

// Prices travel to and from Postgres NUMERIC columns as exact decimal text.
func TestPriceNUMERICRoundTrip(t *testing.T) {
	var (
		_ driver.Valuer = decimal.Decimal{}
		_ sql.Scanner   = (*decimal.Decimal)(nil)
	)
	tests := []struct {
		column  any
		want    string
		wantArg driver.Value
	}{
		{column: "5074031.270", want: "5074031.27", wantArg: "5074031.27"},
		{column: []byte("0.001"), want: "0.001", wantArg: "0.001"},
		{column: "123456789012345.123456789", want: "123456789012345.123456789", wantArg: "123456789012345.123456789"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			var q domain.Quote
			if err := q.Last.Scan(tt.column); err != nil {
				t.Fatalf("Scan(%v) error = %v", tt.column, err)
			}
			if got := q.Last.String(); got != tt.want {
				t.Errorf("scanned %q, want %q", got, tt.want)
			}
			arg, err := q.Last.Value()
			if err != nil {
				t.Fatalf("Value() error = %v", err)
			}
			if arg != tt.wantArg {
				t.Errorf("Value() = %#v, want %#v", arg, tt.wantArg)
			}
		})
	}
}
