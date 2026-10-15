package domain

import (
	"strings"
	"time"
)

// InstrumentClass is the kind of security, following the groups in DFM's
// securities list. ClassIndex is a DFM index, such as the DFM General Index,
// listed so that charts and the heat map can refer to it.
type InstrumentClass string

// Instrument classes.
const (
	ClassEquity InstrumentClass = "equity"
	ClassETF    InstrumentClass = "etf"
	ClassREIT   InstrumentClass = "reit"
	ClassFund   InstrumentClass = "fund"
	ClassBond   InstrumentClass = "bond"
	ClassSukuk  InstrumentClass = "sukuk"
	ClassFuture InstrumentClass = "future"
	ClassIndex  InstrumentClass = "index"
)

// ParseInstrumentClass returns the InstrumentClass named s.
func ParseInstrumentClass(s string) (InstrumentClass, error) {
	return parseEnum[InstrumentClass]("instrument class", s)
}

// Valid reports whether c is a known instrument class.
func (c InstrumentClass) Valid() bool {
	switch c {
	case ClassEquity, ClassETF, ClassREIT, ClassFund, ClassBond, ClassSukuk, ClassFuture, ClassIndex:
		return true
	}
	return false
}

// MarshalText returns c as text.
func (c InstrumentClass) MarshalText() ([]byte, error) { return []byte(c), nil }

// UnmarshalText sets c to ParseInstrumentClass(text), so decoding rejects unknown values.
func (c *InstrumentClass) UnmarshalText(text []byte) error {
	return unmarshalText(c, ParseInstrumentClass, text)
}

// Bind sets c to ParseInstrumentClass(src). The generated HTTP server calls it for
// query parameters, so unknown values are rejected there too.
func (c *InstrumentClass) Bind(src string) error {
	return unmarshalText(c, ParseInstrumentClass, []byte(src))
}

// InstrumentStatus is whether an instrument can trade.
type InstrumentStatus string

// Instrument statuses. Delisted and matured instruments stay in the
// security master so their history still resolves.
const (
	StatusListed    InstrumentStatus = "listed"
	StatusSuspended InstrumentStatus = "suspended"
	StatusDelisted  InstrumentStatus = "delisted"
	StatusMatured   InstrumentStatus = "matured"
)

// ParseInstrumentStatus returns the InstrumentStatus named s.
func ParseInstrumentStatus(s string) (InstrumentStatus, error) {
	return parseEnum[InstrumentStatus]("instrument status", s)
}

// Valid reports whether s is a known instrument status.
func (s InstrumentStatus) Valid() bool {
	switch s {
	case StatusListed, StatusSuspended, StatusDelisted, StatusMatured:
		return true
	}
	return false
}

// MarshalText returns s as text.
func (s InstrumentStatus) MarshalText() ([]byte, error) { return []byte(s), nil }

// UnmarshalText sets s to ParseInstrumentStatus(text), so decoding rejects unknown values.
func (s *InstrumentStatus) UnmarshalText(text []byte) error {
	return unmarshalText(s, ParseInstrumentStatus, text)
}

// Bind sets s to ParseInstrumentStatus(src). The generated HTTP server calls it for
// query parameters, so unknown values are rejected there too.
func (s *InstrumentStatus) Bind(src string) error {
	return unmarshalText(s, ParseInstrumentStatus, []byte(src))
}

// Instrument is one row of the security master.
//
// Symbol, Exchange, NameEN, Class, Currency, and Status are required. ISIN,
// NameAR, Sector, Board, and ListedAt are optional and empty when unknown;
// a non-empty ISIN must pass its check digit.
type Instrument struct {
	Symbol   Symbol
	Exchange Exchange
	ISIN     string
	NameEN   string
	NameAR   string
	Class    InstrumentClass
	Currency Currency
	Sector   string
	Board    string // DFM board code, for example "510"
	Status   InstrumentStatus
	ListedAt time.Time
}

// NewInstrument returns in if it is a valid instrument.
func NewInstrument(in Instrument) (Instrument, error) {
	fail := func(format string, args ...any) (Instrument, error) {
		return Instrument{}, invalidf("instrument", format, args...)
	}
	switch {
	case !in.Symbol.Valid():
		return fail("symbol %q is not a trading code", in.Symbol)
	case !in.Exchange.Valid():
		return fail("unknown exchange %q", in.Exchange)
	case in.ISIN != "" && !validISIN(in.ISIN):
		return fail("ISIN %q is not valid", in.ISIN)
	case strings.TrimSpace(in.NameEN) == "":
		return fail("English name is missing")
	case !in.Class.Valid():
		return fail("unknown class %q", in.Class)
	case !in.Currency.Valid():
		return fail("unknown currency %q", in.Currency)
	case !in.Status.Valid():
		return fail("unknown status %q", in.Status)
	}
	return in, nil
}

// validISIN checks the ISO 6166 shape and the Luhn check digit computed over
// the code with letters expanded to 10..35.
func validISIN(s string) bool {
	if len(s) != 12 {
		return false
	}
	digits := make([]byte, 0, 24)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			if i < 2 {
				return false
			}
			digits = append(digits, c-'0')
		case c >= 'A' && c <= 'Z':
			if i == 11 {
				return false
			}
			n := c - 'A' + 10
			digits = append(digits, n/10, n%10)
		default:
			return false
		}
	}
	sum := 0
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i])
		if (len(digits)-1-i)%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}
