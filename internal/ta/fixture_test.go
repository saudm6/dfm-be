package ta_test

import (
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/ta"
)

// fixture30 is the first 30 daily rows of the price sheet ("Sheet2") in
// StockCharts ChartSchool's ATR spreadsheet, cs-atr.xls, linked from
// https://chartschool.stockcharts.com/table-of-contents/technical-indicators-and-overlays/technical-indicators/average-true-range-atr-and-average-true-range-percent-atrp
//
// The expected values in TestStudiesOnFixture were worked out from it column
// by column, one textbook formula per cell, in 40-digit decimal arithmetic,
// and are given to 10 decimal places.
var fixture30 = []ohlcv{
	{"2011-04-26", "58.01", "58.38", "57.87", "58.18", 49917704},
	{"2011-04-27", "58.25", "58.64", "58.02", "58.57", 57082728},
	{"2011-04-28", "58.41", "58.58", "58.23", "58.48", 48662012},
	{"2011-04-29", "58.39", "58.64", "58.33", "58.43", 45320488},
	{"2011-05-02", "58.54", "58.69", "58.18", "58.32", 48657984},
	{"2011-05-03", "58.3", "58.35", "57.69", "58.05", 38542448},
	{"2011-05-04", "58.08", "58.22", "57.47", "57.96", 53683456},
	{"2011-05-05", "57.68", "58.32", "57.44", "57.64", 72894216},
	{"2011-05-06", "58.21", "58.55", "57.67", "57.83", 73288608},
	{"2011-05-09", "57.92", "58.27", "57.7", "58.05", 40465512},
	{"2011-05-10", "58.22", "58.63", "58.13", "58.54", 38450864},
	{"2011-05-11", "58.45", "58.66", "57.72", "58.11", 62560180},
	{"2011-05-12", "57.92", "58.54", "57.6", "58.46", 66849224},
	{"2011-05-13", "58.33", "58.42", "57.75", "57.77", 64199104},
	{"2011-05-16", "57.52", "57.68", "56.64", "56.77", 73926104},
	{"2011-05-17", "56.49", "56.94", "56.36", "56.93", 63837676},
	{"2011-05-18", "56.85", "57.5", "56.78", "57.4", 42784404},
	{"2011-05-19", "57.59", "57.7", "57.22", "57.57", 35406852},
	{"2011-05-20", "57.51", "57.57", "57.06", "57.13", 45192584},
	{"2011-05-23", "56.37", "56.52", "56.03", "56.3", 44143224},
	{"2011-05-24", "56.44", "56.44", "55.94", "55.95", 39731200},
	{"2011-05-25", "55.87", "56.43", "55.85", "56.17", 41808476},
	{"2011-05-26", "56.0", "56.67", "55.99", "56.52", 40999640},
	{"2011-05-27", "56.61", "56.86", "56.56", "56.8", 30461028},
	{"2011-05-31", "57.33", "57.72", "57.04", "57.72", 48001164},
	{"2011-06-01", "57.58", "57.72", "56.41", "56.46", 52091848},
	{"2011-06-02", "56.58", "56.78", "56.27", "56.58", 50747352},
	{"2011-06-03", "55.94", "56.37", "55.65", "55.73", 58389968},
	{"2011-06-06", "55.62", "55.91", "55.25", "55.28", 50700276},
	{"2011-06-07", "55.44", "55.64", "55.14", "55.18", 46801720},
}

func fixtureDays(t *testing.T) []domain.Bar {
	t.Helper()
	bars := make([]domain.Bar, len(fixture30))
	for i, r := range fixture30 {
		bars[i] = dayBar(t, r)
	}
	return bars
}

// fixtureMinutes is fixture30 as one-minute bars: rows 0-14 from 10:00 on
// 14 October 2026 and rows 15-29 from 10:00 the next day, in Dubai.
func fixtureMinutes(t *testing.T) []domain.Bar {
	t.Helper()
	bars := make([]domain.Bar, len(fixture30))
	for i, r := range fixture30 {
		start := time.Date(2026, 10, 14+i/15, 10, i%15, 0, 0, domain.DubaiTZ)
		bars[i] = newBar(t, domain.OneMinute, start, r)
	}
	return bars
}

type study struct {
	name    string
	minutes bool
	run     func([]domain.Bar) (ta.Series, error)
}

