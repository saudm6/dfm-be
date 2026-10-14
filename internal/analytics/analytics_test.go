package analytics_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/analytics"
	"github.com/saudm6/dfm-be/internal/domain"
)

// Expected values in this file were computed independently with NumPy 2.4.4
// (Python 3.13) from the same literal prices: np.diff(np.log(p)) for log
// returns, np.corrcoef for correlation, np.linalg.lstsq on [1, x] for OLS
// with R squared = 1 - SSres/SStot, sliding_window_view(...).mean() and
// .std(ddof=1) for rolling statistics, and -np.log(2)/b from lstsq of
// np.diff(s) on s[:-1] for the half-life.

// emaarCloses and devCloses are 60 consecutive DFM trading days of closes
// for a cointegrated pair, x and y respectively.
const (
	emaarCloses = "14.11 14.20 14.20 14.27 14.20 13.98 14.18 14.24 13.99 13.86 13.79 13.83 13.99 14.02 13.97 14.03 14.00 14.02 13.94 13.80 " +
		"13.95 13.91 13.93 13.86 13.79 13.75 13.83 13.97 14.04 13.97 14.31 14.41 14.52 14.46 14.43 14.50 14.29 14.20 14.25 14.54 " +
		"14.59 14.46 14.32 14.43 14.34 14.30 14.53 14.54 14.50 14.40 14.56 14.72 14.64 14.47 14.45 14.62 14.67 14.53 14.57 14.66"
	devCloses = "9.67 9.72 9.73 9.77 9.68 9.51 9.54 9.53 9.31 9.35 9.39 9.45 9.59 9.62 9.52 9.59 9.55 9.56 9.36 9.29 " +
		"9.47 9.45 9.41 9.28 9.25 9.40 9.41 9.61 9.60 9.65 9.78 9.87 9.98 9.97 9.84 9.86 9.68 9.64 9.66 9.84 " +
		"9.94 9.79 9.66 9.72 9.69 9.63 9.86 9.79 9.95 9.88 10.06 10.03 9.96 9.87 10.01 10.08 10.20 10.00 10.12 10.07"
)

func day(d int) time.Time { return domain.TradingDayStart(2026, time.July, d) }

func bar(t *testing.T, sym domain.Symbol, start time.Time, closePrice string) domain.Bar {
	t.Helper()
	c := decimal.RequireFromString(closePrice)
	vol, val := int64(1000), c.Mul(decimal.NewFromInt(1000))
	if c.IsZero() {
		vol, val = 0, decimal.Zero
	}
	b, err := domain.NewBar(domain.Bar{
		Symbol: sym, Interval: domain.OneDay, Start: start,
		Open: c, High: c, Low: c, Close: c, Volume: vol, Value: val,
		Source: domain.SourceDFMHistory, Freshness: domain.EndOfDay,
	})
	if err != nil {
		t.Fatalf("fixture bar: %v", err)
	}
	return b
}

// weekdayBars places closes on consecutive Monday to Friday trading days
// from 1 July 2026.
func weekdayBars(t *testing.T, sym domain.Symbol, closes string) []domain.Bar {
	t.Helper()
	var bars []domain.Bar
	d := day(1)
	for _, c := range strings.Fields(closes) {
		for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			d = d.AddDate(0, 0, 1)
		}
		bars = append(bars, bar(t, sym, d, c))
		d = d.AddDate(0, 0, 1)
	}
	return bars
}

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9*math.Max(1, math.Abs(want)) {
		t.Errorf("%s = %.17g, want %.17g", name, got, want)
	}
}

func nearAll(t *testing.T, name string, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s has %d values, want %d", name, len(got), len(want))
	}
	for i := range want {
		near(t, name, got[i], want[i])
	}
}

