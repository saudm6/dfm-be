package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/httpapi"
	"github.com/saudm6/dfm-be/internal/httpapi/paramtest"
	"github.com/saudm6/dfm-be/internal/store/storetest"
)

const password = "correct horse battery"

// clock is a settable time source shared with the server under test.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// authFixture is a database with the owner "saud" and the member "amal",
// and a session service on a settable clock with a 12 hour idle timeout and
// a 7 day maximum age.
type authFixture struct {
	pool     *pgxpool.Pool
	clock    *clock
	sessions *auth.Service
}

func newAuthFixture(t *testing.T) authFixture {
	t.Helper()
	pool := storetest.NewPool(t)
	c := &clock{now: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)}
	svc := auth.NewService(pool, auth.Options{IdleTimeout: 12 * time.Hour, MaxAge: 168 * time.Hour, Now: c.Now})
	for _, u := range []struct {
		name string
		role auth.Role
	}{{"saud", auth.Owner}, {"amal", auth.Member}} {
		if _, err := svc.CreateUser(t.Context(), u.name, password, u.role); err != nil {
			t.Fatal(err)
		}
	}
	return authFixture{pool: pool, clock: c, sessions: svc}
}

type response struct {
	status       int
	setCookie    string
	cacheControl string
	body         string
}

// call sends a request to srv. A body is sent as application/json, a
// non-nil cookie is sent, and csrf, when not empty, goes in the X-CSRF-Token
// header.
func call(t *testing.T, srv *httptest.Server, method, path string, cookie *http.Cookie, csrf, body string) response {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	return send(t, srv, req)
}

func send(t *testing.T, srv *httptest.Server, req *http.Request) response {
	t.Helper()
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{
		status:       resp.StatusCode,
		setCookie:    resp.Header.Get("Set-Cookie"),
		cacheControl: resp.Header.Get("Cache-Control"),
		body:         string(b),
	}
}

// login signs in through POST /auth/login and returns the session cookie and
// the CSRF token.
func login(t *testing.T, srv *httptest.Server, username string) (*http.Cookie, string) {
	t.Helper()
	resp := call(t, srv, http.MethodPost, "/auth/login", nil, "", `{"username":"`+username+`","password":"`+password+`"}`)
	if resp.status != http.StatusOK {
		t.Fatalf("login %s = %d %s, want 200", username, resp.status, resp.body)
	}
	cookie, err := http.ParseSetCookie(resp.setCookie)
	if err != nil {
		t.Fatalf("parse Set-Cookie %q: %v", resp.setCookie, err)
	}
	var me struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal([]byte(resp.body), &me); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: cookie.Name, Value: cookie.Value}, me.CSRFToken
}

