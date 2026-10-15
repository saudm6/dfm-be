package analytics

import (
	"fmt"
	"math"
	"time"

	"github.com/saudm6/dfm-be/internal/domain"
)

// Point is one observation of a time series.
type Point struct {
	At    time.Time
	Value float64
}

// Closes returns the closing price of every bar that traded, in order. Bars
// must share one symbol and be in strictly ascending Start order. Bars whose
// open, high, low, and close are all zero are no-trade periods and are
// skipped.
func Closes(bars []domain.Bar) ([]Point, error) {
	out := make([]Point, 0, len(bars))
	for i, b := range bars {
		if i > 0 {
			if b.Symbol != bars[0].Symbol {
				return nil, fmt.Errorf("%w: %q and %q", ErrMixedSymbols, bars[0].Symbol, b.Symbol)
			}
			if !b.Start.After(bars[i-1].Start) {
				return nil, fmt.Errorf("%w: bar %d at %s is not after %s", ErrUnordered, i,
					b.Start.Format(time.RFC3339), bars[i-1].Start.Format(time.RFC3339))
			}
		}
		if b.Open.IsZero() && b.High.IsZero() && b.Low.IsZero() && b.Close.IsZero() {
			continue
		}
		if !b.Close.IsPositive() {
			return nil, fmt.Errorf("%w: %s close %s at %s", ErrNonPositivePrice, b.Symbol, b.Close,
				b.Start.Format(time.RFC3339))
		}
		out = append(out, Point{At: b.Start, Value: b.Close.InexactFloat64()})
	}
	return out, nil
}

// Align keeps only the instants present in every series. It returns those
// instants in ascending order and, for each input series in argument order,
// its values at them. Each series must be in strictly ascending time order.
func Align(series ...[]Point) ([]time.Time, [][]float64, error) {
	for k, s := range series {
		for i := 1; i < len(s); i++ {
			if !s[i].At.After(s[i-1].At) {
				return nil, nil, fmt.Errorf("%w: series %d point %d at %s is not after %s", ErrUnordered, k, i,
					s[i].At.Format(time.RFC3339), s[i-1].At.Format(time.RFC3339))
			}
		}
	}
	at := []time.Time{}
	values := make([][]float64, len(series))
	for k := range values {
		values[k] = []float64{}
	}
	if len(series) == 0 {
		return at, values, nil
	}
	next := make([]int, len(series))
	for {
		var latest time.Time
		for k, s := range series {
			if next[k] == len(s) {
				return at, values, nil
			}
			if t := s[next[k]].At; k == 0 || t.After(latest) {
				latest = t
			}
		}
		matched := true
		for k, s := range series {
			for next[k] < len(s) && s[next[k]].At.Before(latest) {
				next[k]++
			}
			if next[k] == len(s) {
				return at, values, nil
			}
			if !s[next[k]].At.Equal(latest) {
				matched = false
			}
		}
		if !matched {
			continue
		}
		at = append(at, latest)
		for k, s := range series {
			values[k] = append(values[k], s[next[k]].Value)
			next[k]++
		}
	}
}

// LogReturns returns ln(prices[i] / prices[i-1]) for each i from 1, so the
// result is one shorter than prices and element i-1 belongs to the instant of
// prices[i]. Every price must be positive.
func LogReturns(prices []float64) ([]float64, error) {
	for i, p := range prices {
		if !(p > 0) {
			return nil, fmt.Errorf("%w: price %d is %v", ErrNonPositivePrice, i, p)
		}
	}
	if len(prices) < 2 {
		return nil, fmt.Errorf("%w: %d prices give no return", ErrInsufficientData, len(prices))
	}
	out := make([]float64, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		out[i-1] = math.Log(prices[i] / prices[i-1])
	}
	return out, nil
}
