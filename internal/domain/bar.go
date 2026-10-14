package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// DubaiTZ is Asia/Dubai as a fixed UTC+4 zone. The UAE does not observe
// daylight saving time, so the offset never changes.
var DubaiTZ = time.FixedZone("GST", 4*60*60)

// TradingDayStart returns the Start of the OneDay bar for the DFM trading
// date year-month-day: midnight in Dubai, which is 20:00 UTC the day before.
func TradingDayStart(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, DubaiTZ)
}

// Bar is the open, high, low, and close of one instrument over one Interval.
//
// Start is the first instant the bar covers. A OneMinute bar starts on a
// minute boundary. A OneDay bar starts at midnight in Dubai on the trading
// date it covers, as returned by TradingDayStart. Start may carry any
// location; only the instant is checked, so a value read back from Postgres
// in UTC stays valid.
//
// Open, High, Low, and Close are either all positive, or all zero with zero
// Volume and Value for a period with no trades.
type Bar struct {
	Symbol                 Symbol
	Interval               Interval
	Start                  time.Time
	Open, High, Low, Close decimal.Decimal
	Volume                 int64
	Value                  decimal.Decimal
	Source                 Source
	Freshness              Freshness
}

// NewBar returns b if it is a valid bar.
func NewBar(b Bar) (Bar, error) {
	fail := func(format string, args ...any) (Bar, error) {
		return Bar{}, invalidf("bar", format, args...)
	}
	switch {
	case !b.Symbol.Valid():
		return fail("symbol %q is not a trading code", b.Symbol)
	case !b.Interval.Valid():
		return fail("unknown interval %q", b.Interval)
	case !b.Source.Valid():
		return fail("unknown source %q", b.Source)
	case !b.Freshness.Valid():
		return fail("unknown freshness %q", b.Freshness)
	case b.Start.IsZero():
		return fail("start time is missing")
	case b.Interval == OneMinute && !b.Start.Truncate(time.Minute).Equal(b.Start):
		return fail("start %s is not on a minute boundary", b.Start.Format(time.RFC3339Nano))
	case b.Interval == OneDay && !isMidnight(b.Start.In(DubaiTZ)):
		return fail("start %s is not midnight in Dubai", b.Start.Format(time.RFC3339Nano))
	}
	for _, p := range []struct {
		name string
		v    decimal.Decimal
	}{
		{"open", b.Open}, {"high", b.High}, {"low", b.Low}, {"close", b.Close}, {"value", b.Value},
	} {
		if p.v.IsNegative() {
			return fail("%s %s is negative", p.name, p.v)
		}
	}
	if b.Volume < 0 {
		return fail("volume %d is negative", b.Volume)
	}
	if b.Open.IsZero() && b.High.IsZero() && b.Low.IsZero() && b.Close.IsZero() {
		if b.Volume != 0 || !b.Value.IsZero() {
			return fail("volume %d and value %s with no prices", b.Volume, b.Value)
		}
		return b, nil
	}
	for _, p := range []struct {
		name string
		v    decimal.Decimal
	}{
		{"open", b.Open}, {"high", b.High}, {"low", b.Low}, {"close", b.Close},
	} {
		if p.v.IsZero() {
			return fail("%s is zero but other prices are set", p.name)
		}
	}
	switch {
	case b.High.LessThan(b.Low):
		return fail("high %s is below low %s", b.High, b.Low)
	case b.Open.LessThan(b.Low) || b.Open.GreaterThan(b.High):
		return fail("open %s is outside low %s and high %s", b.Open, b.Low, b.High)
	case b.Close.LessThan(b.Low) || b.Close.GreaterThan(b.High):
		return fail("close %s is outside low %s and high %s", b.Close, b.Low, b.High)
	}
	return b, nil
}

func isMidnight(t time.Time) bool {
	h, m, s := t.Clock()
	return h == 0 && m == 0 && s == 0 && t.Nanosecond() == 0
}
