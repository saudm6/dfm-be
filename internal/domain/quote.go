package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Quote is the latest market state of one instrument.
//
// A zero price or size means the feed did not provide it, for example no bid
// before the open. LastTradeAt is zero when there has been no trade. Last may
// be zero only while Trades, Volume, and Value are all zero.
type Quote struct {
	Symbol            Symbol
	Last, Bid, Ask    decimal.Decimal
	BidSize, AskSize  int64
	Volume, Trades    int64
	Value, PrevClose  decimal.Decimal
	LastTradeAt, AsOf time.Time
	Source            Source
	Freshness         Freshness
}

// NewQuote returns q if it is a valid quote.
func NewQuote(q Quote) (Quote, error) {
	fail := func(format string, args ...any) (Quote, error) {
		return Quote{}, invalidf("quote", format, args...)
	}
	switch {
	case !q.Symbol.Valid():
		return fail("symbol %q is not a trading code", q.Symbol)
	case !q.Source.Valid():
		return fail("unknown source %q", q.Source)
	case !q.Freshness.Valid():
		return fail("unknown freshness %q", q.Freshness)
	case q.AsOf.IsZero():
		return fail("as-of time is missing")
	}
	for _, p := range []struct {
		name string
		v    decimal.Decimal
	}{
		{"last", q.Last}, {"bid", q.Bid}, {"ask", q.Ask}, {"value", q.Value}, {"previous close", q.PrevClose},
	} {
		if p.v.IsNegative() {
			return fail("%s %s is negative", p.name, p.v)
		}
	}
	for _, n := range []struct {
		name string
		v    int64
	}{
		{"bid size", q.BidSize}, {"ask size", q.AskSize}, {"volume", q.Volume}, {"trades", q.Trades},
	} {
		if n.v < 0 {
			return fail("%s %d is negative", n.name, n.v)
		}
	}
	if q.Last.IsZero() {
		switch {
		case q.Trades > 0:
			return fail("last is zero but trades is %d", q.Trades)
		case q.Volume > 0:
			return fail("last is zero but volume is %d", q.Volume)
		case q.Value.IsPositive():
			return fail("last is zero but value is %s", q.Value)
		}
	}
	return q, nil
}
