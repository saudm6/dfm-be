package marketwatch_test

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/domain"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// quoteText prints q with decimals and times normalized, so equal quotes
// print the same.
func quoteText(q domain.Quote) string {
	at := func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.UTC().Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("%s last=%s bid=%s x %d ask=%s x %d vol=%d trades=%d value=%s prev=%s lastTrade=%s asOf=%s %s/%s",
		q.Symbol, q.Last, q.Bid, q.BidSize, q.Ask, q.AskSize, q.Volume, q.Trades, q.Value, q.PrevClose,
		at(q.LastTradeAt), at(q.AsOf), q.Source, q.Freshness)
}
