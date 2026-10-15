package dfm

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/domain"
)

// SecurityMaster is DFM's instrument list joined from its securities lists,
// security overviews, market watch, and sectors.
type SecurityMaster struct {
	// Instruments holds every line of the securities lists, then the DFM
	// General Index and the 8 DFM sector indices.
	Instruments []domain.Instrument
	// Boards is the board-code registry, ordered by code.
	Boards []Board
	// Skipped lists the lines that could not become an instrument.
	Skipped []Skip
}

// Board is a DFM board code, the market-watch "market" field, with what the
// joined data shows trading on it.
type Board struct {
	Code string
	// Exchanges and Classes are sorted and empty when no line on the board
	// joins to an instrument.
	Exchanges []domain.Exchange
	Classes   []domain.InstrumentClass
	// Lines counts the market-watch lines on the board.
	Lines int
}

// Skip is a securities-list line left out of the security master.
type Skip struct {
	Symbol string
	Reason string
}

// groupClasses maps the groups of LiteSecuritiesLists to classes.
var groupClasses = map[string]domain.InstrumentClass{
	"Equities": domain.ClassEquity,
	"Bonds":    domain.ClassBond,
	"Sukuks":   domain.ClassSukuk,
	"Funds":    domain.ClassFund,
	"ETFs":     domain.ClassETF,
	"REITs":    domain.ClassREIT,
}

var groupOrder = []string{"Equities", "Bonds", "Sukuks", "Funds", "ETFs", "REITs"}

// indexSectorIDs are the SectorIDs of the DFM General Index and the 8 DFM
// sector indices in the sectors list (docs/dfm-history-endpoint.md).
var indexSectorIDs = []string{"9999", "11", "22", "33", "44", "55", "66", "77", "88"}

// currencySuffix finds the currency code DFM ends bond and sukuk trading
// codes with, as in DPWDU0926EUR or EIBUH331USD2.
var currencySuffix = regexp.MustCompile(`[0-9]([A-Z]{3})[0-9]?$`)

// usdRate is the market-watch "rate" of lines quoted in USD.
var usdRate = decimal.RequireFromString("3.67")

// liteSecurity is one line of LiteSecuritiesLists.
type liteSecurity struct {
	ID             string `json:"ID"`
	SecuritySymbol string `json:"SecuritySymbol"`
	FullName       string `json:"FullName"`
	Exchange       string `json:"Exchange"`
	Sector         string `json:"Sector"`
}

// overview is a get_security_overview element. Absent values are empty.
type overview struct {
	Board        string `json:"brd"`
	NameEN       string `json:"nme"`
	NameAR       string `json:"nmr"`
	Sector       string `json:"scn"`
	Exchange     string `json:"exc"`
	ISIN         string `json:"isn"`
	ListedAt     string `json:"ldt"`
	MaturityDate string `json:"maturitydate"`
}

// stock is one line of the market watch, mw/v1/stocks.
type stock struct {
	ID             string              `json:"id"`
	Market         string              `json:"market"`
	Suspended      string              `json:"suspended"`
	Rate           decimal.NullDecimal `json:"rate"`
	MaturityDate   string              `json:"maturitydate"`
	UnderlyingCode string              `json:"underlyingcode"`
}

// sector is one entry of web/sf/v1/sectors.
type sector struct {
	SectorID   string `json:"SectorID"`
	SectorName string `json:"SectorName"`
	AS400Code  string `json:"AS400Code"`
}

