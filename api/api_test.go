package api_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/saudm6/dfm-be/api"
	"github.com/saudm6/dfm-be/internal/auth"
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
		{"Currency", []string{"AED", "USD"}, parser(domain.ParseCurrency)},
		{"Freshness", []string{"realtime", "delayed", "eod"}, parser(domain.ParseFreshness)},
		{"Source", []string{"ibkr", "dfm-mw", "dfm-history", "self-recorded"}, parser(domain.ParseSource)},
		{"Interval", []string{"1m", "1d"}, parser(domain.ParseInterval)},
		{"InstrumentClass", []string{"equity", "etf", "reit", "fund", "bond", "sukuk", "future"}, parser(domain.ParseInstrumentClass)},
		{"InstrumentStatus", []string{"listed", "suspended", "delisted", "matured"}, parser(domain.ParseInstrumentStatus)},
		{"Role", []string{"owner", "member"}, parser(auth.ParseRole)},
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

type operation struct {
	key      string // "METHOD /path"
	id       string
	security openapi3.SecurityRequirements
}

// operations returns every operation of doc sorted by key, with the
// document's security requirements applied to operations that declare none.
func operations(doc *openapi3.T) []operation {
	var ops []operation
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			sec := doc.Security
			if op.Security != nil {
				sec = *op.Security
			}
			ops = append(ops, operation{key: method + " " + path, id: op.OperationID, security: sec})
		}
	}
	slices.SortFunc(ops, func(a, b operation) int { return strings.Compare(a.key, b.key) })
	return ops
}

// publicOperationProblems checks doc against public, the IDs of the
// operations the auth middleware serves without a session. A listed
// operation must exist and declare `security: []`; every other operation
// must require sessionCookie in each of its security requirements, and its
// path must not equal a public operation's path once a trailing "/" is
// trimmed from both, because chi's RoutePattern() trims it and /x/ would
// then pass for /x.
func publicOperationProblems(doc *openapi3.T, public []string) []string {
	var problems []string
	seen := map[string]bool{}
	publicPaths := map[string]string{}
	for _, op := range operations(doc) {
		if slices.Contains(public, op.id) {
			_, path, _ := strings.Cut(op.key, " ")
			publicPaths[strings.TrimSuffix(path, "/")] = path
		}
	}
	for _, op := range operations(doc) {
		if slices.Contains(public, op.id) {
			seen[op.id] = true
			if len(op.security) != 0 {
				problems = append(problems, fmt.Sprintf("%s (%s) is public but has security %v, want []", op.key, op.id, op.security))
			}
			continue
		}
		_, path, _ := strings.Cut(op.key, " ")
		if pub, ok := publicPaths[strings.TrimSuffix(path, "/")]; ok && pub != path {
			problems = append(problems, fmt.Sprintf("%s (%s) is not public but its path differs from public path %s only by a trailing \"/\"", op.key, op.id, pub))
		}
		if len(op.security) == 0 {
			problems = append(problems, fmt.Sprintf("%s (%s) has no security requirement but is not public", op.key, op.id))
		}
		for _, req := range op.security {
			if _, ok := req["sessionCookie"]; !ok {
				problems = append(problems, fmt.Sprintf("%s (%s) has security requirement %v without sessionCookie but is not public", op.key, op.id, req))
			}
		}
	}
	for _, id := range public {
		if !seen[id] {
			problems = append(problems, fmt.Sprintf("public operation %s is not in the spec", id))
		}
	}
	return problems
}

// The auth middleware serves exactly api.PublicOperations without a session;
// the spec must say the same to the generated client.
func TestSpecPublicOperations(t *testing.T) {
	if want := []string{"getHealth", "getOpenAPISpec", "login"}; !slices.Equal(api.PublicOperations, want) {
		t.Errorf("api.PublicOperations = %q, want %q", api.PublicOperations, want)
	}
	for _, p := range publicOperationProblems(loadSpec(t), api.PublicOperations) {
		t.Error(p)
	}
}