// must panics on err, which fails the running test with the error and its
// stack; Go cannot pass a multi-value call alongside a *testing.T.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestSixtyDayPair(t *testing.T) {
	emaar := must(analytics.Closes(weekdayBars(t, "EMAAR", emaarCloses)))
	dev := must(analytics.Closes(weekdayBars(t, "EMAARDEV", devCloses)))
	at, prices, err := analytics.Align(emaar, dev)
	if err != nil {
		t.Fatal(err)
	}
	if len(at) != 60 || !at[0].Equal(day(1)) || !at[59].Equal(domain.TradingDayStart(2026, time.September, 22)) {
		t.Fatalf("aligned %d days from %s to %s, want 60 from 2026-07-01 to 2026-09-22", len(at), at[0], at[len(at)-1])
	}
	x, y := prices[0], prices[1]

	rx := must(analytics.LogReturns(x))
	ry := must(analytics.LogReturns(y))
	if len(rx) != 59 {
		t.Fatalf("got %d returns, want 59", len(rx))
	}
	near(t, "rx[0]", rx[0], 0.006358198742492327)
	near(t, "rx[28]", rx[28], -0.004998225328794348)
	near(t, "rx[58]", rx[58], 0.006158076251391975)
	near(t, "ry[0]", ry[0], 0.005157309007144839)
	near(t, "ry[28]", ry[28], 0.0051948168771041026)
	near(t, "ry[58]", ry[58], -0.0049529571288484675)

	corr := must(analytics.CorrelationMatrix(ry, rx))
	near(t, "corr[0][0]", corr[0][0], 1)
	near(t, "corr[0][1]", corr[0][1], 0.7364037813005699)
	near(t, "corr[1][0]", corr[1][0], 0.7364037813005699)
	near(t, "corr[1][1]", corr[1][1], 1)

	beta := must(analytics.Regress(ry, rx))
	near(t, "return alpha", beta.Alpha, 6.045157390293163e-05)
	near(t, "return beta", beta.Beta, 0.9667047124744994)
	near(t, "return R squared", beta.RSquared, 0.5422905291137776)
	if beta.N != 59 {
		t.Errorf("return N = %d, want 59", beta.N)
	}

	fit := must(analytics.Regress(y, x))
	near(t, "price alpha", fit.Alpha, -1.605819627608181)
	near(t, "price R squared", fit.RSquared, 0.8687756405767005)
	hedge := must(analytics.HedgeRatio(y, x))
	near(t, "hedge ratio", hedge, 0.7941149615924609)

	diff := must(analytics.DifferenceSpread(y, x, hedge))
	near(t, "diff[0]", diff[0], -1.5349621080696227)
	near(t, "diff[29]", diff[29], -1.4437860134466778)
	near(t, "diff[59]", diff[59], -1.5717253369454767)

	ratio := must(analytics.RatioSpread(y, x))
	near(t, "ratio[0]", ratio[0], 0.6853295535081503)
	near(t, "ratio[29]", ratio[29], 0.6907659269863994)
	near(t, "ratio[59]", ratio[59], 0.6869031377899045)

	mean := must(analytics.RollingMean(diff, 20))
	sd := must(analytics.RollingStdDev(diff, 20))
	z := must(analytics.ZScore(diff, 20))
	for name, s := range map[string][]float64{"mean": mean, "sd": sd, "z": z} {
		if len(s) != 41 {
			t.Fatalf("rolling %s has %d values, want 41", name, len(s))
		}
	}
	near(t, "mean[0]", mean[0], -1.6057270261038183)
	near(t, "mean[20]", mean[20], -1.6006531892061553)
	near(t, "mean[40]", mean[40], -1.61107866751457)
	near(t, "sd[0]", sd[0], 0.0860955232072886)
	near(t, "sd[20]", sd[20], 0.07659956203008532)
	near(t, "sd[40]", sd[40], 0.09998990814189572)
	near(t, "z[0]", z[0], -0.7324358052894017)
	near(t, "z[20]", z[20], -1.3809263335824213)
	near(t, "z[40]", z[40], 0.3935730245221055)

	near(t, "half-life", must(analytics.HalfLife(diff)), 1.792777188044178)
}

