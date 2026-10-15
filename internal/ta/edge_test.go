package ta_test

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/ta"
)

// withNoTrade returns bars with a no-trade bar inserted before each original
// index in at (len(bars) appends one at the end), and the inserted positions.
// Each inserted bar reuses its neighbour's Start so the sequence stays in
// time order.
func withNoTrade(t *testing.T, bars []domain.Bar, at ...int) ([]domain.Bar, []int) {
	t.Helper()
	var out []domain.Bar
	var inserted []int
	for i := 0; i <= len(bars); i++ {
		for _, a := range at {
			if a == i {
				ref := bars[min(i, len(bars)-1)]
				inserted = append(inserted, len(out))
				out = append(out, noTradeBar(t, ref.Interval, ref.Start))
			}
		}
		if i < len(bars) {
			out = append(out, bars[i])
		}
	}
	return out, inserted
}

func TestStudiesSkipNoTradeBars(t *testing.T) {
	// Before the first bar, inside every warm-up, twice in a row, at the VWAP
	// session change (index 15), and at the end.
	positions := []int{0, 5, 15, 21, 21, 30}
	for _, s := range studies {
		t.Run(s.name, func(t *testing.T) {
			bars := fixtureDays(t)
			if s.minutes {
				bars = fixtureMinutes(t)
			}
			gapped, inserted := withNoTrade(t, bars, positions...)
			want := make(ta.Series, 0, len(gapped))
			plain := fixtureWant[s.name]
			for i, j := 0, 0; i < len(gapped); i++ {
				if slices.Contains(inserted, i) {
					want = append(want, ta.Value{})
					continue
				}
				want = append(want, plain[j])
				j++
			}
			got, err := s.run(gapped)
			if err != nil {
				t.Fatal(err)
			}
			assertSeries(t, got, want)
		})
	}
}

func TestNoTradeBarLiterals(t *testing.T) {
	day := func(i int) time.Time { return domain.TradingDayStart(2026, 10, 11+i) }
	t.Run("SMA(2) windows span the gap", func(t *testing.T) {
		bars := closeBars(t, "10", "12", "14")
		bars = []domain.Bar{bars[0], noTradeBar(t, domain.OneDay, day(1)), bars[1], bars[2]}
		got, err := ta.SMA(bars, 2)
		if err != nil {
			t.Fatal(err)
		}
		assertSeries(t, got, ta.Series{{}, {}, {V: 11, Valid: true}, {V: 13, Valid: true}})
	})
	t.Run("ATR(2) previous close is the last traded close", func(t *testing.T) {
		// True ranges 11-9=2, then max(13-12, |13-10|, |12-10|)=3: mean 2.5.
		// Taking the no-trade bar's zero close would give a true range of 13.
		bars := []domain.Bar{
			newBar(t, domain.OneDay, day(0), ohlcv{open: "10", high: "11", low: "9", close: "10", volume: 1}),
			noTradeBar(t, domain.OneDay, day(1)),
			newBar(t, domain.OneDay, day(2), ohlcv{open: "12.5", high: "13", low: "12", close: "12.5", volume: 1}),
		}
		got, err := ta.ATR(bars, 2)
		if err != nil {
			t.Fatal(err)
		}
		assertSeries(t, got, ta.Series{{}, {}, {V: 2.5, Valid: true}})
	})
	t.Run("all bars no-trade", func(t *testing.T) {
		bars := []domain.Bar{noTradeBar(t, domain.OneDay, day(0)), noTradeBar(t, domain.OneDay, day(1))}
		got, err := ta.EMA(bars, 1)
		if err != nil {
			t.Fatal(err)
		}
		assertSeries(t, got, ta.Series{{}, {}})
	})
}

func TestStudyEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T) (ta.Series, error)
		want ta.Series
	}{
		{
			name: "no bars",
			run:  func(*testing.T) (ta.Series, error) { return ta.SMA(nil, 3) },
			want: ta.Series{},
		},
		{
			name: "fewer bars than the period",
			run:  func(t *testing.T) (ta.Series, error) { return ta.EMA(closeBars(t, "1", "2"), 3) },
			want: ta.Series{{}, {}},
		},
		{
			name: "period 1 SMA is the close",
			run:  func(t *testing.T) (ta.Series, error) { return ta.SMA(closeBars(t, "1.5", "2.25"), 1) },
			want: from(2, 0, 1.5, 2.25),
		},
		{
			name: "WMA(3) weights the newest close most",
			run:  func(t *testing.T) (ta.Series, error) { return ta.WMA(closeBars(t, "1", "2", "4"), 3) },
			want: from(3, 2, (1*1+2*2+3*4)/6.0),
		},
		{
			name: "RSI of a flat series is 50",
			run:  func(t *testing.T) (ta.Series, error) { return ta.RSI(closeBars(t, "5", "5", "5", "5"), 2) },
			want: from(4, 2, 50, 50),
		},
		{
			name: "RSI with no losses is 100",
			run:  func(t *testing.T) (ta.Series, error) { return ta.RSI(closeBars(t, "1", "2", "3", "3"), 2) },
			want: from(4, 2, 100, 100),
		},
		{
			name: "RSI with no gains is 0",
			run:  func(t *testing.T) (ta.Series, error) { return ta.RSI(closeBars(t, "3", "2", "1"), 2) },
			want: from(3, 2, 0),
		},
		{
			name: "stochastic %K of a flat range is 50",
			run: func(t *testing.T) (ta.Series, error) {
				return stochastic(2, 1, func(s ta.StochasticSeries) ta.Series { return s.K })(closeBars(t, "7", "7", "7"))
			},
			want: from(3, 1, 50, 50),
		},
		{
			name: "MACD shorter than the slow period is empty",
			run: func(t *testing.T) (ta.Series, error) {
				return macd(2, 3, 2, func(m ta.MACDSeries) ta.Series { return m.MACD })(closeBars(t, "1", "2"))
			},
			want: ta.Series{{}, {}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.run(t)
			if err != nil {
				t.Fatal(err)
			}
			assertSeries(t, got, tt.want)
		})
	}
}

func TestVWAPSessions(t *testing.T) {
	bar := func(start time.Time, high, low, last string, volume int64) domain.Bar {
		return newBar(t, domain.OneMinute, start, ohlcv{open: last, high: high, low: low, close: last, volume: volume})
	}
	// 19:59 and 20:00 UTC are 23:59 on 14 October and 00:00 on 15 October in
	// Dubai, so the third bar starts a new session.
	bars := []domain.Bar{
		bar(time.Date(2026, 10, 14, 19, 58, 0, 0, time.UTC), "12", "9", "9", 0),
		bar(time.Date(2026, 10, 14, 19, 59, 0, 0, time.UTC), "12", "9", "9", 100),
		bar(time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC), "6", "3", "3", 100),
		bar(time.Date(2026, 10, 14, 20, 1, 0, 0, time.UTC), "9", "9", "9", 300),
	}
	// Typical prices 10, 10, 4, 9. The first bar has no volume, so the
	// session has no VWAP yet; the last is (4*100+9*300)/400.
	assertSeries(t, ta.VWAP(bars), ta.Series{{}, {V: 10, Valid: true}, {V: 4, Valid: true}, {V: 7.75, Valid: true}})
}

func TestInvalidParameters(t *testing.T) {
	bars := closeBars(t, "1", "2", "3")
	tests := []struct {
		name string
		run  func() error
	}{
		{"SMA period 0", func() error { _, err := ta.SMA(bars, 0); return err }},
		{"EMA period -1", func() error { _, err := ta.EMA(bars, -1); return err }},
		{"WMA period 0", func() error { _, err := ta.WMA(bars, 0); return err }},
		{"RSI period 0", func() error { _, err := ta.RSI(bars, 0); return err }},
		{"MACD fast 0", func() error { _, err := ta.MACD(bars, 0, 26, 9); return err }},
		{"MACD signal 0", func() error { _, err := ta.MACD(bars, 12, 26, 0); return err }},
		{"MACD fast equals slow", func() error { _, err := ta.MACD(bars, 12, 12, 9); return err }},
		{"MACD fast above slow", func() error { _, err := ta.MACD(bars, 26, 12, 9); return err }},
		{"Bollinger period 0", func() error { _, err := ta.Bollinger(bars, 0, 2); return err }},
		{"Bollinger width 0", func() error { _, err := ta.Bollinger(bars, 20, 0); return err }},
		{"Bollinger width NaN", func() error { _, err := ta.Bollinger(bars, 20, math.NaN()); return err }},
		{"Bollinger width +Inf", func() error { _, err := ta.Bollinger(bars, 20, math.Inf(1)); return err }},
		{"ATR period 0", func() error { _, err := ta.ATR(bars, 0); return err }},
		{"Stochastic k 0", func() error { _, err := ta.Stochastic(bars, 0, 3); return err }},
		{"Stochastic d 0", func() error { _, err := ta.Stochastic(bars, 14, 0); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("err = %v, want domain.ErrInvalid", err)
			}
		})
	}
}

func TestInvalidParameterMessage(t *testing.T) {
	_, err := ta.MACD(nil, 26, 12, 9)
	const want = "ta: invalid macd: fast period 26 is not below slow period 12"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