func TestLoginMeLogout(t *testing.T) {
	f := newAuthFixture(t)
	srv := httptest.NewServer(httpapi.NewRouter(slog.New(slog.DiscardHandler), fakeDB{}, fakeCalendars{}, at(thursday), f.sessions, nil))
	defer srv.Close()

	resp := call(t, srv, http.MethodPost, "/auth/login", nil, "", `{"username":"Saud","password":"`+password+`"}`)

	if resp.status != http.StatusOK {
		t.Fatalf("login = %d %s, want 200", resp.status, resp.body)
	}
	cookie, err := http.ParseSetCookie(resp.setCookie)
	if err != nil {
		t.Fatal(err)
	}
	wantSetCookie := "dfm_session=" + cookie.Value + "; Path=/; Max-Age=604800; HttpOnly; Secure; SameSite=Strict"
	if resp.setCookie != wantSetCookie {
		t.Errorf("Set-Cookie = %q, want %q", resp.setCookie, wantSetCookie)
	}
	if resp.cacheControl != "no-store" {
		t.Errorf("login Cache-Control = %q, want %q", resp.cacheControl, "no-store")
	}
	var me struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal([]byte(resp.body), &me); err != nil {
		t.Fatal(err)
	}
	wantMe := `{"id":1,"username":"saud","role":"owner","csrfToken":"` + me.CSRFToken + `"}` + "\n"
	if resp.body != wantMe || len(me.CSRFToken) != 43 {
		t.Errorf("login body = %q, want %q with a 43 character token", resp.body, wantMe)
	}
	session := &http.Cookie{Name: cookie.Name, Value: cookie.Value}

	if got := call(t, srv, http.MethodGet, "/me", session, "", ""); got.status != http.StatusOK || got.body != wantMe || got.cacheControl != "no-store" {
		t.Errorf("GET /me = %d %q Cache-Control %q, want 200 %q no-store", got.status, got.body, got.cacheControl, wantMe)
	}

	tests := []struct {
		name       string
		csrf       string
		wantStatus int
		wantBody   string
	}{
		{"logout without CSRF token", "", http.StatusForbidden, `{"code":"forbidden","message":"missing or invalid X-CSRF-Token header"}` + "\n"},
		{"logout with wrong CSRF token", "x" + me.CSRFToken[1:], http.StatusForbidden, `{"code":"forbidden","message":"missing or invalid X-CSRF-Token header"}` + "\n"},
	}
	for _, tt := range tests {
		got := call(t, srv, http.MethodPost, "/auth/logout", session, tt.csrf, "")
		if got.status != tt.wantStatus || got.body != tt.wantBody {
			t.Errorf("%s = %d %q, want %d %q", tt.name, got.status, got.body, tt.wantStatus, tt.wantBody)
		}
	}

	got := call(t, srv, http.MethodPost, "/auth/logout", session, me.CSRFToken, "")
	if got.status != http.StatusNoContent || got.body != "" {
		t.Errorf("logout = %d %q, want 204 with no body", got.status, got.body)
	}
	if want := "dfm_session=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Strict"; got.setCookie != want {
		t.Errorf("logout Set-Cookie = %q, want %q", got.setCookie, want)
	}
	var rows int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("sessions after logout = %d, want 0", rows)
	}

	wantUnauthorized := `{"code":"unauthorized","message":"sign in to use /me"}` + "\n"
	if got := call(t, srv, http.MethodGet, "/me", session, "", ""); got.status != http.StatusUnauthorized || got.body != wantUnauthorized {
		t.Errorf("GET /me after logout = %d %q, want 401 %q", got.status, got.body, wantUnauthorized)
	}
}

