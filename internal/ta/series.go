package ta

import (
	"fmt"
	"time"

	"github.com/saudm6/dfm-be/internal/domain"
)

// Value is one element of a Series. Valid is false where the study has no
// value: during warm-up and at no-trade bars.
type Value struct {
	V     float64
	Valid bool
}

// Series holds one Value per input bar, aligned by index.
type Series []Value

// traded is the bars that had trades, as parallel float64 columns. at maps
// each traded bar back to its index in the input.
type traded struct {
	at                       []int
	start                    []time.Time
	high, low, close, volume []float64
}

func noTrade(b domain.Bar) bool {
	return b.Open.IsZero() && b.High.IsZero() && b.Low.IsZero() && b.Close.IsZero()
}

func tradedBars(bars []domain.Bar) traded {
	var t traded
	for i, b := range bars {
		if noTrade(b) {
			continue
		}
		t.at = append(t.at, i)
		t.start = append(t.start, b.Start)
		t.high = append(t.high, b.High.InexactFloat64())
		t.low = append(t.low, b.Low.InexactFloat64())
		t.close = append(t.close, b.Close.InexactFloat64())
		t.volume = append(t.volume, float64(b.Volume))
	}
	return t
}

// spread returns a Series of length n holding vals[i] at input index t.at[i]
// and empty Values at the no-trade bars.
func (t traded) spread(n int, vals []Value) Series {
	out := make(Series, n)
	for i, v := range vals {
		out[t.at[i]] = v
	}
	return out
}

func valid(v float64) Value { return Value{V: v, Valid: true} }

func checkPeriod(study, name string, n int) error {
	if n < 1 {
		return invalidf(study, "%s %d is below 1", name, n)
	}
	return nil
}

func invalidf(study, format string, args ...any) error {
	return fmt.Errorf("ta: %w %s: %s", domain.ErrInvalid, study, fmt.Sprintf(format, args...))
}