var studies = []study{
	{name: "SMA(10)", run: func(b []domain.Bar) (ta.Series, error) { return ta.SMA(b, 10) }},
	{name: "EMA(10)", run: func(b []domain.Bar) (ta.Series, error) { return ta.EMA(b, 10) }},
	{name: "WMA(10)", run: func(b []domain.Bar) (ta.Series, error) { return ta.WMA(b, 10) }},
	{name: "RSI(14)", run: func(b []domain.Bar) (ta.Series, error) { return ta.RSI(b, 14) }},
	{name: "MACD(6,13,5) line", run: macd(6, 13, 5, func(m ta.MACDSeries) ta.Series { return m.MACD })},
	{name: "MACD(6,13,5) signal", run: macd(6, 13, 5, func(m ta.MACDSeries) ta.Series { return m.Signal })},
	{name: "MACD(6,13,5) histogram", run: macd(6, 13, 5, func(m ta.MACDSeries) ta.Series { return m.Histogram })},
	{name: "MACD(12,26,9) line", run: macd(12, 26, 9, func(m ta.MACDSeries) ta.Series { return m.MACD })},
	{name: "MACD(12,26,9) signal", run: macd(12, 26, 9, func(m ta.MACDSeries) ta.Series { return m.Signal })},
	{name: "MACD(12,26,9) histogram", run: macd(12, 26, 9, func(m ta.MACDSeries) ta.Series { return m.Histogram })},
	{name: "Bollinger(20,2) middle", run: bollinger(20, 2, func(b ta.BollingerSeries) ta.Series { return b.Middle })},
	{name: "Bollinger(20,2) upper", run: bollinger(20, 2, func(b ta.BollingerSeries) ta.Series { return b.Upper })},
	{name: "Bollinger(20,2) lower", run: bollinger(20, 2, func(b ta.BollingerSeries) ta.Series { return b.Lower })},
	{name: "ATR(14)", run: func(b []domain.Bar) (ta.Series, error) { return ta.ATR(b, 14) }},
	{name: "Stochastic(14,3) %K", run: stochastic(14, 3, func(s ta.StochasticSeries) ta.Series { return s.K })},
	{name: "Stochastic(14,3) %D", run: stochastic(14, 3, func(s ta.StochasticSeries) ta.Series { return s.D })},
	{name: "VWAP", minutes: true, run: func(b []domain.Bar) (ta.Series, error) { return ta.VWAP(b), nil }},
}

func macd(fast, slow, signal int, pick func(ta.MACDSeries) ta.Series) func([]domain.Bar) (ta.Series, error) {
	return func(b []domain.Bar) (ta.Series, error) {
		m, err := ta.MACD(b, fast, slow, signal)
		return pick(m), err
	}
}

func bollinger(period int, width float64, pick func(ta.BollingerSeries) ta.Series) func([]domain.Bar) (ta.Series, error) {
	return func(b []domain.Bar) (ta.Series, error) {
		s, err := ta.Bollinger(b, period, width)
		return pick(s), err
	}
}

func stochastic(k, d int, pick func(ta.StochasticSeries) ta.Series) func([]domain.Bar) (ta.Series, error) {
	return func(b []domain.Bar) (ta.Series, error) {
		s, err := ta.Stochastic(b, k, d)
		return pick(s), err
	}
}

