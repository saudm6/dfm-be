package ta

import (
	"math"

	"github.com/saudm6/dfm-be/internal/domain"
)

// BollingerSeries is the output of Bollinger.
type BollingerSeries struct {
	Middle, Upper, Lower Series
}

// Bollinger returns Bollinger Bands of the close: the period SMA, and that SMA
// plus and minus width population standard deviations of the same window.
// The first values are at the period-th traded bar.
func Bollinger(bars []domain.Bar, period int, width float64) (BollingerSeries, error) {
	if err := checkPeriod("bollinger", "period", period); err != nil {
		return BollingerSeries{}, err
	}
	if !(width > 0) || math.IsInf(width, 1) {
		return BollingerSeries{}, invalidf("bollinger", "width %v is not a positive number", width)
	}
	t := tradedBars(bars)
	mid := sma(t.close, period)
	upper, lower := make([]Value, len(mid)), make([]Value, len(mid))
	for i := period - 1; i < len(mid); i++ {
		var ss float64
		for _, c := range t.close[i-period+1 : i+1] {
			ss += (c - mid[i].V) * (c - mid[i].V)
		}
		sd := math.Sqrt(ss / float64(period))
		upper[i] = valid(mid[i].V + width*sd)
		lower[i] = valid(mid[i].V - width*sd)
	}
	return BollingerSeries{
		Middle: t.spread(len(bars), mid),
		Upper:  t.spread(len(bars), upper),
		Lower:  t.spread(len(bars), lower),
	}, nil
}

// ATR returns Wilder's average true range over period bars. True range is the
// largest of high-low, |high-previous close|, and |low-previous close|; the
// first traded bar has no previous close, so its true range is high-low. The
// first ATR is the mean of the first period true ranges, at the period-th
// traded bar; later values use Wilder smoothing.
func ATR(bars []domain.Bar, period int) (Series, error) {
	if err := checkPeriod("atr", "period", period); err != nil {
		return nil, err
	}
	t := tradedBars(bars)
	out := make([]Value, len(t.close))
	var atr float64
	for i := range t.close {
		tr := t.high[i] - t.low[i]
		if i > 0 {
			tr = math.Max(tr, math.Max(math.Abs(t.high[i]-t.close[i-1]), math.Abs(t.low[i]-t.close[i-1])))
		}
		switch {
		case i < period-1:
			atr += tr
			continue
		case i == period-1:
			atr = (atr + tr) / float64(period)
		default:
			atr = (atr*float64(period-1) + tr) / float64(period)
		}
		out[i] = valid(atr)
	}
	return t.spread(len(bars), out), nil
}
