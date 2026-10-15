package dfm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/domain"
)

// DFMGI is the trading code of the DFM General Index, the index mw/v1/status
// and mw/v1/indices report.
const DFMGI domain.Symbol = "DFMGI"

// localLayout is how the market watch writes times: Dubai local time with no
// offset and 0 to 3 fractional digits, as in "2026-10-15T15:15:04.923".
const localLayout = "2006-01-02T15:04:05.999999999"

// MarketWatch is one read of mw/v1/stocks.
type MarketWatch struct {
	// Quotes holds a quote per line, in DFM's order. AsOf is the line's
	// modificationdate.
	Quotes []domain.Quote
	// Skipped lists the lines that could not become a quote.
	Skipped []Skip
}

// MarketStatus is mw/v1/status: the market's breadth and totals for the
// trading day, and the DFM General Index.
type MarketStatus struct {
	// Date is the trading day, as domain.TradingDayStart returns it.
	Date time.Time
	// Trades, Volume and Value are the day's market totals.
	Trades, Volume int64
	Value          decimal.Decimal
	// Traded counts the instruments that traded; Gained, Lost and Stable
	// split them by their change on the previous close.
	Traded, Gained, Lost, Stable int
	// Index is the DFM General Index, Change its change on the previous close
	// in points, and ChangePercent that change in percent.
	Index, Change, ChangePercent decimal.Decimal
	// At is when DFM last updated the status.
	At time.Time
}

// IndexQuote returns the DFM General Index of s as a delayed quote.
func (s MarketStatus) IndexQuote() (domain.Quote, error) {
	return domain.NewQuote(domain.Quote{
		Symbol:    DFMGI,
		Last:      s.Index,
		PrevClose: s.Index.Sub(s.Change),
		AsOf:      s.At,
		Source:    domain.SourceDFMMarketWatch,
		Freshness: domain.Delayed,
	})
}

// IndexPoint is one minute of the DFM General Index from mw/v1/indices.
type IndexPoint struct {
	// At is the start of the minute.
	At time.Time
	// Value is the index level; Change and ChangePercent are its change on
	// the previous close in points and percent.
	Value, Change, ChangePercent decimal.Decimal
	// Volume is the day's cumulative volume reported with the point.
	Volume int64
}

// stockQuote is the part of a mw/v1/stocks line a quote is made of. Numbers
// are kept as their JSON text so prices never pass through float64.
type stockQuote struct {
	ID                   string      `json:"id"`
	LastTradePrice       json.Number `json:"lastradeprice"`
	LastTradeTime        *string     `json:"lastradetime"`
	PreviousClosingPrice json.Number `json:"previousclosingprice"`
	BidPrice             json.Number `json:"bidprice"`
	BidVolume            json.Number `json:"bidvolume"`
	OfferPrice           json.Number `json:"offerprice"`
	OfferVolume          json.Number `json:"offervolume"`
	TotalVolume          json.Number `json:"totalvolume"`
	TotalValue           json.Number `json:"totalvalue"`
	TotalTrades          json.Number `json:"totaltrades"`
	ModificationDate     *string     `json:"modificationdate"`
}

type statusResponse struct {
	ID               string      `json:"id"`
	Trades           json.Number `json:"trades"`
	Traded           json.Number `json:"traded"`
	Gained           json.Number `json:"gained"`
	Lost             json.Number `json:"lost"`
	Stable           json.Number `json:"stable"`
	Volume           json.Number `json:"volume"`
	Value            json.Number `json:"value"`
	Index            json.Number `json:"index"`
	Change           json.Number `json:"change"`
	ChangePercentage json.Number `json:"changepercentage"`
	Timestamp        string      `json:"timestamp"`
}

type indexPoint struct {
	ID               string      `json:"id"`
	Value            json.Number `json:"value"`
	Change           json.Number `json:"change"`
	ChangePercentage json.Number `json:"changepercentage"`
	Volume           json.Number `json:"volume"`
}

// MarketWatch reads every line of the market watch, mw/v1/stocks, as a
// delayed quote. A line that does not parse is skipped, not fatal; an empty
// market watch is an error.
func (c *Client) MarketWatch(ctx context.Context) (MarketWatch, error) {
	var lines []stockQuote
	if err := c.getNumbers(ctx, "/mw/v1/stocks", &lines); err != nil {
		return MarketWatch{}, err
	}
	if len(lines) == 0 {
		return MarketWatch{}, errors.New("dfm: market watch has no lines")
	}
	var mw MarketWatch
	for _, l := range lines {
		q, err := l.quote()
		if err != nil {
			mw.Skipped = append(mw.Skipped, Skip{Symbol: l.ID, Reason: err.Error()})
			continue
		}
		mw.Quotes = append(mw.Quotes, q)
	}
	return mw, nil
}

