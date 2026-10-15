package api_test

import (
	"slices"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/saudm6/dfm-be/api"
	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/domain"
)

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(api.OpenAPI)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	return doc
}

func TestSpecIsValidOpenAPI31(t *testing.T) {
	doc := loadSpec(t)

	if doc.OpenAPI != "3.1.0" {
		t.Errorf("openapi = %q, want %q", doc.OpenAPI, "3.1.0")
	}
	if err := doc.Validate(t.Context()); err != nil {
		t.Errorf("validate spec: %v", err)
	}
}

// The generated Go types alias the domain enums, so the spec's enum lists must
// hold exactly the values the domain parsers accept.
func TestSpecEnumsMatchDomain(t *testing.T) {
	tests := []struct {
		schema string
		want   []string
		parse  func(string) error
	}{
		{"Exchange", []string{"DFM", "NASDAQ_DUBAI"}, parser(domain.ParseExchange)},
		{"Currency", []string{"AED", "USD", "EUR", "GBP", "CNY", "AUD", "HKD", "NZD"}, parser(domain.ParseCurrency)},
		{"Freshness", []string{"realtime", "delayed", "eod"}, parser(domain.ParseFreshness)},
		{"Source", []string{"ibkr", "dfm-mw", "dfm-history", "self-recorded"}, parser(domain.ParseSource)},
		{"Interval", []string{"1m", "1d"}, parser(domain.ParseInterval)},
		{"InstrumentClass", []string{"equity", "etf", "reit", "fund", "bond", "sukuk", "future", "index"}, parser(domain.ParseInstrumentClass)},
		{"InstrumentStatus", []string{"listed", "suspended", "delisted", "matured"}, parser(domain.ParseInstrumentStatus)},
		{"MarketSession", []string{"pre-open", "continuous", "closing-auction", "closed"}, parser(calendar.ParseSession)},
	}
	schemas := loadSpec(t).Components.Schemas
	for _, tt := range tests {
		t.Run(tt.schema, func(t *testing.T) {
			ref, ok := schemas[tt.schema]
			if !ok {
				t.Fatalf("schema %s missing", tt.schema)
			}
			var got []string
			for _, v := range ref.Value.Enum {
				s, ok := v.(string)
				if !ok {
					t.Fatalf("enum value %v is %T, want string", v, v)
				}
				got = append(got, s)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("enum = %q, want %q", got, tt.want)
			}
			for _, s := range got {
				if err := tt.parse(s); err != nil {
					t.Errorf("domain rejects spec value %q: %v", s, err)
				}
			}
		})
	}
}

func parser[T any](parse func(string) (T, error)) func(string) error {
	return func(s string) error {
		_, err := parse(s)
		return err
	}
}

// The generated code binds a decimal.Decimal parameter as an exploded object,
// so it reads nothing and reports no error. Decimal parameters must be plain
// strings that the handler parses.
func TestSpecHasNoDecimalParameters(t *testing.T) {
	if got := decimalParams(loadSpec(t)); len(got) != 0 {
		t.Errorf("parameters using the Decimal schema: %q, want none", got)
	}
}

func TestDecimalParamsFindsDecimalParameter(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData([]byte(`
openapi: 3.1.0
info: {title: t, version: "0"}
paths:
  /quotes:
    get:
      parameters:
        - {name: minPrice, in: query, schema: {$ref: "#/components/schemas/Decimal"}}
        - {name: maxPrice, in: query, schema: {type: string}}
      responses:
        "200": {description: ok}
components:
  schemas:
    Decimal: {type: string, x-go-type: decimal.Decimal}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := decimalParams(doc), []string{"GET /quotes minPrice"}; !slices.Equal(got, want) {
		t.Errorf("decimalParams = %q, want %q", got, want)
	}
}

func decimalParams(doc *openapi3.T) []string {
	var found []string
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			for _, p := range append(slices.Clone(item.Parameters), op.Parameters...) {
				if s := p.Value.Schema; s != nil && s.Value != nil && s.Value.Extensions["x-go-type"] == "decimal.Decimal" {
					found = append(found, method+" "+path+" "+p.Value.Name)
				}
			}
		}
	}
	slices.Sort(found)
	return found
}
