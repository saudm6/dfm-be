package ta

import "github.com/saudm6/dfm-be/internal/domain"

// VWAP returns the intraday volume-weighted average price: the running sum of
// typical price (high+low+close)/3 times volume, divided by the running
// volume. It restarts at the first bar of each trading day, taken as the
// calendar date of Start in Dubai. It is meant for OneMinute bars; over
// OneDay bars every value is that day's typical price. A value is empty until
// the day has traded volume.
//
// Typical price is used rather than Bar.Value because not every source fills
// Value.
func VWAP(bars []domain.Bar) Series {
	t := tradedBars(bars)
	out := make([]Value, len(t.close))
	var pv, vol float64
	var day [3]int
	for i := range t.close {
		y, m, d := t.start[i].In(domain.DubaiTZ).Date()
		if today := [3]int{y, int(m), d}; i == 0 || today != day {
			day, pv, vol = today, 0, 0
		}
		pv += (t.high[i] + t.low[i] + t.close[i]) / 3 * t.volume[i]
		vol += t.volume[i]
		if vol > 0 {
			out[i] = valid(pv / vol)
		}
	}
	return t.spread(len(bars), out)
}
