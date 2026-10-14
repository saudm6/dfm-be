package domain

import "strings"

// Symbol is a DFM trading code, for example "EMAAR".
type Symbol string

// maxSymbolLen leaves headroom over the longest code DFM lists today (12).
const maxSymbolLen = 16

// ParseSymbol trims and upper-cases s and checks that it is a trading code.
func ParseSymbol(s string) (Symbol, error) {
	sym := Symbol(strings.ToUpper(strings.TrimSpace(s)))
	if !sym.Valid() {
		return "", invalidf("symbol", "%q is not a trading code", s)
	}
	return sym, nil
}

// Valid reports whether s is an upper-case trading code. Inner spaces,
// hyphens, underscores, and dots are allowed because DFM lists codes such as
// "NBD GULF", "TAKAFUL-EM", and "SALAM_BAH".
func (s Symbol) Valid() bool {
	if s == "" || len(s) > maxSymbolLen {
		return false
	}
	last := len(s) - 1
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == ' ', c == '-', c == '_', c == '.':
			if i == 0 || i == last {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Exchange is the market an instrument is listed on.
type Exchange string

const (
	ExchangeDFM         Exchange = "DFM"
	ExchangeNasdaqDubai Exchange = "NASDAQ_DUBAI"
)

// ParseExchange returns the Exchange named s.
func ParseExchange(s string) (Exchange, error) { return parseEnum[Exchange]("exchange", s) }

// Valid reports whether e is a known exchange.
func (e Exchange) Valid() bool {
	switch e {
	case ExchangeDFM, ExchangeNasdaqDubai:
		return true
	}
	return false
}

// Currency is the ISO 4217 code an instrument trades in.
type Currency string

const (
	CurrencyAED Currency = "AED"
	CurrencyUSD Currency = "USD"
)

// ParseCurrency returns the Currency named s.
func ParseCurrency(s string) (Currency, error) { return parseEnum[Currency]("currency", s) }

// Valid reports whether c is a known currency.
func (c Currency) Valid() bool {
	switch c {
	case CurrencyAED, CurrencyUSD:
		return true
	}
	return false
}

// Freshness says how old a price can be when it is shown.
type Freshness string

const (
	RealTime Freshness = "realtime"
	Delayed  Freshness = "delayed"
	EndOfDay Freshness = "eod"
)

// ParseFreshness returns the Freshness named s.
func ParseFreshness(s string) (Freshness, error) { return parseEnum[Freshness]("freshness", s) }

// Valid reports whether f is a known freshness.
func (f Freshness) Valid() bool {
	switch f {
	case RealTime, Delayed, EndOfDay:
		return true
	}
	return false
}

// Source is the feed a price came from.
type Source string

const (
	SourceIBKR           Source = "ibkr"
	SourceDFMMarketWatch Source = "dfm-mw"
	SourceDFMHistory     Source = "dfm-history"
	SourceSelfRecorded   Source = "self-recorded"
)

// ParseSource returns the Source named s.
func ParseSource(s string) (Source, error) { return parseEnum[Source]("source", s) }

// Valid reports whether s is a known source.
func (s Source) Valid() bool {
	switch s {
	case SourceIBKR, SourceDFMMarketWatch, SourceDFMHistory, SourceSelfRecorded:
		return true
	}
	return false
}

// Interval is the period one Bar covers.
type Interval string

const (
	OneMinute Interval = "1m"
	OneDay    Interval = "1d"
)

// ParseInterval returns the Interval named s.
func ParseInterval(s string) (Interval, error) { return parseEnum[Interval]("interval", s) }

// Valid reports whether i is a known interval.
func (i Interval) Valid() bool {
	switch i {
	case OneMinute, OneDay:
		return true
	}
	return false
}

type enum interface {
	~string
	Valid() bool
}

func parseEnum[T enum](kind, s string) (T, error) {
	v := T(s)
	if !v.Valid() {
		var zero T
		return zero, invalidf(kind, "unknown value %q", s)
	}
	return v, nil
}
