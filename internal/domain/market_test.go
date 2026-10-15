package domain_test

import (
	"encoding"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/saudm6/dfm-be/internal/domain"
)

func TestParseSymbol(t *testing.T) {
	tests := []struct {
		in      string
		want    domain.Symbol
		wantErr string
	}{
		{in: "EMAAR", want: "EMAAR"},
		{in: " emaar ", want: "EMAAR"},
		{in: "NBD GULF", want: "NBD GULF"},
		{in: "TAKAFUL-EM", want: "TAKAFUL-EM"},
		{in: "SALAM_BAH", want: "SALAM_BAH"},
		{in: "EBIUHPRP1USD", want: "EBIUHPRP1USD"},
		{in: "", wantErr: `domain: invalid symbol: "" is not a trading code`},
		{in: "-EMAAR", wantErr: `domain: invalid symbol: "-EMAAR" is not a trading code`},
		{in: "EMAAR!", wantErr: `domain: invalid symbol: "EMAAR!" is not a trading code`},
		{in: "ABCDEFGHIJKLMNOPQ", wantErr: `domain: invalid symbol: "ABCDEFGHIJKLMNOPQ" is not a trading code`},
		{in: "إعمار", wantErr: `domain: invalid symbol: "إعمار" is not a trading code`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := domain.ParseSymbol(tt.in)
			checkErr(t, err, tt.wantErr)
			if got != tt.want {
				t.Errorf("ParseSymbol(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseEnums(t *testing.T) {
	tests := []struct {
		name    string
		parse   func(string) (string, error)
		in      string
		want    string
		wantErr string
	}{
		{name: "exchange DFM", parse: wrap(domain.ParseExchange), in: "DFM", want: "DFM"},
		{name: "exchange Nasdaq Dubai", parse: wrap(domain.ParseExchange), in: "NASDAQ_DUBAI", want: "NASDAQ_DUBAI"},
		{name: "exchange unknown", parse: wrap(domain.ParseExchange), in: "ADX", wantErr: `domain: invalid exchange: unknown value "ADX"`},
		{name: "currency AED", parse: wrap(domain.ParseCurrency), in: "AED", want: "AED"},
		{name: "currency USD", parse: wrap(domain.ParseCurrency), in: "USD", want: "USD"},
		{name: "currency unknown", parse: wrap(domain.ParseCurrency), in: "aed", wantErr: `domain: invalid currency: unknown value "aed"`},
		{name: "freshness realtime", parse: wrap(domain.ParseFreshness), in: "realtime", want: "realtime"},
		{name: "freshness delayed", parse: wrap(domain.ParseFreshness), in: "delayed", want: "delayed"},
		{name: "freshness eod", parse: wrap(domain.ParseFreshness), in: "eod", want: "eod"},
		{name: "freshness unknown", parse: wrap(domain.ParseFreshness), in: "live", wantErr: `domain: invalid freshness: unknown value "live"`},
		{name: "freshness empty", parse: wrap(domain.ParseFreshness), in: "", wantErr: `domain: invalid freshness: unknown value ""`},
		{name: "source ibkr", parse: wrap(domain.ParseSource), in: "ibkr", want: "ibkr"},
		{name: "source dfm-mw", parse: wrap(domain.ParseSource), in: "dfm-mw", want: "dfm-mw"},
		{name: "source dfm-history", parse: wrap(domain.ParseSource), in: "dfm-history", want: "dfm-history"},
		{name: "source self-recorded", parse: wrap(domain.ParseSource), in: "self-recorded", want: "self-recorded"},
		{name: "source unknown", parse: wrap(domain.ParseSource), in: "yahoo", wantErr: `domain: invalid source: unknown value "yahoo"`},
		{name: "interval 1m", parse: wrap(domain.ParseInterval), in: "1m", want: "1m"},
		{name: "interval 1d", parse: wrap(domain.ParseInterval), in: "1d", want: "1d"},
		{name: "interval unknown", parse: wrap(domain.ParseInterval), in: "5m", wantErr: `domain: invalid interval: unknown value "5m"`},
		{name: "class equity", parse: wrap(domain.ParseInstrumentClass), in: "equity", want: "equity"},
		{name: "class sukuk", parse: wrap(domain.ParseInstrumentClass), in: "sukuk", want: "sukuk"},
		{name: "class unknown", parse: wrap(domain.ParseInstrumentClass), in: "Equities", wantErr: `domain: invalid instrument class: unknown value "Equities"`},
		{name: "status listed", parse: wrap(domain.ParseInstrumentStatus), in: "listed", want: "listed"},
		{name: "status matured", parse: wrap(domain.ParseInstrumentStatus), in: "matured", want: "matured"},
		{name: "status unknown", parse: wrap(domain.ParseInstrumentStatus), in: "A", wantErr: `domain: invalid instrument status: unknown value "A"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.parse(tt.in)
			checkErr(t, err, tt.wantErr)
			if got != tt.want {
				t.Errorf("parse(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func wrap[T ~string](parse func(string) (T, error)) func(string) (string, error) {
	return func(s string) (string, error) {
		v, err := parse(s)
		return string(v), err
	}
}

func TestEnumTextUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		decode  func(string) (string, error)
		in      string
		want    string
		wantErr string
	}{
		{name: "exchange", decode: unmarshalText[domain.Exchange], in: "DFM", want: "DFM"},
		{name: "exchange unknown", decode: unmarshalText[domain.Exchange], in: "ADX", wantErr: `domain: invalid exchange: unknown value "ADX"`},
		{name: "currency", decode: unmarshalText[domain.Currency], in: "AED", want: "AED"},
		{name: "currency unknown", decode: unmarshalText[domain.Currency], in: "aed", wantErr: `domain: invalid currency: unknown value "aed"`},
		{name: "freshness", decode: unmarshalText[domain.Freshness], in: "eod", want: "eod"},
		{name: "freshness unknown", decode: unmarshalText[domain.Freshness], in: "live", wantErr: `domain: invalid freshness: unknown value "live"`},
		{name: "freshness empty", decode: unmarshalText[domain.Freshness], in: "", wantErr: `domain: invalid freshness: unknown value ""`},
		{name: "source", decode: unmarshalText[domain.Source], in: "dfm-mw", want: "dfm-mw"},
		{name: "source unknown", decode: unmarshalText[domain.Source], in: "yahoo", wantErr: `domain: invalid source: unknown value "yahoo"`},
		{name: "interval", decode: unmarshalText[domain.Interval], in: "1d", want: "1d"},
		{name: "interval unknown", decode: unmarshalText[domain.Interval], in: "5m", wantErr: `domain: invalid interval: unknown value "5m"`},
		{name: "class", decode: unmarshalText[domain.InstrumentClass], in: "reit", want: "reit"},
		{name: "class unknown", decode: unmarshalText[domain.InstrumentClass], in: "Equities", wantErr: `domain: invalid instrument class: unknown value "Equities"`},
		{name: "status", decode: unmarshalText[domain.InstrumentStatus], in: "suspended", want: "suspended"},
		{name: "status unknown", decode: unmarshalText[domain.InstrumentStatus], in: "A", wantErr: `domain: invalid instrument status: unknown value "A"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.decode(tt.in)
			checkErr(t, err, tt.wantErr)
			if got != tt.want {
				t.Errorf("UnmarshalText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEnumBind(t *testing.T) {
	tests := []struct {
		name    string
		bind    func(string) (string, error)
		in      string
		want    string
		wantErr string
	}{
		{name: "exchange", bind: bind[domain.Exchange], in: "NASDAQ_DUBAI", want: "NASDAQ_DUBAI"},
		{name: "exchange unknown", bind: bind[domain.Exchange], in: "ADX", wantErr: `domain: invalid exchange: unknown value "ADX"`},
		{name: "currency", bind: bind[domain.Currency], in: "USD", want: "USD"},
		{name: "currency unknown", bind: bind[domain.Currency], in: "EUR", wantErr: `domain: invalid currency: unknown value "EUR"`},
		{name: "freshness", bind: bind[domain.Freshness], in: "realtime", want: "realtime"},
		{name: "freshness unknown", bind: bind[domain.Freshness], in: "stale", wantErr: `domain: invalid freshness: unknown value "stale"`},
		{name: "source", bind: bind[domain.Source], in: "ibkr", want: "ibkr"},
		{name: "source unknown", bind: bind[domain.Source], in: "bogus", wantErr: `domain: invalid source: unknown value "bogus"`},
		{name: "interval", bind: bind[domain.Interval], in: "1m", want: "1m"},
		{name: "interval unknown", bind: bind[domain.Interval], in: "5m", wantErr: `domain: invalid interval: unknown value "5m"`},
		{name: "interval empty", bind: bind[domain.Interval], in: "", wantErr: `domain: invalid interval: unknown value ""`},
		{name: "class", bind: bind[domain.InstrumentClass], in: "sukuk", want: "sukuk"},
		{name: "class unknown", bind: bind[domain.InstrumentClass], in: "index", wantErr: `domain: invalid instrument class: unknown value "index"`},
		{name: "status", bind: bind[domain.InstrumentStatus], in: "delisted", want: "delisted"},
		{name: "status unknown", bind: bind[domain.InstrumentStatus], in: "halted", wantErr: `domain: invalid instrument status: unknown value "halted"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.bind(tt.in)
			checkErr(t, err, tt.wantErr)
			if got != tt.want {
				t.Errorf("Bind(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func bind[T ~string, P interface {
	*T
	Bind(string) error
}](s string) (string, error) {
	var v T
	err := P(&v).Bind(s)
	return string(v), err
}

func unmarshalText[T ~string, P interface {
	*T
	encoding.TextUnmarshaler
}](s string) (string, error) {
	var v T
	err := P(&v).UnmarshalText([]byte(s))
	return string(v), err
}

func TestEnumTextMarshal(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{name: "exchange", v: domain.ExchangeDFM, want: "DFM"},
		{name: "currency", v: domain.CurrencyAED, want: "AED"},
		{name: "freshness", v: domain.EndOfDay, want: "eod"},
		{name: "source", v: domain.SourceSelfRecorded, want: "self-recorded"},
		{name: "interval", v: domain.OneDay, want: "1d"},
		{name: "class", v: domain.ClassETF, want: "etf"},
		{name: "status", v: domain.StatusMatured, want: "matured"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, ok := tt.v.(encoding.TextMarshaler)
			if !ok {
				t.Fatalf("%T does not implement encoding.TextMarshaler", tt.v)
			}
			got, err := m.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("MarshalText() = %q, want %q", got, tt.want)
			}
		})
	}
}

type enums struct {
	Exchange  domain.Exchange
	Currency  domain.Currency
	Freshness domain.Freshness
	Source    domain.Source
	Interval  domain.Interval
	Class     domain.InstrumentClass
	Status    domain.InstrumentStatus
}

func TestEnumJSON(t *testing.T) {
	const valid = `{"Exchange":"NASDAQ_DUBAI","Currency":"USD","Freshness":"realtime","Source":"ibkr","Interval":"1m","Class":"sukuk","Status":"listed"}`
	want := enums{
		Exchange:  domain.ExchangeNasdaqDubai,
		Currency:  domain.CurrencyUSD,
		Freshness: domain.RealTime,
		Source:    domain.SourceIBKR,
		Interval:  domain.OneMinute,
		Class:     domain.ClassSukuk,
		Status:    domain.StatusListed,
	}
	var got enums
	if err := json.Unmarshal([]byte(valid), &got); err != nil {
		t.Fatalf("Unmarshal(valid) error = %v", err)
	}
	if got != want {
		t.Errorf("Unmarshal(valid) = %+v, want %+v", got, want)
	}
	out, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if string(out) != valid {
		t.Errorf("Marshal = %s, want %s", out, valid)
	}

	tests := []struct {
		name string
		in   string
	}{
		{name: "exchange", in: `{"Exchange":"ADX"}`},
		{name: "currency", in: `{"Currency":"EUR"}`},
		{name: "freshness", in: `{"Freshness":"live"}`},
		{name: "source", in: `{"Source":"yahoo"}`},
		{name: "interval", in: `{"Interval":"5m"}`},
		{name: "class", in: `{"Class":"Equities"}`},
		{name: "status", in: `{"Status":"A"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v enums
			err := json.Unmarshal([]byte(tt.in), &v)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("Unmarshal(%s) error = %v, want ErrInvalid", tt.in, err)
			}
		})
	}
}

func TestImportsOnlyStdlibAndDecimal(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var imports []string
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(imports, path) {
				imports = append(imports, path)
			}
		}
	}
	slices.Sort(imports)
	want := []string{"errors", "fmt", "github.com/shopspring/decimal", "strings", "time"}
	if !slices.Equal(imports, want) {
		t.Errorf("imports = %q, want %q", imports, want)
	}
}

func checkErr(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("error = nil, want %q", want)
	}
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("errors.Is(%v, ErrInvalid) = false, want true", err)
	}
}
