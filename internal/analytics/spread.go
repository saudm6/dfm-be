package analytics

import (
	"fmt"
	"math"

	"gonum.org/v1/gonum/stat"
)

// DifferenceSpread returns y[i] - hedge*x[i].
func DifferenceSpread(y, x []float64, hedge float64) ([]float64, error) {
	if len(x) != len(y) {
		return nil, fmt.Errorf("%w: y has %d values, x has %d", ErrLengthMismatch, len(y), len(x))
	}
	out := make([]float64, len(y))
	for i := range y {
		out[i] = y[i] - hedge*x[i]
	}
	return out, nil
}

// RatioSpread returns y[i] / x[i]. Every x must be positive.
func RatioSpread(y, x []float64) ([]float64, error) {
	if len(x) != len(y) {
		return nil, fmt.Errorf("%w: y has %d values, x has %d", ErrLengthMismatch, len(y), len(x))
	}
	out := make([]float64, len(y))
	for i := range y {
		if !(x[i] > 0) {
			return nil, fmt.Errorf("%w: x %d is %v", ErrNonPositivePrice, i, x[i])
		}
		out[i] = y[i] / x[i]
	}
	return out, nil
}

// RollingMean returns the mean of each run of window consecutive values.
// The result has len(v)-window+1 elements; element i covers v[i:i+window], so
// it belongs to the instant of v[i+window-1].
func RollingMean(v []float64, window int) ([]float64, error) {
	if err := checkWindow(len(v), window, 1); err != nil {
		return nil, err
	}
	out := make([]float64, len(v)-window+1)
	for i := range out {
		out[i] = stat.Mean(v[i:i+window], nil)
	}
	return out, nil
}

// RollingStdDev returns the sample standard deviation (n-1 denominator) of
// each run of window consecutive values, laid out as in RollingMean. window
// must be at least 2.
func RollingStdDev(v []float64, window int) ([]float64, error) {
	if err := checkWindow(len(v), window, 2); err != nil {
		return nil, err
	}
	out := make([]float64, len(v)-window+1)
	for i := range out {
		out[i] = stat.StdDev(v[i:i+window], nil)
	}
	return out, nil
}

// ZScore returns, for each run of window consecutive values, how many rolling
// sample standard deviations its last value sits from the rolling mean. It is
// laid out as in RollingMean. A constant window has every value on its mean,
// so its z-score is 0.
func ZScore(v []float64, window int) ([]float64, error) {
	if err := checkWindow(len(v), window, 2); err != nil {
		return nil, err
	}
	out := make([]float64, len(v)-window+1)
	for i := range out {
		mean, sd := stat.MeanStdDev(v[i:i+window], nil)
		if sd == 0 {
			continue
		}
		out[i] = (v[i+window-1] - mean) / sd
	}
	return out, nil
}

// HalfLife returns the number of observations a spread takes to close half
// its distance to its mean. It fits s[t]-s[t-1] = a + b*s[t-1] by OLS, the
// discretised Ornstein-Uhlenbeck process, and returns -ln(2)/b. For day bars
// the result is in trading days. A b of zero or more means no pull to the
// mean and returns ErrNotMeanReverting.
func HalfLife(spread []float64) (float64, error) {
	if len(spread) < 4 {
		return 0, fmt.Errorf("%w: half-life needs 4 observations, got %d", ErrInsufficientData, len(spread))
	}
	lagged := spread[:len(spread)-1]
	delta := make([]float64, len(lagged))
	for i := range delta {
		delta[i] = spread[i+1] - spread[i]
	}
	r, err := Regress(delta, lagged)
	if err != nil {
		return 0, err
	}
	if r.Beta >= 0 {
		return 0, fmt.Errorf("%w: slope %v", ErrNotMeanReverting, r.Beta)
	}
	return -math.Ln2 / r.Beta, nil
}

func checkWindow(n, window, minWindow int) error {
	if window < minWindow {
		return fmt.Errorf("%w: window %d is below %d", ErrBadWindow, window, minWindow)
	}
	if window > n {
		return fmt.Errorf("%w: window %d exceeds %d values", ErrInsufficientData, window, n)
	}
	return nil
}
