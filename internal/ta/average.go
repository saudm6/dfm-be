package ta

import "github.com/saudm6/dfm-be/internal/domain"

// SMA returns the simple moving average of the close over period bars. The
// first value is at the period-th traded bar.
func SMA(bars []domain.Bar, period int) (Series, error) {
	if err := checkPeriod("sma", "period", period); err != nil {
		return nil, err
	}
	t := tradedBars(bars)
	return t.spread(len(bars), sma(t.close, period)), nil
}

// EMA returns the exponential moving average of the close over period bars,
// with smoothing 2/(period+1). It is seeded with the SMA of the first period
// closes, so its first value is at the period-th traded bar.
func EMA(bars []domain.Bar, period int) (Series, error) {
	if err := checkPeriod("ema", "period", period); err != nil {
		return nil, err
	}
	t := tradedBars(bars)
	return t.spread(len(bars), ema(t.close, period)), nil
}

// WMA returns the linearly weighted moving average of the close over period
// bars: the newest close has weight period and the oldest weight 1. The first
// value is at the period-th traded bar.
func WMA(bars []domain.Bar, period int) (Series, error) {
	if err := checkPeriod("wma", "period", period); err != nil {
		return nil, err
	}
	t := tradedBars(bars)
	out := make([]Value, len(t.close))
	den := float64(period*(period+1)) / 2
	for i := period - 1; i < len(t.close); i++ {
		var sum float64
		for j := range period {
			sum += float64(j+1) * t.close[i-period+1+j]
		}
		out[i] = valid(sum / den)
	}
	return t.spread(len(bars), out), nil
}

// sma sums each window afresh rather than keeping a running sum, so rounding
// error does not accumulate over long series.
func sma(x []float64, n int) []Value {
	out := make([]Value, len(x))
	for i := n - 1; i < len(x); i++ {
		out[i] = valid(mean(x[i-n+1 : i+1]))
	}
	return out
}

func ema(x []float64, n int) []Value {
	out := make([]Value, len(x))
	if len(x) < n {
		return out
	}
	k := 2 / float64(n+1)
	prev := mean(x[:n])
	out[n-1] = valid(prev)
	for i := n; i < len(x); i++ {
		prev += (x[i] - prev) * k
		out[i] = valid(prev)
	}
	return out
}

func mean(x []float64) float64 {
	var sum float64
	for _, v := range x {
		sum += v
	}
	return sum / float64(len(x))
}