// SecurityMaster fetches and joins DFM's instrument data. now decides which
// bonds and sukuk have matured.
func (c *Client) SecurityMaster(ctx context.Context, now time.Time) (SecurityMaster, error) {
	en, err := c.liteSecurities(ctx, "en")
	if err != nil {
		return SecurityMaster{}, err
	}
	ar, err := c.liteSecurities(ctx, "ar")
	if err != nil {
		return SecurityMaster{}, err
	}
	var stocks []stock
	if err := c.getJSON(ctx, "/mw/v1/stocks", nil, &stocks); err != nil {
		return SecurityMaster{}, err
	}
	sectorsEN, err := c.sectors(ctx, "en")
	if err != nil {
		return SecurityMaster{}, err
	}
	sectorsAR, err := c.sectors(ctx, "ar")
	if err != nil {
		return SecurityMaster{}, err
	}
	indices, err := indexInstruments(sectorsEN, sectorsAR)
	if err != nil {
		return SecurityMaster{}, err
	}

	j := joiner{
		arNames: make(map[string]string),
		stocks:  make(map[domain.Symbol]stock, len(stocks)),
		today:   dubaiDate(now),
	}
	for _, lines := range ar {
		for _, l := range lines {
			j.arNames[l.ID] = strings.TrimSpace(l.FullName)
		}
	}
	for _, s := range stocks {
		if sym, err := domain.ParseSymbol(s.ID); err == nil {
			j.stocks[sym] = s
		}
	}

	var m SecurityMaster
	seen := make(map[domain.Symbol]bool)
	for _, group := range groups(en) {
		class, known := groupClasses[group]
		for _, l := range en[group] {
			sym, err := domain.ParseSymbol(l.SecuritySymbol)
			switch {
			case err != nil:
				m.Skipped = append(m.Skipped, Skip{l.SecuritySymbol, err.Error()})
				continue
			case !known:
				m.Skipped = append(m.Skipped, Skip{l.SecuritySymbol, fmt.Sprintf("unknown securities group %q", group)})
				continue
			case seen[sym]:
				m.Skipped = append(m.Skipped, Skip{l.SecuritySymbol, "duplicate of an earlier line"})
				continue
			}
			seen[sym] = true
			ov, err := c.overview(ctx, sym)
			if err != nil {
				return SecurityMaster{}, err
			}
			in, err := j.instrument(sym, class, l, ov)
			if err != nil {
				m.Skipped = append(m.Skipped, Skip{l.SecuritySymbol, err.Error()})
				continue
			}
			m.Instruments = append(m.Instruments, in)
		}
	}
	m.Boards = boards(stocks, m.Instruments)
	m.Instruments = append(m.Instruments, indices...)
	return m, nil
}

// groups returns the group names of a securities list, known ones first in
// DFM's order.
func groups(lists map[string][]liteSecurity) []string {
	names := slices.Clone(groupOrder)
	for _, g := range slices.Sorted(maps.Keys(lists)) {
		if !slices.Contains(names, g) {
			names = append(names, g)
		}
	}
	return names
}

func (c *Client) liteSecurities(ctx context.Context, lang string) (map[string][]liteSecurity, error) {
	const path = "/web/widgets/v1/data"
	status, body, err := c.postForm(ctx, path, url.Values{"Command": {"LiteSecuritiesLists"}, "Language": {lang}})
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &statusError{url: path, status: status}
	}
	var lists map[string][]liteSecurity
	if err := decode("LiteSecuritiesLists "+lang, body, &lists); err != nil {
		return nil, err
	}
	if len(lists["Equities"]) == 0 {
		return nil, fmt.Errorf("dfm: LiteSecuritiesLists %s has no equities", lang)
	}
	return lists, nil
}

