package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/store/storetest"
)

// clock is a settable time source shared with the service under test.
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

func newService(t *testing.T) (*auth.Service, *clock) {
	t.Helper()
	pool := storetest.NewPool(t)
	c := &clock{now: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)}
	return auth.NewService(pool, auth.Options{IdleTimeout: time.Hour, MaxAge: 8 * time.Hour, Now: c.Now}), c
}

func requestWith(cookie *http.Cookie) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/me", nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}

func TestCreateUser(t *testing.T) {
	svc, _ := newService(t)

	got, err := svc.CreateUser(t.Context(), "  Saud ", "correct horse battery", auth.Owner)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if want := (auth.User{ID: 1, Username: "saud", Role: auth.Owner}); got != want {
		t.Errorf("CreateUser() = %+v, want %+v", got, want)
	}

	tests := []struct {
		name     string
		username string
		password string
		role     auth.Role
		wantErr  string
	}{
		{"taken username, other case", "SAUD", "another password", auth.Member, "auth: username is taken: saud"},
		{"short password", "amal", "1234567", auth.Member, "auth: password must be at least 8 characters"},
		{"unknown role", "amal", "correct horse battery", auth.Role("admin"), `auth: unknown role "admin"`},
		{"empty username", " ", "correct horse battery", auth.Member, `auth: username " " must be 1 to 64 characters of a-z, 0-9, '.', '_' and '-', starting with a letter or digit`},
		{"username with space", "a b", "correct horse battery", auth.Member, `auth: username "a b" must be 1 to 64 characters of a-z, 0-9, '.', '_' and '-', starting with a letter or digit`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateUser(t.Context(), tt.username, tt.password, tt.role)
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("CreateUser() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestCreateUserStoresArgon2idHash(t *testing.T) {
	pool := storetest.NewPool(t)
	svc := auth.NewService(pool, auth.Options{IdleTimeout: time.Hour, MaxAge: time.Hour})
	if _, err := svc.CreateUser(t.Context(), "saud", "correct horse battery", auth.Member); err != nil {
		t.Fatal(err)
	}

	var hash, role string
	if err := pool.QueryRow(t.Context(), "SELECT password_hash, role FROM users WHERE username = 'saud'").Scan(&hash, &role); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("password_hash = %q, want an argon2id PHC string", hash)
	}
	if ok, err := auth.VerifyPassword("correct horse battery", hash); err != nil || !ok {
		t.Errorf("VerifyPassword(stored hash) = %t, %v, want true", ok, err)
	}
	if role != "member" {
		t.Errorf("role = %q, want %q", role, "member")
	}
}

func TestLoginRejectsBadCredentialsAlike(t *testing.T) {
	svc, _ := newService(t)
	if _, err := svc.CreateUser(t.Context(), "saud", "correct horse battery", auth.Owner); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		username string
		password string
	}{
		{"wrong password", "saud", "correct horse batter"},
		{"unknown user", "nobody", "correct horse battery"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cookie, err := svc.Login(t.Context(), tt.username, tt.password, "")
			if !errors.Is(err, auth.ErrInvalidCredentials) || err.Error() != "auth: invalid username or password" {
				t.Errorf("Login() error = %v, want %v", err, auth.ErrInvalidCredentials)
			}
			if cookie != nil {
				t.Errorf("Login() cookie = %v, want nil", cookie)
			}
		})
	}
}

