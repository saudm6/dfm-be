package api_test

import (
	"slices"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/saudm6/dfm-be/api"
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
		{"Currency", []string{"AED", "USD"}, parser(domain.ParseCurrency)},
		{"Freshness", []string{"realtime", "delayed", "eod"}, parser(domain.ParseFreshness)},
		{"Source", []string{"ibkr", "dfm-mw", "dfm-history", "self-recorded"}, parser(domain.ParseSource)},
		{"Interval", []string{"1m", "1d"}, parser(domain.ParseInterval)},
		{"InstrumentClass", []string{"equity", "etf", "reit", "fund", "bond", "sukuk", "future"}, parser(domain.ParseInstrumentClass)},
		{"InstrumentStatus", []string{"listed", "suspended", "delisted", "matured"}, parser(domain.ParseInstrumentStatus)},
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