var fixtureWant = map[string]ta.Series{
	"SMA(10)": from(30, 9,
		58.151, 58.187, 58.141, 58.139, 58.073, 57.918, 57.806, 57.75, 57.743, 57.673,
		57.498, 57.239, 57.045, 56.851, 56.754, 56.849, 56.802, 56.72, 56.536, 56.351,
		56.239),
	"EMA(10)": from(30, 9,
		58.151, 58.2217272727, 58.2014132231, 58.2484290008, 58.1614419097,
		57.9084524716, 57.7305520222, 57.6704516545, 57.6521877173, 57.557244496,
		57.3286545876, 57.0779901172, 56.9129010049, 56.8414644586, 56.8339254661,
		56.9950299268, 56.8977517583, 56.8399787113, 56.6381644002, 56.3912254183,
		56.171002615),
	"WMA(10)": from(30, 9,
		58.0398181818, 58.1105454545, 58.0965454545, 58.1545454545, 58.0874545455,
		57.8505454545, 57.6709090909, 57.5970909091, 57.5643636364, 57.4529090909,
		57.2032727273, 56.9218181818, 56.7274545455, 56.632, 56.6227272727,
		56.7983636364, 56.7276363636, 56.6872727273, 56.5072727273, 56.2789090909,
		56.066),
	"RSI(14)": from(30, 14,
		34.9680170576, 37.272583452, 43.5957761121, 45.7268560478, 41.3701010998,
		34.661276433, 32.2837850986, 35.2884464897, 39.8603181943, 43.3108559127,
		52.8776618427, 42.3392022216, 43.4942316817, 37.7290770655, 35.0781700402,
		34.4980931358),
	"MACD(6,13,5) line": from(30, 12,
		0.0350722214, -0.0365967649, -0.2218391335, -0.3033410272, -0.2737160222,
		-0.2201204672, -0.2411794961, -0.3628001466, -0.4724536001, -0.4888759758,
		-0.42897637, -0.3347939024, -0.1320375384, -0.1825117018, -0.1888219364,
		-0.306406899, -0.4301770027, -0.5026820864),
	"MACD(6,13,5) signal": from(30, 16,
		-0.1600841453, -0.1800962526, -0.2004573338, -0.2545716047, -0.3271989365,
		-0.3810912829, -0.3970529786, -0.3762999532, -0.2948791483, -0.2574233328,
		-0.2345562007, -0.2585064334, -0.3157299565, -0.3780473331),
	"MACD(6,13,5) histogram": from(30, 16,
		-0.1136318769, -0.0400242146, -0.0407221624, -0.1082285419, -0.1452546636,
		-0.1077846928, -0.0319233914, 0.0415060508, 0.1628416099, 0.074911631,
		0.0457342642, -0.0479004656, -0.1144470461, -0.1246347533),
	"MACD(12,26,9) line": from(30, 25,
		-0.5832145205, -0.5702851772, -0.6214626413, -0.6903741856, -0.7444744077),
	"MACD(12,26,9) signal":    from(30, 0),
	"MACD(12,26,9) histogram": from(30, 0),
	"Bollinger(20,2) middle": from(30, 19,
		57.8245, 57.713, 57.593, 57.495, 57.4135, 57.3835, 57.304, 57.235, 57.1395, 57.012,
		56.8685),
	"Bollinger(20,2) upper": from(30, 19,
		59.0768493921, 59.1949325221, 59.1639245685, 59.0768659867, 58.9618833505,
		58.8829435635, 58.8221488728, 58.7530579699, 58.7790789094, 58.8062641946,
		58.76396063),
	"Bollinger(20,2) lower": from(30, 19,
		56.5721506079, 56.2310674779, 56.0220754315, 55.9131340133, 55.8651166495,
		55.8840564365, 55.7858511272, 55.7169420301, 55.4999210906, 55.2177358054,
		54.97303937),
	"ATR(14)": from(30, 13,
		0.66, 0.6935714286, 0.6854591837, 0.6879263848, 0.6730745002,
		0.6614263216, 0.6927530129, 0.6789849406, 0.6719145877, 0.6724921171,
		0.6487426802, 0.668118203, 0.7139669028, 0.6993978383, 0.7158694213,
		0.7118787484, 0.696744552),
	"Stochastic(14,3) %K": from(30, 13,
		26.4, 6.3414634146, 24.4635193133, 44.635193133, 51.9313304721,
		33.4782608696, 10.2661596958, 0.3676470588, 11.3879003559, 23.8434163701,
		33.8078291815, 66.5480427046, 22.6765799257, 28.4046692607, 3.8647342995,
		1.2145748988, 1.5503875969),
	"Stochastic(14,3) %D": from(30, 15,
		19.068327576, 25.146725287, 40.3433476395, 43.3482614916, 31.8919170125,
		14.7040225414, 7.3405690368, 11.8663212616, 23.0130486358, 41.3997627521,
		41.0108172706, 39.2097639637, 18.3153278286, 11.161326153, 2.2098989317),
	"VWAP": from(30, 0,
		58.1433333333, 58.2855950094, 58.3307378018, 58.3613889741, 58.368265016,
		58.323024508, 58.2539796623, 58.1741927762, 58.1505376636, 58.139522249,
		58.1594481169, 58.1598342166, 58.1636899693, 58.1481849386, 58.0491280261,
		56.7433333333, 56.9372812342, 57.0767322386, 57.1193612055, 56.9598513094,
		56.8352990482, 56.743733358, 56.7031396586, 56.7060608498, 56.7934635917,
		56.8009764065, 56.7765470892, 56.6919635682, 56.596592936, 56.5101409481),
}

func TestStudiesOnFixture(t *testing.T) {
	for _, s := range studies {
		t.Run(s.name, func(t *testing.T) {
			bars := fixtureDays(t)
			if s.minutes {
				bars = fixtureMinutes(t)
			}
			got, err := s.run(bars)
			if err != nil {
				t.Fatal(err)
			}
			want, ok := fixtureWant[s.name]
			if !ok {
				t.Fatalf("no expected values for %s", s.name)
			}
			assertSeries(t, got, want)
		})
	}
}