func TestLoginSessionLifecycle(t *testing.T) {
	pool := storetest.NewPool(t)
	svc := auth.NewService(pool, auth.Options{IdleTimeout: time.Hour, MaxAge: 8 * time.Hour})
	if _, err := svc.CreateUser(t.Context(), "saud", "correct horse battery", auth.Owner); err != nil {
		t.Fatal(err)
	}

	sess, cookie, err := svc.Login(t.Context(), " SAUD", "correct horse battery", "")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if want := (auth.User{ID: 1, Username: "saud", Role: auth.Owner}); sess.User != want {
		t.Errorf("Login() user = %+v, want %+v", sess.User, want)
	}
	if len(sess.CSRFToken) != 43 {
		t.Errorf("CSRF token %q has %d characters, want 43 (32 random bytes)", sess.CSRFToken, len(sess.CSRFToken))
	}
	wantCookie := "dfm_session=" + cookie.Value + "; Path=/; Max-Age=28800; HttpOnly; Secure; SameSite=Strict"
	if got := cookie.String(); got != wantCookie {
		t.Errorf("cookie = %q, want %q", got, wantCookie)
	}

	token, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(token) != 32 {
		t.Fatalf("cookie value %q is not 32 bytes of base64url: %v", cookie.Value, err)
	}
	var stored []byte
	if err := pool.QueryRow(t.Context(), "SELECT token_hash FROM sessions").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if want := sha256.Sum256(token); string(stored) != string(want[:]) {
		t.Errorf("token_hash = %x, want SHA-256 of the token %x", stored, want)
	}

	got, err := svc.UserFromRequest(requestWith(cookie))
	if err != nil {
		t.Fatalf("UserFromRequest() error = %v", err)
	}
	if got.User != sess.User || got.CSRFToken != sess.CSRFToken {
		t.Errorf("UserFromRequest() = %+v, want %+v", got, sess)
	}

	cleared, err := svc.Logout(t.Context(), got)
	if err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if want := "dfm_session=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Strict"; cleared.String() != want {
		t.Errorf("Logout() cookie = %q, want %q", cleared.String(), want)
	}
	var rows int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("sessions after logout = %d, want 0", rows)
	}
	if _, err := svc.UserFromRequest(requestWith(cookie)); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("UserFromRequest() after logout error = %v, want %v", err, auth.ErrUnauthenticated)
	}
}

func TestInsecureCookieOption(t *testing.T) {
	pool := storetest.NewPool(t)
	svc := auth.NewService(pool, auth.Options{IdleTimeout: time.Hour, MaxAge: time.Hour, InsecureCookie: true})
	if _, err := svc.CreateUser(t.Context(), "saud", "correct horse battery", auth.Owner); err != nil {
		t.Fatal(err)
	}
	_, cookie, err := svc.Login(t.Context(), "saud", "correct horse battery", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "dfm_session=" + cookie.Value + "; Path=/; Max-Age=3600; HttpOnly; SameSite=Strict"; cookie.String() != want {
		t.Errorf("cookie = %q, want %q", cookie.String(), want)
	}
}

func TestUserFromRequestRejectsBadCookies(t *testing.T) {
	svc, _ := newService(t)
	unknown := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	tests := []struct {
		name   string
		cookie *http.Cookie
	}{
		{"no cookie", nil},
		{"not base64", &http.Cookie{Name: auth.CookieName, Value: "not*base64"}},
		{"short token", &http.Cookie{Name: auth.CookieName, Value: "c2hvcnQ"}},
		{"unknown token", &http.Cookie{Name: auth.CookieName, Value: unknown}},
		{"other cookie name", &http.Cookie{Name: "session", Value: unknown}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.UserFromRequest(requestWith(tt.cookie)); !errors.Is(err, auth.ErrUnauthenticated) {
				t.Errorf("UserFromRequest() error = %v, want %v", err, auth.ErrUnauthenticated)
			}
		})
	}
}

func TestSessionExpiry(t *testing.T) {
	tests := []struct {
		name string
		// steps advance the clock before each request; the last request's
		// outcome is checked.
		steps    []time.Duration
		wantLive bool
	}{
		{"just inside idle timeout", []time.Duration{59 * time.Minute}, true},
		{"idle timeout reached", []time.Duration{time.Hour}, false},
		{"activity extends idle expiry", []time.Duration{50 * time.Minute, 50 * time.Minute}, true},
		{"absolute expiry despite activity", []time.Duration{
			50 * time.Minute, 50 * time.Minute, 50 * time.Minute, 50 * time.Minute, 50 * time.Minute,
			50 * time.Minute, 50 * time.Minute, 50 * time.Minute, 50 * time.Minute, 30 * time.Minute,
		}, false},
		{"just inside absolute expiry", []time.Duration{
			50 * time.Minute, 50 * time.Minute, 50 * time.Minute, 50 * time.Minute, 50 * time.Minute,
			50 * time.Minute, 50 * time.Minute, 50 * time.Minute, 50 * time.Minute, 29 * time.Minute,
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, c := newService(t)
			if _, err := svc.CreateUser(t.Context(), "saud", "correct horse battery", auth.Member); err != nil {
				t.Fatal(err)
			}
			_, cookie, err := svc.Login(t.Context(), "saud", "correct horse battery", "")
			if err != nil {
				t.Fatal(err)
			}
			var lastErr error
			for _, d := range tt.steps {
				c.Advance(d)
				_, lastErr = svc.UserFromRequest(requestWith(cookie))
			}
			if tt.wantLive && lastErr != nil {
				t.Errorf("UserFromRequest() error = %v, want a live session", lastErr)
			}
			if !tt.wantLive && !errors.Is(lastErr, auth.ErrUnauthenticated) {
				t.Errorf("UserFromRequest() error = %v, want %v", lastErr, auth.ErrUnauthenticated)
			}
		})
	}
}