func (l stockQuote) quote() (domain.Quote, error) {
	sym, err := domain.ParseSymbol(l.ID)
	if err != nil {
		return domain.Quote{}, err
	}
	if l.ModificationDate == nil {
		return domain.Quote{}, errors.New("modificationdate is missing")
	}
	q := domain.Quote{Symbol: sym, Source: domain.SourceDFMMarketWatch, Freshness: domain.Delayed}
	if q.AsOf, err = parseLocal("modificationdate", *l.ModificationDate); err != nil {
		return domain.Quote{}, err
	}
	if l.LastTradeTime != nil {
		if q.LastTradeAt, err = parseLocal("lastradetime", *l.LastTradeTime); err != nil {
			return domain.Quote{}, err
		}
	}
	p := numbers{}
	q.Last = p.decimal("lastradeprice", l.LastTradePrice)
	q.PrevClose = p.decimal("previousclosingprice", l.PreviousClosingPrice)
	q.Bid = p.decimal("bidprice", l.BidPrice)
	q.Ask = p.decimal("offerprice", l.OfferPrice)
	q.Value = p.decimal("totalvalue", l.TotalValue)
	q.BidSize = p.int("bidvolume", l.BidVolume)
	q.AskSize = p.int("offervolume", l.OfferVolume)
	q.Volume = p.int("totalvolume", l.TotalVolume)
	q.Trades = p.int("totaltrades", l.TotalTrades)
	if p.err != nil {
		return domain.Quote{}, p.err
	}
	return domain.NewQuote(q)
}

// MarketStatus reads mw/v1/status.
func (c *Client) MarketStatus(ctx context.Context) (MarketStatus, error) {
	var r statusResponse
	if err := c.getNumbers(ctx, "/mw/v1/status", &r); err != nil {
		return MarketStatus{}, err
	}
	day, err := parseLocal("id", r.ID)
	if err != nil {
		return MarketStatus{}, fmt.Errorf("dfm: mw/v1/status: %w", err)
	}
	at, err := parseLocal("timestamp", r.Timestamp)
	if err != nil {
		return MarketStatus{}, fmt.Errorf("dfm: mw/v1/status: %w", err)
	}
	p := numbers{}
	s := MarketStatus{
		Date:          domain.TradingDayStart(day.Year(), day.Month(), day.Day()),
		Trades:        p.int("trades", r.Trades),
		Volume:        p.int("volume", r.Volume),
		Value:         p.decimal("value", r.Value),
		Traded:        int(p.int("traded", r.Traded)),
		Gained:        int(p.int("gained", r.Gained)),
		Lost:          int(p.int("lost", r.Lost)),
		Stable:        int(p.int("stable", r.Stable)),
		Index:         p.decimal("index", r.Index),
		Change:        p.decimal("change", r.Change),
		ChangePercent: p.decimal("changepercentage", r.ChangePercentage),
		At:            at,
	}
	if p.err != nil {
		return MarketStatus{}, fmt.Errorf("dfm: mw/v1/status: %w", p.err)
	}
	return s, nil
}

// IndexSeries reads mw/v1/indices, the DFM General Index minute by minute
// through the current or last trading day, oldest first.
func (c *Client) IndexSeries(ctx context.Context) ([]IndexPoint, error) {
	var raw []indexPoint
	if err := c.getNumbers(ctx, "/mw/v1/indices", &raw); err != nil {
		return nil, err
	}
	out := make([]IndexPoint, len(raw))
	for i, r := range raw {
		at, err := parseLocal("id", r.ID)
		if err != nil {
			return nil, fmt.Errorf("dfm: mw/v1/indices: %w", err)
		}
		p := numbers{}
		out[i] = IndexPoint{
			At:            at,
			Value:         p.decimal("value", r.Value),
			Change:        p.decimal("change", r.Change),
			ChangePercent: p.decimal("changepercentage", r.ChangePercentage),
			Volume:        p.int("volume", r.Volume),
		}
		if p.err != nil {
			return nil, fmt.Errorf("dfm: mw/v1/indices %s: %w", r.ID, p.err)
		}
	}
	slices.SortFunc(out, func(a, b IndexPoint) int { return a.At.Compare(b.At) })
	return out, nil
}

// getNumbers is getJSON with JSON numbers decoded as json.Number.
func (c *Client) getNumbers(ctx context.Context, path string, dst any) error {
	status, body, err := c.get(ctx, path, nil, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return &statusError{url: path, status: status}
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("dfm: decode %s: %w", path, err)
	}
	return nil
}

// parseLocal reads a market-watch time, which is Dubai local time.
func parseLocal(field, s string) (time.Time, error) {
	t, err := time.ParseInLocation(localLayout, strings.TrimSpace(s), domain.DubaiTZ)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %q is not a local time", field, s)
	}
	return t, nil
}

// numbers converts JSON numbers, keeping the first error. An absent number
// (null) is zero.
type numbers struct{ err error }

func (p *numbers) decimal(field string, n json.Number) decimal.Decimal {
	if n == "" || p.err != nil {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(n.String())
	if err != nil {
		p.err = fmt.Errorf("%s %q is not a number", field, n)
	}
	return d
}

func (p *numbers) int(field string, n json.Number) int64 {
	d := p.decimal(field, n)
	if p.err == nil && !d.IsInteger() {
		p.err = fmt.Errorf("%s %s is not a whole number", field, n)
	}
	return d.IntPart()
}
