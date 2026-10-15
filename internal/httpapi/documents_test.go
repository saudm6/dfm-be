package httpapi_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/documents"
	"github.com/saudm6/dfm-be/internal/httpapi"
)

// docResponse is a response from a /me/documents route.
type docResponse struct {
	status int
	etag   string
	body   string
}

// docRequest is a request to a /me/documents route by a signed-in user.
type docRequest struct {
	method  string
	path    string
	ifMatch string // sent when not empty; noIfMatch sends none
	body    string
}

// docClient sends requests as one signed-in user.
type docClient struct {
	t      *testing.T
	srv    *httptest.Server
	cookie *http.Cookie
	csrf   string
}

func (c docClient) do(r docRequest) docResponse {
	c.t.Helper()
	var rd io.Reader
	if r.body != "" {
		rd = strings.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(c.t.Context(), r.method, c.srv.URL+r.path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	if r.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	if r.ifMatch != "" {
		req.Header.Set("If-Match", r.ifMatch)
	}
	resp, err := c.srv.Client().Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", r.method, r.path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return docResponse{status: resp.StatusCode, etag: resp.Header.Get("ETag"), body: string(b)}
}

func (c docClient) expect(r docRequest, want docResponse) {
	c.t.Helper()
	got := c.do(r)
	if got.body != want.body+"\n" && got.body != want.body {
		c.t.Errorf("%s %s If-Match %q: body = %s, want %s", r.method, r.path, r.ifMatch, got.body, want.body)
	}
	if got.status != want.status || got.etag != want.etag {
		c.t.Errorf("%s %s If-Match %q = %d ETag %q, want %d ETag %q", r.method, r.path, r.ifMatch, got.status, got.etag, want.status, want.etag)
	}
}

// newDocumentServer serves the API with the auth fixture's users and a
// document store whose clock is the fixture's.
func newDocumentServer(t *testing.T) (*httptest.Server, authFixture) {
	t.Helper()
	f := newAuthFixture(t)
	srv := httptest.NewServer(httpapi.NewRouter(slog.New(slog.DiscardHandler), fakeDB{}, fakeCalendars{}, at(thursday), f.sessions, documents.NewStore(f.pool, f.clock.Now), nil))
	t.Cleanup(srv.Close)
	return srv, f
}

func signInDocs(t *testing.T, srv *httptest.Server, username string) docClient {
	t.Helper()
	cookie, csrf := login(t, srv, username)
	return docClient{t: t, srv: srv, cookie: cookie, csrf: csrf}
}

const (
	morning = "/me/documents/layout/Morning"
	// Entity tags of {"panes":[1,2]} and {"panes":[3]} at schema version 1.
	etagPanes12 = `"0fb3fad7ded9711f4674bc1dcfaf1e2b"`
	etagPanes3  = `"f3558a517958a17752040d4fa606d938"`
)

func TestDocumentLifecycle(t *testing.T) {
	srv, f := newDocumentServer(t)
	saud := signInDocs(t, srv, "saud")

	saud.expect(docRequest{method: http.MethodGet, path: morning},
		docResponse{status: 404, body: `{"code":"not_found","message":"no layout named \"Morning\""}`})
	saud.expect(docRequest{method: http.MethodPut, path: morning, body: `{"schemaVersion":1,"body":{"panes": [1, 2]}}`},
		docResponse{status: 201, etag: etagPanes12, body: `{"name":"Morning","etag":"\"0fb3fad7ded9711f4674bc1dcfaf1e2b\"","updatedAt":"2026-10-15T09:00:00Z"}`})
	saud.expect(docRequest{method: http.MethodGet, path: morning},
		docResponse{status: 200, etag: etagPanes12, body: `{"kind":"layout","name":"Morning","schemaVersion":1,"body":{"panes":[1,2]}}`})
	saud.expect(docRequest{method: http.MethodPut, path: morning, body: `{"schemaVersion":1,"body":{"panes":[9]}}`},
		docResponse{status: 428, body: `{"code":"precondition_required","message":"layout \"Morning\" exists; send If-Match with its ETag to replace it"}`})

	f.clock.Advance(time.Minute)
	saud.expect(docRequest{method: http.MethodPut, path: morning, ifMatch: etagPanes12, body: `{"schemaVersion":1,"body":{"panes":[3]}}`},
		docResponse{status: 200, etag: etagPanes3, body: `{"name":"Morning","etag":"\"f3558a517958a17752040d4fa606d938\"","updatedAt":"2026-10-15T09:01:00Z"}`})

	// The Done-when check: a write with the replaced (stale) ETag is refused
	// and leaves the document as it was.
	saud.expect(docRequest{method: http.MethodPut, path: morning, ifMatch: etagPanes12, body: `{"schemaVersion":1,"body":{"panes":[4]}}`},
		docResponse{status: 412, body: `{"code":"precondition_failed","message":"layout \"Morning\" has changed or does not exist; get it again"}`})
	saud.expect(docRequest{method: http.MethodGet, path: morning},
		docResponse{status: 200, etag: etagPanes3, body: `{"kind":"layout","name":"Morning","schemaVersion":1,"body":{"panes":[3]}}`})

	saud.expect(docRequest{method: http.MethodPut, path: "/me/documents/chart_template/Candles%20v2", body: `{"schemaVersion":3,"body":{}}`},
		docResponse{status: 201, etag: `"ce578b6742b5c697b639be3cce3edab7"`, body: `{"name":"Candles v2","etag":"\"ce578b6742b5c697b639be3cce3edab7\"","updatedAt":"2026-10-15T09:01:00Z"}`})
	saud.expect(docRequest{method: http.MethodGet, path: "/me/documents/layout"},
		docResponse{status: 200, body: `{"documents":[{"name":"Morning","etag":"\"f3558a517958a17752040d4fa606d938\"","updatedAt":"2026-10-15T09:01:00Z"}]}`})
	saud.expect(docRequest{method: http.MethodGet, path: "/me/documents/chart_template"},
		docResponse{status: 200, body: `{"documents":[{"name":"Candles v2","etag":"\"ce578b6742b5c697b639be3cce3edab7\"","updatedAt":"2026-10-15T09:01:00Z"}]}`})

	saud.expect(docRequest{method: http.MethodDelete, path: morning},
		docResponse{status: 428, body: `{"code":"precondition_required","message":"send If-Match with the document's ETag to delete it"}`})
	saud.expect(docRequest{method: http.MethodDelete, path: morning, ifMatch: etagPanes12},
		docResponse{status: 412, body: `{"code":"precondition_failed","message":"layout \"Morning\" has changed or does not exist; get it again"}`})
	saud.expect(docRequest{method: http.MethodDelete, path: morning, ifMatch: `W/` + etagPanes3},
		docResponse{status: 412, body: `{"code":"precondition_failed","message":"layout \"Morning\" has changed or does not exist; get it again"}`})
	saud.expect(docRequest{method: http.MethodDelete, path: morning, ifMatch: `"x", ` + etagPanes3},
		docResponse{status: 204})
	saud.expect(docRequest{method: http.MethodDelete, path: morning, ifMatch: etagPanes3},
		docResponse{status: 404, body: `{"code":"not_found","message":"no layout named \"Morning\""}`})
	saud.expect(docRequest{method: http.MethodPut, path: morning, ifMatch: "*", body: `{"schemaVersion":1,"body":{}}`},
		docResponse{status: 412, body: `{"code":"precondition_failed","message":"layout \"Morning\" has changed or does not exist; get it again"}`})
	saud.expect(docRequest{method: http.MethodGet, path: "/me/documents/layout"},
		docResponse{status: 200, body: `{"documents":[]}`})
}

func TestDocumentsAreOwnedByOneUser(t *testing.T) {
	srv, _ := newDocumentServer(t)
	saud := signInDocs(t, srv, "saud")
	amal := signInDocs(t, srv, "amal")

	saud.expect(docRequest{method: http.MethodPut, path: morning, body: `{"schemaVersion":1,"body":{"panes":[1,2]}}`},
		docResponse{status: 201, etag: etagPanes12, body: `{"name":"Morning","etag":"\"0fb3fad7ded9711f4674bc1dcfaf1e2b\"","updatedAt":"2026-10-15T09:00:00Z"}`})

	amal.expect(docRequest{method: http.MethodGet, path: morning},
		docResponse{status: 404, body: `{"code":"not_found","message":"no layout named \"Morning\""}`})
	amal.expect(docRequest{method: http.MethodGet, path: "/me/documents/layout"},
		docResponse{status: 200, body: `{"documents":[]}`})
	amal.expect(docRequest{method: http.MethodPut, path: morning, ifMatch: etagPanes12, body: `{"schemaVersion":1,"body":{"panes":[3]}}`},
		docResponse{status: 412, body: `{"code":"precondition_failed","message":"layout \"Morning\" has changed or does not exist; get it again"}`})
	amal.expect(docRequest{method: http.MethodDelete, path: morning, ifMatch: etagPanes12},
		docResponse{status: 404, body: `{"code":"not_found","message":"no layout named \"Morning\""}`})
	amal.expect(docRequest{method: http.MethodPut, path: morning, body: `{"schemaVersion":1,"body":{"panes":[3]}}`},
		docResponse{status: 201, etag: etagPanes3, body: `{"name":"Morning","etag":"\"f3558a517958a17752040d4fa606d938\"","updatedAt":"2026-10-15T09:00:00Z"}`})

	saud.expect(docRequest{method: http.MethodGet, path: morning},
		docResponse{status: 200, etag: etagPanes12, body: `{"kind":"layout","name":"Morning","schemaVersion":1,"body":{"panes":[1,2]}}`})
	amal.expect(docRequest{method: http.MethodGet, path: morning},
		docResponse{status: 200, etag: etagPanes3, body: `{"kind":"layout","name":"Morning","schemaVersion":1,"body":{"panes":[3]}}`})
}

func TestDocumentRequestsNeedSessionAndCSRF(t *testing.T) {
	srv, _ := newDocumentServer(t)
	saud := signInDocs(t, srv, "saud")
	anonymous := docClient{t: t, srv: srv}
	noCSRF := docClient{t: t, srv: srv, cookie: saud.cookie}
	put := `{"schemaVersion":1,"body":{}}`

	for _, r := range []docRequest{
		{method: http.MethodGet, path: "/me/documents/layout"},
		{method: http.MethodGet, path: morning},
		{method: http.MethodPut, path: morning, body: put},
		{method: http.MethodDelete, path: morning, ifMatch: "*"},
	} {
		anonymous.expect(r, docResponse{status: 401, body: `{"code":"unauthorized","message":"sign in to use ` + r.path + `"}`})
	}
	for _, r := range []docRequest{
		{method: http.MethodPut, path: morning, body: put},
		{method: http.MethodDelete, path: morning, ifMatch: "*"},
	} {
		noCSRF.expect(r, docResponse{status: 403, body: `{"code":"forbidden","message":"missing or invalid X-CSRF-Token header"}`})
	}
	saud.expect(docRequest{method: http.MethodGet, path: "/me/documents/layout"}, docResponse{status: 200, body: `{"documents":[]}`})
}

func TestDocumentRequestValidation(t *testing.T) {
	srv, _ := newDocumentServer(t)
	saud := signInDocs(t, srv, "saud")
	saud.expect(docRequest{method: http.MethodPut, path: morning, body: `{"schemaVersion":1,"body":{}}`},
		docResponse{status: 201, etag: `"2abb749635de1a7f1d45ce3a27636ef3"`, body: `{"name":"Morning","etag":"\"2abb749635de1a7f1d45ce3a27636ef3\"","updatedAt":"2026-10-15T09:00:00Z"}`})

	nameErr := `{"code":"bad_request","message":"parameter name: invalid document name: \"a/b\" must be 1 to 64 characters of A-Z, a-z, 0-9, space, '_', '.' and '-', starting with a letter or digit and not ending with a space"}`
	tests := []struct {
		name string
		req  docRequest
		want docResponse
	}{
		{name: "unknown kind", req: docRequest{method: http.MethodGet, path: "/me/documents/notes/x"},
			want: docResponse{status: 400, body: `{"code":"bad_request","message":"parameter kind: invalid document kind: unknown value \"notes\""}`}},
		{name: "unknown kind in list", req: docRequest{method: http.MethodGet, path: "/me/documents/Layout"},
			want: docResponse{status: 400, body: `{"code":"bad_request","message":"parameter kind: invalid document kind: unknown value \"Layout\""}`}},
		{name: "bad name on get", req: docRequest{method: http.MethodGet, path: "/me/documents/layout/a%2Fb"},
			want: docResponse{status: 400, body: nameErr}},
		{name: "bad name on put", req: docRequest{method: http.MethodPut, path: "/me/documents/layout/a%2Fb", body: `{"schemaVersion":1,"body":{}}`},
			want: docResponse{status: 400, body: nameErr}},
		{name: "bad name on delete", req: docRequest{method: http.MethodDelete, path: "/me/documents/layout/a%2Fb", ifMatch: "*"},
			want: docResponse{status: 400, body: nameErr}},
		{name: "array body", req: docRequest{method: http.MethodPut, path: "/me/documents/layout/x", body: `{"schemaVersion":1,"body":[1]}`},
			want: docResponse{status: 400, body: `{"code":"bad_request","message":"invalid body: must be a JSON object"}`}},
		{name: "null body", req: docRequest{method: http.MethodPut, path: "/me/documents/layout/x", body: `{"schemaVersion":1,"body":null}`},
			want: docResponse{status: 400, body: `{"code":"bad_request","message":"invalid body: must be a JSON object"}`}},
		{name: "missing body", req: docRequest{method: http.MethodPut, path: "/me/documents/layout/x", body: `{"schemaVersion":1}`},
			want: docResponse{status: 400, body: `{"code":"bad_request","message":"invalid body: must be a JSON object"}`}},
		{name: "zero schema version", req: docRequest{method: http.MethodPut, path: "/me/documents/layout/x", body: `{"schemaVersion":0,"body":{}}`},
			want: docResponse{status: 400, body: `{"code":"bad_request","message":"invalid schemaVersion: must be a positive integer, got 0"}`}},
		{name: "fractional schema version", req: docRequest{method: http.MethodPut, path: "/me/documents/layout/x", body: `{"schemaVersion":1.5,"body":{}}`},
			want: docResponse{status: 400, body: `{"code":"bad_request","message":"request body: field schemaVersion has the wrong type"}`}},
		{name: "not JSON", req: docRequest{method: http.MethodPut, path: "/me/documents/layout/x", body: `{`},
			want: docResponse{status: 400, body: `{"code":"bad_request","message":"request body is not valid JSON"}`}},
		{name: "bad If-Match on put", req: docRequest{method: http.MethodPut, path: morning, ifMatch: "abc", body: `{"schemaVersion":1,"body":{}}`},
			want: docResponse{status: 400, body: `{"code":"bad_request","message":"invalid If-Match: must be * or a comma-separated list of quoted entity tags"}`}},
		{name: "bad If-Match on delete", req: docRequest{method: http.MethodDelete, path: morning, ifMatch: `"a`},
			want: docResponse{status: 400, body: `{"code":"bad_request","message":"invalid If-Match: must be * or a comma-separated list of quoted entity tags"}`}},
		{name: "body over the limit", req: docRequest{method: http.MethodPut, path: "/me/documents/layout/x", body: `{"schemaVersion":1,"body":{"a":"` + strings.Repeat("x", 256<<10) + `"}}`},
			want: docResponse{status: 413, body: `{"code":"content_too_large","message":"request body is larger than 262144 bytes"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saud.t = t
			saud.expect(tt.req, tt.want)
		})
	}
	saud.t = t
	saud.expect(docRequest{method: http.MethodGet, path: "/me/documents/layout"},
		docResponse{status: 200, body: `{"documents":[{"name":"Morning","etag":"\"2abb749635de1a7f1d45ce3a27636ef3\"","updatedAt":"2026-10-15T09:00:00Z"}]}`})
}

func TestDocumentBodyAtTheLimit(t *testing.T) {
	srv, _ := newDocumentServer(t)
	saud := signInDocs(t, srv, "saud")
	prefix, suffix := `{"schemaVersion":1,"body":{"a":"`, `"}}`
	body := prefix + strings.Repeat("x", 256<<10-len(prefix)-len(suffix)) + suffix
	if got := saud.do(docRequest{method: http.MethodPut, path: "/me/documents/layout/big", body: body}); got.status != http.StatusCreated {
		t.Fatalf("PUT of a %d byte request = %d %s, want 201", len(body), got.status, got.body)
	}
	got := saud.do(docRequest{method: http.MethodGet, path: "/me/documents/layout/big"})
	want := `{"kind":"layout","name":"big","schemaVersion":1,"body":{"a":"` + strings.Repeat("x", 256<<10-len(prefix)-len(suffix)) + `"}}` + "\n"
	if got.status != http.StatusOK || got.body != want {
		t.Errorf("GET = %d with a %d byte body, want 200 with %d bytes", got.status, len(got.body), len(want))
	}
}