func TestLoginFailures(t *testing.T) {
	f := newAuthFixture(t)
	srv := httptest.NewServer(httpapi.NewRouter(slog.New(slog.DiscardHandler), fakeDB{}, fakeCalendars{}, at(thursday), f.sessions, nil))
	defer srv.Close()
	invalid := `{"code":"unauthorized","message":"invalid username or password"}` + "\n"
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"wrong password", `{"username":"saud","password":"correct horse batter"}`, http.StatusUnauthorized, invalid},
		{"unknown user", `{"username":"nobody","password":"` + password + `"}`, http.StatusUnauthorized, invalid},
		{"missing fields", `{}`, http.StatusUnauthorized, invalid},
		{"not JSON", `username=saud`, http.StatusBadRequest, `{"code":"bad_request","message":"request body is not valid JSON"}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := call(t, srv, http.MethodPost, "/auth/login", nil, "", tt.body)

			if got.status != tt.wantStatus || got.body != tt.wantBody {
				t.Errorf("login = %d %q, want %d %q", got.status, got.body, tt.wantStatus, tt.wantBody)
			}
			if got.setCookie != "" {
				t.Errorf("Set-Cookie = %q, want none", got.setCookie)
			}
		})
	}
	var rows int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("sessions after failed logins = %d, want 0", rows)
	}
}

// Login refuses bodies that are not JSON or are too large before it checks
// the password, so a cross-site form cannot sign a browser in and large
// input never reaches the password hash.
func TestLoginRequestLimits(t *testing.T) {
	f := newAuthFixture(t)
	srv := httptest.NewServer(httpapi.NewRouter(slog.New(slog.DiscardHandler), fakeDB{}, fakeCalendars{}, at(thursday), f.sessions, nil))
	defer srv.Close()
	body := `{"username":"saud","password":"` + password + `"}`
	unsupported := `{"code":"unsupported_media_type","message":"send the login request as application/json"}` + "\n"
	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
		wantBody    string
	}{
		{"no content type", "", body, http.StatusUnsupportedMediaType, unsupported},
		{"form", "application/x-www-form-urlencoded", "username=saud&password=x", http.StatusUnsupportedMediaType, unsupported},
		{"text", "text/plain", body, http.StatusUnsupportedMediaType, unsupported},
		{"malformed content type", "application/json; charset", body, http.StatusUnsupportedMediaType, unsupported},
		{
			"body over 4096 bytes", "application/json", `{"username":"saud","password":"` + strings.Repeat("x", 4096) + `"}`,
			http.StatusBadRequest, `{"code":"bad_request","message":"request body is larger than 4096 bytes"}` + "\n",
		},
		{"JSON with charset", "application/json; charset=utf-8", body, http.StatusOK, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/auth/login", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			got := send(t, srv, req)

			if got.status != tt.wantStatus || (tt.wantBody != "" && got.body != tt.wantBody) {
				t.Errorf("login = %d %q, want %d %q", got.status, got.body, tt.wantStatus, tt.wantBody)
			}
			if tt.wantStatus != http.StatusOK && got.setCookie != "" {
				t.Errorf("Set-Cookie = %q, want none", got.setCookie)
			}
		})
	}
	var rows int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("sessions = %d, want 1 (from the JSON login only)", rows)
	}
}

func TestLoginEndsSessionTheRequestCarried(t *testing.T) {
	f := newAuthFixture(t)
	srv := httptest.NewServer(httpapi.NewRouter(slog.New(slog.DiscardHandler), fakeDB{}, fakeCalendars{}, at(thursday), f.sessions, nil))
	defer srv.Close()
	old, _ := login(t, srv, "saud")

	resp := call(t, srv, http.MethodPost, "/auth/login", old, "", `{"username":"amal","password":"`+password+`"}`)

	if resp.status != http.StatusOK {
		t.Fatalf("login = %d %s, want 200", resp.status, resp.body)
	}
	if got := call(t, srv, http.MethodGet, "/me", old, "", ""); got.status != http.StatusUnauthorized {
		t.Errorf("GET /me with the old cookie = %d %q, want 401", got.status, got.body)
	}
	cookie, err := http.ParseSetCookie(resp.setCookie)
	if err != nil {
		t.Fatal(err)
	}
	got := call(t, srv, http.MethodGet, "/me", &http.Cookie{Name: cookie.Name, Value: cookie.Value}, "", "")
	if got.status != http.StatusOK || !strings.Contains(got.body, `"username":"amal"`) {
		t.Errorf("GET /me with the new cookie = %d %q, want 200 for amal", got.status, got.body)
	}
	var rows int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("sessions = %d, want 1", rows)
	}
}

func TestProtectedRouteRejectsMissingAndBadSessions(t *testing.T) {
	f := newAuthFixture(t)
	srv := httptest.NewServer(httpapi.NewRouter(slog.New(slog.DiscardHandler), fakeDB{}, fakeCalendars{}, at(thursday), f.sessions, nil))
	defer srv.Close()
	want := `{"code":"unauthorized","message":"sign in to use /me"}` + "\n"
	tests := []struct {
		name   string
		cookie *http.Cookie
	}{
		{"no cookie", nil},
		{"garbage cookie", &http.Cookie{Name: "dfm_session", Value: "garbage"}},
		{"unknown session", &http.Cookie{Name: "dfm_session", Value: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := call(t, srv, http.MethodGet, "/me", tt.cookie, "", ""); got.status != http.StatusUnauthorized || got.body != want {
				t.Errorf("GET /me = %d %q, want 401 %q", got.status, got.body, want)
			}
		})
	}
	// Logout needs a session before its CSRF token is checked.
	if got := call(t, srv, http.MethodPost, "/auth/logout", nil, "", ""); got.status != http.StatusUnauthorized {
		t.Errorf("POST /auth/logout without a session = %d %q, want 401", got.status, got.body)
	}
}

func TestExpiredSessionGets401(t *testing.T) {
	tests := []struct {
		name       string
		advance    []time.Duration
		wantStatus int
	}{
		{"active session", []time.Duration{11 * time.Hour}, http.StatusOK},
		{"idle for 12 hours", []time.Duration{12 * time.Hour}, http.StatusUnauthorized},
		{"active but older than 7 days", []time.Duration{
			11 * time.Hour, 11 * time.Hour, 11 * time.Hour, 11 * time.Hour, 11 * time.Hour, 11 * time.Hour, 11 * time.Hour, 11 * time.Hour,
			11 * time.Hour, 11 * time.Hour, 11 * time.Hour, 11 * time.Hour, 11 * time.Hour, 11 * time.Hour, 11 * time.Hour, 3 * time.Hour,
		}, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAuthFixture(t)
			srv := httptest.NewServer(httpapi.NewRouter(slog.New(slog.DiscardHandler), fakeDB{}, fakeCalendars{}, at(thursday), f.sessions, nil))
			defer srv.Close()
			session, _ := login(t, srv, "amal")

			var got response
			for _, d := range tt.advance {
				f.clock.Advance(d)
				got = call(t, srv, http.MethodGet, "/me", session, "", "")
			}

			if got.status != tt.wantStatus {
				t.Errorf("GET /me = %d %q, want %d", got.status, got.body, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusUnauthorized && got.body != `{"code":"unauthorized","message":"sign in to use /me"}`+"\n" {
				t.Errorf("GET /me body = %q, want the unauthorized Error body", got.body)
			}
		})
	}
}

func TestSessionLookupFailureIs500(t *testing.T) {
	f := newAuthFixture(t)
	var logs bytes.Buffer
	srv := httptest.NewServer(httpapi.NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), fakeDB{}, fakeCalendars{}, at(thursday), f.sessions, nil))
	defer srv.Close()
	session, _ := login(t, srv, "saud")
	f.pool.Close()

	got := call(t, srv, http.MethodGet, "/me", session, "", "")

	if want := `{"code":"internal","message":"internal server error"}` + "\n"; got.status != http.StatusInternalServerError || got.body != want {
		t.Errorf("GET /me = %d %q, want 500 %q", got.status, got.body, want)
	}
	if !strings.Contains(logs.String(), `"level":"ERROR","msg":"authenticate request","path":"/me","error":"auth: find session: closed pool"`) {
		t.Errorf("logs = %s, want an ERROR authenticate request line", logs.String())
	}
}

// securityServer serves the test-only contract's secured operations.
type securityServer struct {
	paramtest.StrictServerInterface
}

func (securityServer) ListQuotes(context.Context, paramtest.ListQuotesRequestObject) (paramtest.ListQuotesResponseObject, error) {
	return paramtest.ListQuotes204Response{}, nil
}

func (securityServer) PostMessage(context.Context, paramtest.PostMessageRequestObject) (paramtest.PostMessageResponseObject, error) {
	return paramtest.PostMessage204Response{}, nil
}

func (securityServer) PlaceOrderTrailingSlash(context.Context, paramtest.PlaceOrderTrailingSlashRequestObject) (paramtest.PlaceOrderTrailingSlashResponseObject, error) {
	return paramtest.PlaceOrderTrailingSlash204Response{}, nil
}

func (securityServer) GetUnlisted(context.Context, paramtest.GetUnlistedRequestObject) (paramtest.GetUnlistedResponseObject, error) {
	return paramtest.GetUnlisted204Response{}, nil
}

func (securityServer) PostUnlisted(context.Context, paramtest.PostUnlistedRequestObject) (paramtest.PostUnlistedResponseObject, error) {
	return paramtest.PostUnlisted204Response{}, nil
}

func TestRolesOnMarketDataAndChatRoutes(t *testing.T) {
	f := newAuthFixture(t)
	accounts := httptest.NewServer(httpapi.NewRouter(slog.New(slog.DiscardHandler), fakeDB{}, fakeCalendars{}, at(thursday), f.sessions, nil))
	defer accounts.Close()
	srv := httptest.NewServer(httpapi.NewParamTestRouter(slog.New(slog.DiscardHandler), securityServer{}, f.sessions))
	defer srv.Close()
	ownerCookie, ownerCSRF := login(t, accounts, "saud")
	memberCookie, memberCSRF := login(t, accounts, "amal")

	tests := []struct {
		name       string
		method     string
		path       string
		cookie     *http.Cookie
		csrf       string
		wantStatus int
		wantBody   string
	}{
		{"owner reads market data", http.MethodGet, "/quotes", ownerCookie, "", http.StatusNoContent, ""},
		{"member reads market data", http.MethodGet, "/quotes", memberCookie, "", http.StatusForbidden, `{"code":"forbidden","message":"account amal has no access to market data"}` + "\n"},
		{"anonymous reads market data", http.MethodGet, "/quotes", nil, "", http.StatusUnauthorized, `{"code":"unauthorized","message":"sign in to use /quotes"}` + "\n"},
		{"member chats", http.MethodPost, "/messages", memberCookie, memberCSRF, http.StatusNoContent, ""},
		{"owner chats", http.MethodPost, "/messages", ownerCookie, ownerCSRF, http.StatusNoContent, ""},
		{"member chats with owner's CSRF token", http.MethodPost, "/messages", memberCookie, ownerCSRF, http.StatusForbidden, `{"code":"forbidden","message":"missing or invalid X-CSRF-Token header"}` + "\n"},
		{"member chats without CSRF token", http.MethodPost, "/messages", memberCookie, "", http.StatusForbidden, `{"code":"forbidden","message":"missing or invalid X-CSRF-Token header"}` + "\n"},
		// Operations off the public list need a session whatever their security says.
		{"anonymous calls an op with an empty requirement", http.MethodGet, "/unlisted", nil, "", http.StatusUnauthorized, `{"code":"unauthorized","message":"sign in to use /unlisted"}` + "\n"},
		{"anonymous calls a CSRF-only op", http.MethodPost, "/unlisted", nil, "", http.StatusUnauthorized, `{"code":"unauthorized","message":"sign in to use /unlisted"}` + "\n"},
		{"anonymous calls a CSRF-only op with a token", http.MethodPost, "/unlisted", nil, memberCSRF, http.StatusUnauthorized, `{"code":"unauthorized","message":"sign in to use /unlisted"}` + "\n"},
		{"member calls an op with an empty requirement", http.MethodGet, "/unlisted", memberCookie, "", http.StatusNoContent, ""},
		{"member calls a CSRF-only op", http.MethodPost, "/unlisted", memberCookie, memberCSRF, http.StatusNoContent, ""},
		{"member calls a CSRF-only op without CSRF token", http.MethodPost, "/unlisted", memberCookie, "", http.StatusForbidden, `{"code":"forbidden","message":"missing or invalid X-CSRF-Token header"}` + "\n"},
		// POST /orders is public; POST /orders/ is not.
		{"anonymous posts to a public path plus /", http.MethodPost, "/orders/", nil, "", http.StatusUnauthorized, `{"code":"unauthorized","message":"sign in to use /orders/"}` + "\n"},
		{"member posts to a public path plus /", http.MethodPost, "/orders/", memberCookie, memberCSRF, http.StatusNoContent, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := call(t, srv, tt.method, tt.path, tt.cookie, tt.csrf, "")

			if got.status != tt.wantStatus || got.body != tt.wantBody {
				t.Errorf("%s %s = %d %q, want %d %q", tt.method, tt.path, got.status, got.body, tt.wantStatus, tt.wantBody)
			}
		})
	}
}

func TestPublicRoutes(t *testing.T) {
	tests := []struct {
		name    string
		ids     []string
		want    map[string]bool
		wantErr string
	}{
		{"listed operations", []string{"listBars", "placeOrder"}, map[string]bool{"GET /bars/{interval}": true, "POST /orders": true}, ""},
		{"none", nil, map[string]bool{}, ""},
		{"unknown operation", []string{"listBars", "nope"}, nil, `public operation "nope" is not in the spec`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpapi.PublicRoutes(paramtest.Spec, tt.ids)

			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("PublicRoutes() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PublicRoutes() error = %v", err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("PublicRoutes() = %v, want %v", got, tt.want)
			}
		})
	}
}