func TestLoginDeletesExpiredSessions(t *testing.T) {
	pool := storetest.NewPool(t)
	c := &clock{now: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)}
	svc := auth.NewService(pool, auth.Options{IdleTimeout: time.Hour, MaxAge: 8 * time.Hour, Now: c.Now})
	if _, err := svc.CreateUser(t.Context(), "saud", "correct horse battery", auth.Member); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Login(t.Context(), "saud", "correct horse battery", ""); err != nil {
		t.Fatal(err)
	}
	c.Advance(2 * time.Hour)
	if _, _, err := svc.Login(t.Context(), "saud", "correct horse battery", ""); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("sessions = %d, want 1 (the expired one deleted)", rows)
	}
}

func TestLoginDeletesPriorSession(t *testing.T) {
	tests := []struct {
		name          string
		password      string
		wantErr       error
		wantPriorLive bool
	}{
		{"successful login", "correct horse battery", nil, false},
		{"failed login", "wrong password", auth.ErrInvalidCredentials, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newService(t)
			if _, err := svc.CreateUser(t.Context(), "saud", "correct horse battery", auth.Owner); err != nil {
				t.Fatal(err)
			}
			_, prior, err := svc.Login(t.Context(), "saud", "correct horse battery", "")
			if err != nil {
				t.Fatal(err)
			}

			_, _, err = svc.Login(t.Context(), "saud", tt.password, prior.Value)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Login() error = %v, want %v", err, tt.wantErr)
			}
			_, err = svc.UserFromRequest(requestWith(prior))
			if live := err == nil; live != tt.wantPriorLive {
				t.Errorf("prior session live = %t (error %v), want %t", live, err, tt.wantPriorLive)
			}
		})
	}
}

func TestLoginIgnoresMalformedPriorCookie(t *testing.T) {
	svc, _ := newService(t)
	if _, err := svc.CreateUser(t.Context(), "saud", "correct horse battery", auth.Owner); err != nil {
		t.Fatal(err)
	}
	_, cookie, err := svc.Login(t.Context(), "saud", "correct horse battery", "garbage")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if _, err := svc.UserFromRequest(requestWith(cookie)); err != nil {
		t.Errorf("UserFromRequest() error = %v, want the new session", err)
	}
}

// The hash for unknown usernames exists before the first login, so the
// first failed login takes no longer than later ones.
func TestNewServiceMakesDummyHash(t *testing.T) {
	svc := auth.NewService(nil, auth.Options{})

	h := auth.DummyHash(svc)

	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("dummy hash = %q, want an argon2id hash", h)
	}
	if ok, err := auth.VerifyPassword("", h); err != nil || ok {
		t.Errorf("VerifyPassword(\"\", dummy hash) = %t, %v, want false, nil", ok, err)
	}
}

func TestCheckCSRF(t *testing.T) {
	svc, _ := newService(t)
	if _, err := svc.CreateUser(t.Context(), "saud", "correct horse battery", auth.Member); err != nil {
		t.Fatal(err)
	}
	sess, _, err := svc.Login(t.Context(), "saud", "correct horse battery", "")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		header string
		want   error
	}{
		{"matching token", sess.CSRFToken, nil},
		{"missing token", "", auth.ErrCSRF},
		{"wrong token", strings.ToUpper(sess.CSRFToken), auth.ErrCSRF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/logout", nil)
			if tt.header != "" {
				r.Header.Set(auth.CSRFHeader, tt.header)
			}
			if err := auth.CheckCSRF(r, sess); !errors.Is(err, tt.want) {
				t.Errorf("CheckCSRF() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRequireMarketData(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want error
	}{
		{"owner", auth.WithSession(context.Background(), auth.Session{User: auth.User{Role: auth.Owner}}), nil},
		{"member", auth.WithSession(context.Background(), auth.Session{User: auth.User{Role: auth.Member}}), auth.ErrForbidden},
		{"no session", context.Background(), auth.ErrUnauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := auth.RequireMarketData(tt.ctx); !errors.Is(err, tt.want) {
				t.Errorf("RequireMarketData() error = %v, want %v", err, tt.want)
			}
		})
	}
}