func TestPublicOperationProblems(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData([]byte(`
openapi: 3.1.0
info: {title: t, version: "0"}
security:
  - sessionCookie: []
paths:
  /login:
    post: {operationId: login, security: [], responses: {"204": {description: ok}}}
  /me:
    get: {operationId: getMe, responses: {"204": {description: ok}}}
    post: {operationId: postMe, security: [{sessionCookie: [], csrfToken: []}], responses: {"204": {description: ok}}}
  /login/:
    post: {operationId: postLoginSlash, security: [{sessionCookie: [], csrfToken: []}], responses: {"204": {description: ok}}}
  /scratch:
    get: {operationId: getScratch, security: [{}], responses: {"204": {description: ok}}}
    post: {operationId: postScratch, security: [{csrfToken: []}], responses: {"204": {description: ok}}}
    put: {operationId: putScratch, security: [], responses: {"204": {description: ok}}}
components:
  securitySchemes:
    sessionCookie: {type: apiKey, in: cookie, name: dfm_session}
    csrfToken: {type: apiKey, in: header, name: X-CSRF-Token}
`))
	if err != nil {
		t.Fatal(err)
	}

	got := publicOperationProblems(doc, []string{"login", "getMe", "gone"})

	want := []string{
		"GET /me (getMe) is public but has security [map[sessionCookie:[]]], want []",
		"GET /scratch (getScratch) has security requirement map[] without sessionCookie but is not public",
		"POST /login/ (postLoginSlash) is not public but its path differs from public path /login only by a trailing \"/\"",
		"POST /scratch (postScratch) has security requirement map[csrfToken:[]] without sessionCookie but is not public",
		"PUT /scratch (putScratch) has no security requirement but is not public",
		"public operation gone is not in the spec",
	}
	if !slices.Equal(got, want) {
		t.Errorf("publicOperationProblems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The middleware checks the CSRF token on every unsafe method; the spec must
// say so for the generated client.
func TestSpecUnsafeOperationsNeedCSRFToken(t *testing.T) {
	for _, op := range operations(loadSpec(t)) {
		method, _, _ := strings.Cut(op.key, " ")
		if len(op.security) == 0 || method == "GET" || method == "HEAD" || method == "OPTIONS" {
			continue
		}
		for _, req := range op.security {
			if _, ok := req["csrfToken"]; !ok {
				t.Errorf("%s: security requirement %v lacks csrfToken", op.key, req)
			}
		}
	}
}

// sessionScopeProblems reports operations with more than one security
// requirement object, because the generated code keeps only the last
// requirement's sessionCookie scopes, and sessionCookie scopes the
// middleware does not know, because it answers those with 500.
func sessionScopeProblems(doc *openapi3.T) []string {
	var problems []string
	for _, op := range operations(doc) {
		if len(op.security) > 1 {
			problems = append(problems, fmt.Sprintf("%s has %d security requirement objects, want at most 1", op.key, len(op.security)))
		}
		for _, req := range op.security {
			for _, scope := range req["sessionCookie"] {
				if scope != "marketData" {
					problems = append(problems, fmt.Sprintf("%s: unknown sessionCookie scope %q", op.key, scope))
				}
			}
		}
	}
	return problems
}

func TestSpecSessionScopesAreKnown(t *testing.T) {
	for _, p := range sessionScopeProblems(loadSpec(t)) {
		t.Error(p)
	}
}

func TestSessionScopeProblems(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData([]byte(`
openapi: 3.1.0
info: {title: t, version: "0"}
paths:
  /quotes:
    get: {operationId: listQuotes, security: [{sessionCookie: [marketData]}], responses: {"204": {description: ok}}}
  /either:
    get: {operationId: getEither, security: [{sessionCookie: [marketData]}, {sessionCookie: []}], responses: {"204": {description: ok}}}
  /admin:
    get: {operationId: getAdmin, security: [{sessionCookie: [admin]}], responses: {"204": {description: ok}}}
components:
  securitySchemes:
    sessionCookie: {type: apiKey, in: cookie, name: dfm_session}
`))
	if err != nil {
		t.Fatal(err)
	}

	got := sessionScopeProblems(doc)

	want := []string{
		`GET /admin: unknown sessionCookie scope "admin"`,
		"GET /either has 2 security requirement objects, want at most 1",
	}
	if !slices.Equal(got, want) {
		t.Errorf("sessionScopeProblems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
