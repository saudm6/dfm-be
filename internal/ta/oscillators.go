package ta

import (
	"math"

	"github.com/saudm6/dfm-be/internal/domain"
)

// RSI returns Wilder's relative strength index of the close over period bars.
// The first average gain and loss are simple means of the first period
// changes; later ones use Wilder smoothing, (prev*(period-1)+current)/period.
// The first value is at traded bar period+1. RSI is 100 when the average loss
// is zero and 50 when the average gain and loss are both zero, so a flat
// stretch reads as neutral rather than overbought.
func RSI(bars []domain.Bar, period int) (Series, error) {
	if err := checkPeriod("rsi", "period", period); err != nil {
		return nil, err
	}
	t := tradedBars(bars)
	out := make([]Value, len(t.close))
	var avgGain, avgLoss float64
	for i := 1; i < len(t.close); i++ {
		change := t.close[i] - t.close[i-1]
		gain, loss := math.Max(change, 0), math.Max(-change, 0)
		switch {
		case i < period:
			avgGain += gain
			avgLoss += loss
			continue
		case i == period:
			avgGain = (avgGain + gain) / float64(period)
			avgLoss = (avgLoss + loss) / float64(period)
		default:
			avgGain = (avgGain*float64(period-1) + gain) / float64(period)
			avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
		}
		switch {
		case avgLoss == 0 && avgGain == 0:
			out[i] = valid(50)
		case avgLoss == 0:
			out[i] = valid(100)
		default:
			out[i] = valid(100 - 100/(1+avgGain/avgLoss))
		}
	}
	return t.spread(len(bars), out), nil
}

// MACDSeries is the output of MACD.
type MACDSeries struct {
	MACD, Signal, Histogram Series
}

// MACD returns the moving average convergence divergence of the close: the
// fast EMA minus the slow EMA, its signal-period EMA, and their difference.
// MACD starts at traded bar slow; Signal and Histogram start signal-1 traded
// bars later.
func MACD(bars []domain.Bar, fast, slow, signal int) (MACDSeries, error) {
	for _, p := range []struct {
		name string
		n    int
	}{{"fast", fast}, {"slow", slow}, {"signal", signal}} {
		if err := checkPeriod("macd", p.name+" period", p.n); err != nil {
			return MACDSeries{}, err
		}
	}
	if fast >= slow {
		return MACDSeries{}, invalidf("macd", "fast period %d is not below slow period %d", fast, slow)
	}
	t := tradedBars(bars)
	n := len(t.close)
	line, sig, hist := make([]Value, n), make([]Value, n), make([]Value, n)
	if n >= slow {
		emaFast, emaSlow := ema(t.close, fast), ema(t.close, slow)
		diff := make([]float64, 0, n-slow+1)
		for i := slow - 1; i < n; i++ {
			d := emaFast[i].V - emaSlow[i].V
			line[i] = valid(d)
			diff = append(diff, d)
		}
		for j, s := range ema(diff, signal) {
			if s.Valid {
				i := slow - 1 + j
				sig[i] = s
				hist[i] = valid(line[i].V - s.V)
			}
		}
	}
	return MACDSeries{
		MACD:      t.spread(len(bars), line),
		Signal:    t.spread(len(bars), sig),
		Histogram: t.spread(len(bars), hist),
	}, nil
}

// StochasticSeries is the output of Stochastic.
type StochasticSeries struct {
	K, D Series
}

// Stochastic returns the fast stochastic oscillator: %K is where the close
// sits in the high-low range of the last kPeriod bars, scaled 0 to 100, and %D
// is the dPeriod SMA of %K. %K is 50 when that range is zero, which happens on
// thinly traded names that print one price for kPeriod bars. %K starts at
// traded bar kPeriod and %D dPeriod-1 traded bars later.
func Stochastic(bars []domain.Bar, kPeriod, dPeriod int) (StochasticSeries, error) {
	if err := checkPeriod("stochastic", "k period", kPeriod); err != nil {
		return StochasticSeries{}, err
	}
	if err := checkPeriod("stochastic", "d period", dPeriod); err != nil {
		return StochasticSeries{}, err
	}
	t := tradedBars(bars)
	n := len(t.close)
	k, d := make([]Value, n), make([]Value, n)
	if n >= kPeriod {
		ks := make([]float64, 0, n-kPeriod+1)
		for i := kPeriod - 1; i < n; i++ {
			hh, ll := t.high[i], t.low[i]
			for j := i - kPeriod + 1; j < i; j++ {
				hh, ll = math.Max(hh, t.high[j]), math.Min(ll, t.low[j])
			}
			pct := 50.0
			if hh > ll {
				pct = 100 * (t.close[i] - ll) / (hh - ll)
			}
			k[i] = valid(pct)
			ks = append(ks, pct)
		}
		for j, v := range sma(ks, dPeriod) {
			d[kPeriod-1+j] = v
		}
	}
	return StochasticSeries{K: t.spread(len(bars), k), D: t.spread(len(bars), d)}, nil
}
