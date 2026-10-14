package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Bar is the open, high, low, and close of one instrument over one Interval.
//
// Start is the first instant the bar covers. A OneMinute bar starts on a
// minute boundary. A OneDay bar starts at midnight, in Start's location, of
// the trading date it covers.
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
	case b.Interval == OneDay && !isMidnight(b.Start):
		return fail("start %s is not midnight", b.Start.Format(time.RFC3339Nano))
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
	switch {
	case b.Volume < 0:
		return fail("volume %d is negative", b.Volume)
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
