package ta_test

import (
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/ta"
)

const tolerance = 1e-9

// ohlcv is one fixture row as written in its source spreadsheet.
type ohlcv struct {
	date                   string
	open, high, low, close string
	volume                 int64
}

func dayBar(t *testing.T, r ohlcv) domain.Bar {
	t.Helper()
	d, err := time.Parse(time.DateOnly, r.date)
	if err != nil {
		t.Fatal(err)
	}
	return newBar(t, domain.OneDay, domain.TradingDayStart(d.Year(), d.Month(), d.Day()), r)
}

func newBar(t *testing.T, iv domain.Interval, start time.Time, r ohlcv) domain.Bar {
	t.Helper()
	b, err := domain.NewBar(domain.Bar{
		Symbol:    "EMAAR",
		Interval:  iv,
		Start:     start,
		Open:      decimal.RequireFromString(r.open),
		High:      decimal.RequireFromString(r.high),
		Low:       decimal.RequireFromString(r.low),
		Close:     decimal.RequireFromString(r.close),
		Volume:    r.volume,
		Source:    domain.SourceDFMHistory,
		Freshness: domain.EndOfDay,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func noTradeBar(t *testing.T, iv domain.Interval, start time.Time) domain.Bar {
	t.Helper()
	return newBar(t, iv, start, ohlcv{open: "0", high: "0", low: "0", close: "0"})
}

// closeBars returns consecutive day bars with open, high, low, and close all
// equal to each close, for studies of the close alone.
func closeBars(t *testing.T, closes ...string) []domain.Bar {
	t.Helper()
	bars := make([]domain.Bar, len(closes))
	for i, c := range closes {
		bars[i] = newBar(t, domain.OneDay, domain.TradingDayStart(2026, 1, 1).AddDate(0, 0, i),
			ohlcv{open: c, high: c, low: c, close: c, volume: 1})
	}
	return bars
}

// from returns a Series of length n that is empty before index first and then
// holds vals.
func from(n, first int, vals ...float64) ta.Series {
	s := make(ta.Series, n)
	for i, v := range vals {
		s[first+i] = ta.Value{V: v, Valid: true}
	}
	return s
}

func assertSeries(t *testing.T, got, want ta.Series) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		switch {
		case g.Valid != w.Valid:
			t.Errorf("[%d] = %+v, want %+v", i, g, w)
		case w.Valid && math.Abs(g.V-w.V) > tolerance:
			t.Errorf("[%d] = %.12f, want %.12f", i, g.V, w.V)
		}
	}
}