func TestMisalignedPair(t *testing.T) {
	// EMAARDEV has no trade on 3 July and no bar on 7 July; EMAAR has no bar
	// on 10 July. Only 1, 2, 6, 8, and 9 July are common traded days.
	emaar := must(analytics.Closes([]domain.Bar{
		bar(t, "EMAAR", day(1), "14.00"), bar(t, "EMAAR", day(2), "14.20"), bar(t, "EMAAR", day(3), "14.10"),
		bar(t, "EMAAR", day(6), "14.35"), bar(t, "EMAAR", day(7), "14.30"), bar(t, "EMAAR", day(8), "14.50"),
		bar(t, "EMAAR", day(9), "14.45"),
	}))
	dev := must(analytics.Closes([]domain.Bar{
		bar(t, "EMAARDEV", day(1), "9.50"), bar(t, "EMAARDEV", day(2), "9.62"), bar(t, "EMAARDEV", day(3), "0"),
		bar(t, "EMAARDEV", day(6), "9.70"), bar(t, "EMAARDEV", day(8), "9.85"), bar(t, "EMAARDEV", day(9), "9.80"),
		bar(t, "EMAARDEV", day(10), "9.90"),
	}))
	at, prices, err := analytics.Align(emaar, dev)
	if err != nil {
		t.Fatal(err)
	}
	wantAt := []time.Time{day(1), day(2), day(6), day(8), day(9)}
	if len(at) != len(wantAt) {
		t.Fatalf("aligned %d days, want %d", len(at), len(wantAt))
	}
	for i := range wantAt {
		if !at[i].Equal(wantAt[i]) {
			t.Errorf("at[%d] = %s, want %s", i, at[i], wantAt[i])
		}
	}
	x, y := prices[0], prices[1]
	nearAll(t, "x", x, []float64{14.00, 14.20, 14.35, 14.50, 14.45})
	nearAll(t, "y", y, []float64{9.50, 9.62, 9.70, 9.85, 9.80})

	rx := must(analytics.LogReturns(x))
	ry := must(analytics.LogReturns(y))
	nearAll(t, "rx", rx, []float64{0.014184634991956546, 0.010507977598415152, 0.01039870722089864, -0.0034542348680877133})
	nearAll(t, "ry", ry, []float64{0.012552466071119817, 0.008281620831721792, 0.01534556967466072, -0.005089069507471233})
	corr := must(analytics.CorrelationMatrix(ry, rx))
	near(t, "corr", corr[0][1], 0.9294437791139059)

	fit := must(analytics.Regress(y, x))
	near(t, "alpha", fit.Alpha, -0.09933333333334679)
	near(t, "hedge", fit.Beta, 0.6848484848484857)
	near(t, "R squared", fit.RSquared, 0.9830777285045565)
	diff := must(analytics.DifferenceSpread(y, x, fit.Beta))
	nearAll(t, "diff", diff, []float64{-0.08787878787880032, -0.10484848484849785, -0.12757575757577122, -0.08030303030304253, -0.0960606060606164})
	ratio := must(analytics.RatioSpread(y, x))
	nearAll(t, "ratio", ratio, []float64{0.6785714285714286, 0.6774647887323944, 0.6759581881533101, 0.6793103448275862, 0.6782006920415226})
}

func TestClosesSkipsNoTradeDays(t *testing.T) {
	closes := must(analytics.Closes([]domain.Bar{
		bar(t, "SALIK", day(1), "10.00"),
		bar(t, "SALIK", day(2), "0"),
		bar(t, "SALIK", day(3), "11.00"),
		bar(t, "SALIK", day(6), "12.10"),
	}))
	want := []analytics.Point{{At: day(1), Value: 10}, {At: day(3), Value: 11}, {At: day(6), Value: 12.1}}
	if len(closes) != len(want) {
		t.Fatalf("got %d closes, want %d", len(closes), len(want))
	}
	for i := range want {
		if !closes[i].At.Equal(want[i].At) || closes[i].Value != want[i].Value {
			t.Errorf("close %d = %v, want %v", i, closes[i], want[i])
		}
	}
	returns := must(analytics.LogReturns([]float64{closes[0].Value, closes[1].Value, closes[2].Value}))
	nearAll(t, "returns", returns, []float64{0.09531017980432493, 0.09531017980432474})
}

func TestCorrelationMatrixThreeSeries(t *testing.T) {
	got := must(analytics.CorrelationMatrix(
		[]float64{1, 2, 3, 4, 5, 7},
		[]float64{2, 1, 4, 3, 7, 5},
		[]float64{9, 7, 6, 4, 2, 1},
	))
	want := [][]float64{
		{1, 0.7428571428571429, -0.9781029125685023},
		{0.7428571428571429, 1, -0.7965992793083678},
		{-0.9781029125685023, -0.7965992793083678, 1},
	}
	for i := range want {
		nearAll(t, "corr row", got[i], want[i])
	}
}

func TestZScoreConstantWindowIsZero(t *testing.T) {
	z := must(analytics.ZScore([]float64{2, 2, 2, 3}, 3))
	nearAll(t, "z", z, []float64{0, 1.1547005383792515})
}