func (c *Client) sectors(ctx context.Context, culture string) ([]sector, error) {
	var resp struct {
		Value []sector `json:"value"`
	}
	if err := c.getJSON(ctx, "/web/sf/v1/sectors", url.Values{"sf_culture": {culture}}, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// overview returns the security overview of sym, or the zero overview when
// DFM has none: it answers 400 "Symbol passed does not exist" for some codes
// and an empty array for others, Nasdaq Dubai lines among them.
func (c *Client) overview(ctx context.Context, sym domain.Symbol) (overview, error) {
	every := c.OverviewInterval
	if every == 0 {
		every = DefaultOverviewInterval
	}
	if err := c.overviews.wait(ctx, every); err != nil {
		return overview{}, err
	}
	const path = "/cdn2/web/v1/get_security_overview"
	status, body, err := c.get(ctx, path, url.Values{"symbol": {string(sym)}})
	switch {
	case err != nil:
		return overview{}, err
	case status == http.StatusBadRequest:
		var e struct {
			Success *bool `json:"success"`
		}
		if decode(path, body, &e) == nil && e.Success != nil && !*e.Success {
			return overview{}, nil
		}
		return overview{}, &statusError{url: path, status: status}
	case status != http.StatusOK:
		return overview{}, &statusError{url: path, status: status}
	}
	var ovs []overview
	if err := decode("security overview of "+string(sym), body, &ovs); err != nil {
		return overview{}, err
	}
	if len(ovs) == 0 {
		return overview{}, nil
	}
	return trimOverview(ovs[0]), nil
}

func trimOverview(o overview) overview {
	for _, f := range []*string{&o.Board, &o.NameEN, &o.NameAR, &o.Sector, &o.Exchange, &o.ISIN, &o.ListedAt, &o.MaturityDate} {
		*f = strings.TrimSpace(*f)
	}
	return o
}

// joiner turns a securities-list line into an instrument.
type joiner struct {
	arNames map[string]string // by line ID
	stocks  map[domain.Symbol]stock
	today   time.Time
}

func (j joiner) instrument(sym domain.Symbol, class domain.InstrumentClass, l liteSecurity, ov overview) (domain.Instrument, error) {
	st, inMarketWatch := j.stocks[sym]
	exchange, err := parseExchange(cmp.Or(ov.Exchange, l.Exchange))
	if err != nil {
		return domain.Instrument{}, err
	}
	currency, err := currencyOf(sym, st, inMarketWatch)
	if err != nil {
		return domain.Instrument{}, err
	}
	in := domain.Instrument{
		Symbol:   sym,
		Exchange: exchange,
		ISIN:     ov.ISIN,
		// Some Nasdaq Dubai lines have no name in either list; the trading
		// code stands in so the line still resolves.
		NameEN:   cmp.Or(ov.NameEN, strings.TrimSpace(l.FullName), string(sym)),
		NameAR:   cmp.Or(ov.NameAR, j.arNames[l.ID]),
		Class:    class,
		Currency: currency,
		Sector:   cmp.Or(ov.Sector, strings.TrimSpace(l.Sector)),
		Board:    cmp.Or(strings.TrimSpace(st.Market), ov.Board),
		Status:   j.status(cmp.Or(strings.TrimSpace(l.FullName), ov.NameEN), st, inMarketWatch, ov),
	}
	if ov.ListedAt != "" {
		at, err := time.Parse(time.RFC3339, ov.ListedAt)
		if err != nil {
			return domain.Instrument{}, fmt.Errorf("listing date %q is not RFC 3339", ov.ListedAt)
		}
		in.ListedAt = at.UTC()
	}
	return domain.NewInstrument(in)
}

// status tells a line's status. A line whose maturity date has passed, or
// whose name DFM marks "(Matured)", has matured. A line missing from the
// market watch is no longer traded: delisted. The market watch flags
// suspended lines with "S".
func (j joiner) status(name string, st stock, inMarketWatch bool, ov overview) domain.InstrumentStatus {
	if strings.Contains(name, "(Matured)") {
		return domain.StatusMatured
	}
	for _, s := range []string{st.MaturityDate, ov.MaturityDate} {
		if d, ok := parseDate(s); ok && d.Before(j.today) {
			return domain.StatusMatured
		}
	}
	switch {
	case !inMarketWatch:
		return domain.StatusDelisted
	case strings.TrimSpace(st.Suspended) == "S":
		return domain.StatusSuspended
	}
	return domain.StatusListed
}

// currencyOf reads the currency from the code's suffix, which bond and sukuk
// lines carry, else from the market-watch rate, 3.67 on USD lines. Other
// lines trade in AED.
func currencyOf(sym domain.Symbol, st stock, inMarketWatch bool) (domain.Currency, error) {
	if m := currencySuffix.FindStringSubmatch(string(sym)); m != nil {
		c, err := domain.ParseCurrency(m[1])
		if err != nil {
			return "", fmt.Errorf("currency suffix %q is not a known currency", m[1])
		}
		return c, nil
	}
	if inMarketWatch && st.Rate.Valid && st.Rate.Decimal.Equal(usdRate) {
		return domain.CurrencyUSD, nil
	}
	return domain.CurrencyAED, nil
}

// parseExchange reads the exchange as DFM writes it: "DFM", "Nasdaq Dubai",
// or "NASDAQ Dubai".
func parseExchange(s string) (domain.Exchange, error) {
	switch strings.ToUpper(strings.Join(strings.Fields(s), " ")) {
	case "DFM":
		return domain.ExchangeDFM, nil
	case "NASDAQ DUBAI":
		return domain.ExchangeNasdaqDubai, nil
	}
	return "", fmt.Errorf("unknown exchange %q", s)
}

// parseDate reads the date of a DFM local timestamp such as
// "2026-10-23T00:00:00" as a Dubai trading day.
func parseDate(s string) (time.Time, bool) {
	if len(s) < len(time.DateOnly) {
		return time.Time{}, false
	}
	d, err := time.Parse(time.DateOnly, s[:len(time.DateOnly)])
	if err != nil {
		return time.Time{}, false
	}
	return domain.TradingDayStart(d.Year(), d.Month(), d.Day()), true
}

func dubaiDate(t time.Time) time.Time {
	y, m, d := t.In(domain.DubaiTZ).Date()
	return domain.TradingDayStart(y, m, d)
}

// indexInstruments lists the DFM General Index and the sector indices from
// the English and Arabic sectors lists.
func indexInstruments(en, ar []sector) ([]domain.Instrument, error) {
	arNames := make(map[string]string, len(ar))
	for _, s := range ar {
		arNames[s.SectorID] = strings.TrimSpace(s.SectorName)
	}
	byID := make(map[string]sector, len(en))
	for _, s := range en {
		byID[s.SectorID] = s
	}
	out := make([]domain.Instrument, 0, len(indexSectorIDs))
	for _, id := range indexSectorIDs {
		s, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("dfm: sectors list has no sector %s", id)
		}
		sym, err := domain.ParseSymbol(s.AS400Code)
		if err != nil {
			return nil, fmt.Errorf("dfm: index of sector %s: %w", id, err)
		}
		name := strings.TrimSpace(s.SectorName)
		in := domain.Instrument{
			Symbol:   sym,
			Exchange: domain.ExchangeDFM,
			NameEN:   name,
			NameAR:   arNames[id],
			Class:    domain.ClassIndex,
			Currency: domain.CurrencyAED,
			Status:   domain.StatusListed,
		}
		if id != "9999" {
			in.Sector = name
		}
		in, err = domain.NewInstrument(in)
		if err != nil {
			return nil, fmt.Errorf("dfm: index of sector %s: %w", id, err)
		}
		out = append(out, in)
	}
	return out, nil
}

// boards builds the board-code registry from the market watch joined to the
// instruments. Futures are not in the securities lists; a line with an
// underlying is a future.
func boards(stocks []stock, instruments []domain.Instrument) []Board {
	bySymbol := make(map[domain.Symbol]domain.Instrument, len(instruments))
	for _, in := range instruments {
		bySymbol[in.Symbol] = in
	}
	type acc struct {
		exchanges map[domain.Exchange]bool
		classes   map[domain.InstrumentClass]bool
		lines     int
	}
	accs := make(map[string]*acc)
	for _, s := range stocks {
		code := strings.TrimSpace(s.Market)
		if code == "" {
			continue
		}
		a := accs[code]
		if a == nil {
			a = &acc{exchanges: map[domain.Exchange]bool{}, classes: map[domain.InstrumentClass]bool{}}
			accs[code] = a
		}
		a.lines++
		sym, _ := domain.ParseSymbol(s.ID)
		if in, ok := bySymbol[sym]; ok {
			a.exchanges[in.Exchange] = true
			a.classes[in.Class] = true
		} else if strings.TrimSpace(s.UnderlyingCode) != "" {
			a.classes[domain.ClassFuture] = true
		}
	}
	out := make([]Board, 0, len(accs))
	for _, code := range slices.Sorted(maps.Keys(accs)) {
		a := accs[code]
		out = append(out, Board{
			Code:      code,
			Exchanges: sortedKeys(a.exchanges),
			Classes:   sortedKeys(a.classes),
			Lines:     a.lines,
		})
	}
	return out
}

// sortedKeys returns the keys of m in order, empty rather than nil.
func sortedKeys[K cmp.Ordered](m map[K]bool) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