func TestHalfLife(t *testing.T) {
	// s[t] = 0.5*s[t-1] exactly, so b = -0.5 and the half-life is 2*ln(2).
	near(t, "half-life", must(analytics.HalfLife([]float64{16, 8, 4, 2, 1})), 1.3862943611198906)
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{"closes out of order", func() error {
			_, err := analytics.Closes([]domain.Bar{bar(t, "EMAAR", day(2), "14"), bar(t, "EMAAR", day(1), "14")})
			return err
		}, analytics.ErrUnordered},
		{"closes duplicate day", func() error {
			_, err := analytics.Closes([]domain.Bar{bar(t, "EMAAR", day(1), "14"), bar(t, "EMAAR", day(1), "14")})
			return err
		}, analytics.ErrUnordered},
		{"closes mixed symbols", func() error {
			_, err := analytics.Closes([]domain.Bar{bar(t, "EMAAR", day(1), "14"), bar(t, "DEWA", day(2), "2")})
			return err
		}, analytics.ErrMixedSymbols},
		{"closes negative close", func() error {
			b := bar(t, "EMAAR", day(1), "14")
			b.Close = decimal.RequireFromString("-1")
			_, err := analytics.Closes([]domain.Bar{b})
			return err
		}, analytics.ErrNonPositivePrice},
		{"align out of order", func() error {
			_, _, err := analytics.Align([]analytics.Point{{At: day(2)}, {At: day(1)}})
			return err
		}, analytics.ErrUnordered},
		{"log return of zero price", func() error {
			_, err := analytics.LogReturns([]float64{10, 0, 11})
			return err
		}, analytics.ErrNonPositivePrice},
		{"log return of one price", func() error {
			_, err := analytics.LogReturns([]float64{10})
			return err
		}, analytics.ErrInsufficientData},
		{"correlation length mismatch", func() error {
			_, err := analytics.CorrelationMatrix([]float64{1, 2, 3}, []float64{1, 2})
			return err
		}, analytics.ErrLengthMismatch},
		{"correlation one observation", func() error {
			_, err := analytics.CorrelationMatrix([]float64{1}, []float64{2})
			return err
		}, analytics.ErrInsufficientData},
		{"correlation constant series", func() error {
			_, err := analytics.CorrelationMatrix([]float64{1, 2, 3}, []float64{5, 5, 5})
			return err
		}, analytics.ErrZeroVariance},
		{"regress length mismatch", func() error {
			_, err := analytics.Regress([]float64{1, 2, 3}, []float64{1, 2})
			return err
		}, analytics.ErrLengthMismatch},
		{"regress two observations", func() error {
			_, err := analytics.Regress([]float64{1, 2}, []float64{1, 2})
			return err
		}, analytics.ErrInsufficientData},
		{"regress constant x", func() error {
			_, err := analytics.HedgeRatio([]float64{1, 2, 3}, []float64{4, 4, 4})
			return err
		}, analytics.ErrZeroVariance},
		{"difference spread length mismatch", func() error {
			_, err := analytics.DifferenceSpread([]float64{1, 2}, []float64{1}, 1)
			return err
		}, analytics.ErrLengthMismatch},
		{"ratio spread zero denominator", func() error {
			_, err := analytics.RatioSpread([]float64{1, 2}, []float64{1, 0})
			return err
		}, analytics.ErrNonPositivePrice},
		{"rolling mean zero window", func() error {
			_, err := analytics.RollingMean([]float64{1, 2}, 0)
			return err
		}, analytics.ErrBadWindow},
		{"rolling std dev window of one", func() error {
			_, err := analytics.RollingStdDev([]float64{1, 2}, 1)
			return err
		}, analytics.ErrBadWindow},
		{"z-score window longer than series", func() error {
			_, err := analytics.ZScore([]float64{1, 2}, 3)
			return err
		}, analytics.ErrInsufficientData},
		{"half-life of trending series", func() error {
			_, err := analytics.HalfLife([]float64{1, 2, 4, 8, 16})
			return err
		}, analytics.ErrNotMeanReverting},
		{"half-life of three values", func() error {
			_, err := analytics.HalfLife([]float64{1, 2, 1})
			return err
		}, analytics.ErrInsufficientData},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
		})
	}
}
