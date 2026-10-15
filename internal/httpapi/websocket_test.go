package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/httpapi"
	"github.com/saudm6/dfm-be/internal/ws"
)

// syncBuffer is a bytes.Buffer safe for the server's and the test's
// goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type wsFixture struct {
	authFixture
	srv     *httptest.Server
	gateway *ws.Gateway
	logs    *syncBuffer
	// recheck is set by newGatedWSFixture.
	recheck *gate
}

func newWSFixture(t *testing.T) wsFixture {
	t.Helper()
	f := newAuthFixture(t)
	return startWS(t, f, f.sessions, ws.Options{})
}

// newGatedWSFixture returns a fixture whose sockets revalidate their session
// only when the test calls f.recheck.allow.
func newGatedWSFixture(t *testing.T) wsFixture {
	t.Helper()
	f := newAuthFixture(t)
	g := &gate{sessions: f.sessions, allowed: make(chan struct{}), done: make(chan error)}
	wf := startWS(t, f, g, ws.Options{RecheckInterval: time.Millisecond})
	wf.recheck = g
	return wf
}

// gate passes one revalidation through to the real sessions per call to
// allow.
type gate struct {
	sessions *auth.Service
	allowed  chan struct{}
	done     chan error
}

func (g *gate) Revalidate(ctx context.Context, sess auth.Session) (auth.Session, error) {
	select {
	case <-g.allowed:
	case <-ctx.Done():
		return auth.Session{}, ctx.Err()
	}
	sess, err := g.sessions.Revalidate(ctx, sess)
	select {
	case g.done <- err:
	case <-ctx.Done():
	}
	return sess, err
}

// allow lets one socket revalidate its session and returns the result.
func (g *gate) allow(t *testing.T) error {
	t.Helper()
	select {
	case g.allowed <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("no socket revalidated its session")
	}
	return <-g.done
}

func startWS(t *testing.T, f authFixture, sessions ws.Sessions, opts ws.Options) wsFixture {
	t.Helper()
	logs := &syncBuffer{}
	gateway := ws.New(slog.New(slog.DiscardHandler), ws.Topics, sessions, opts)
	srv := httptest.NewServer(httpapi.NewRouter(slog.New(slog.NewJSONHandler(logs, nil)), fakeDB{}, fakeCalendars{}, at(thursday), f.sessions, nil, gateway))
	t.Cleanup(func() {
		gateway.Close()
		srv.Close()
	})
	return wsFixture{authFixture: f, srv: srv, gateway: gateway, logs: logs}
}

func (f wsFixture) url() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/ws" }

// dialWS opens /ws with cookie, when not nil, and origin, when not empty.
// When the upgrade is refused it returns the response's status and body.
func dialWS(t *testing.T, f wsFixture, cookie *http.Cookie, origin string) (c *websocket.Conn, status int, body string, err error) {
	t.Helper()
	h := http.Header{}
	if cookie != nil {
		h.Set("Cookie", cookie.String())
	}
	if origin != "" {
		h.Set("Origin", origin)
	}
	c, resp, err := websocket.Dial(t.Context(), f.url(), &websocket.DialOptions{HTTPHeader: h})
	if c != nil {
		t.Cleanup(func() { _ = c.CloseNow() })
	}
	if resp != nil && resp.Body != nil {
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		status, body = resp.StatusCode, string(b)
	}
	return c, status, body, err
}

func wsSend(t *testing.T, c *websocket.Conn, s string) {
	t.Helper()
	if err := c.Write(t.Context(), websocket.MessageText, []byte(s)); err != nil {
		t.Fatal(err)
	}
}

func wsExpect(t *testing.T, c *websocket.Conn, want ...string) {
	t.Helper()
	for _, w := range want {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_, b, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(b) != w {
			t.Errorf("message = %s\nwant      %s", b, w)
		}
	}
}

func TestWebSocketRefusals(t *testing.T) {
	f := newWSFixture(t)
	owner, _ := login(t, f.srv, "saud")
	tests := []struct {
		name       string
		cookie     *http.Cookie
		origin     string
		wantStatus int
		wantBody   string
	}{
		{"no session", nil, "", http.StatusUnauthorized, `{"code":"unauthorized","message":"sign in to use /ws"}` + "\n"},
		{"unknown session", &http.Cookie{Name: "dfm_session", Value: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}, "", http.StatusUnauthorized, `{"code":"unauthorized","message":"sign in to use /ws"}` + "\n"},
		{"cross-site origin", owner, "https://evil.example", http.StatusForbidden, `{"code":"forbidden","message":"origin https://evil.example may not open a WebSocket here"}` + "\n"},
		{"cross-site origin without session", nil, "https://evil.example", http.StatusForbidden, `{"code":"forbidden","message":"origin https://evil.example may not open a WebSocket here"}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, status, body, err := dialWS(t, f, tt.cookie, tt.origin)

			if err == nil {
				t.Fatal("Dial() succeeded, want the upgrade refused")
			}
			if status != tt.wantStatus || body != tt.wantBody {
				t.Errorf("upgrade = %d %q, want %d %q", status, body, tt.wantStatus, tt.wantBody)
			}
		})
	}

	t.Run("not an upgrade", func(t *testing.T) {
		got := call(t, f.srv, http.MethodGet, "/ws", owner, "", "")

		if want := `{"code":"upgrade_required","message":"/ws needs a WebSocket upgrade"}` + "\n"; got.status != http.StatusUpgradeRequired || got.body != want {
			t.Errorf("GET /ws = %d %q, want 426 %q", got.status, got.body, want)
		}
	})
}

func TestWebSocketSubscribeReceiveUnsubscribe(t *testing.T) {
	f := newWSFixture(t)
	owner, _ := login(t, f.srv, "saud")
	c, _, _, err := dialWS(t, f, owner, f.srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	wsSend(t, c, `{"type":"subscribe","topics":["quote:EMAAR"]}`)
	wsExpect(t, c, `{"type":"snapshot","topic":"quote:EMAAR","seq":1,"data":null}`)
	for _, last := range []string{"11.4", "11.45"} {
		if err := f.gateway.Publish("quote:EMAAR", map[string]string{"last": last}); err != nil {
			t.Fatal(err)
		}
	}
	wsExpect(t, c,
		`{"type":"data","topic":"quote:EMAAR","seq":2,"data":{"last":"11.4"}}`,
		`{"type":"data","topic":"quote:EMAAR","seq":3,"data":{"last":"11.45"}}`,
	)
	wsSend(t, c, `{"type":"unsubscribe","topics":["quote:EMAAR"]}`)
	wsSend(t, c, `{"type":"subscribe","topics":["status"]}`)
	wsExpect(t, c, `{"type":"snapshot","topic":"status","seq":1,"data":null}`)
	if err := f.gateway.Publish("quote:EMAAR", map[string]string{"last": "11.5"}); err != nil {
		t.Fatal(err)
	}
	if err := f.gateway.Publish("status", map[string]string{"ibkr": "up"}); err != nil {
		t.Fatal(err)
	}
	wsExpect(t, c, `{"type":"data","topic":"status","seq":2,"data":{"ibkr":"up"}}`)

	if err := c.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatal(err)
	}
	// The access log line is written when the connection ends.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(f.logs.String(), `"msg":"http request","method":"GET","path":"/ws","status":101`) {
		if time.Now().After(deadline) {
			t.Fatalf("logs = %s, want an http request line for /ws with status 101", f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(f.logs.String(), `"hijacked":true`) {
		t.Errorf("logs = %s, want the /ws line marked hijacked", f.logs.String())
	}
}

func TestWebSocketMemberIsRefusedMarketData(t *testing.T) {
	f := newWSFixture(t)
	member, _ := login(t, f.srv, "amal")
	c, _, _, err := dialWS(t, f, member, "")
	if err != nil {
		t.Fatal(err)
	}

	wsSend(t, c, `{"type":"subscribe","topics":["quote:EMAAR","chat:desk-1"]}`)

	wsExpect(t, c,
		`{"type":"error","topic":"quote:EMAAR","code":"forbidden","message":"your role may not read quote"}`,
		`{"type":"snapshot","topic":"chat:desk-1","seq":1,"data":null}`,
	)
}

func expectWSClose(t *testing.T, c *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, b, err := c.Read(ctx)
	var ce websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != ws.StatusSessionEnded || ce.Reason != "session_ended" {
		t.Errorf("read = %s, %v, want close status 4401 reason session_ended", b, err)
	}
}

func TestWebSocketLogoutClosesSessionSockets(t *testing.T) {
	f := newWSFixture(t)
	session, csrf := login(t, f.srv, "saud")
	other, _ := login(t, f.srv, "saud")
	var sockets []*websocket.Conn
	for _, cookie := range []*http.Cookie{session, session, other} {
		c, _, _, err := dialWS(t, f, cookie, "")
		if err != nil {
			t.Fatal(err)
		}
		wsSend(t, c, `{"type":"subscribe","topics":["status"]}`)
		wsExpect(t, c, `{"type":"snapshot","topic":"status","seq":1,"data":null}`)
		sockets = append(sockets, c)
	}

	if got := call(t, f.srv, http.MethodPost, "/auth/logout", session, csrf, ""); got.status != http.StatusNoContent {
		t.Fatalf("logout = %d %s, want 204", got.status, got.body)
	}

	// The re-check interval is 20 seconds, so a close within the read
	// timeout of 5 seconds comes from logout.
	expectWSClose(t, sockets[0])
	expectWSClose(t, sockets[1])
	wsSend(t, sockets[2], `{"type":"subscribe","topics":["movers"]}`)
	wsExpect(t, sockets[2], `{"type":"snapshot","topic":"movers","seq":1,"data":null}`)
}

func TestWebSocketIdleSessionCloses(t *testing.T) {
	f := newGatedWSFixture(t)
	session, _ := login(t, f.srv, "saud")
	c, _, _, err := dialWS(t, f, session, "")
	if err != nil {
		t.Fatal(err)
	}

	f.clock.Advance(12 * time.Hour)
	if err := f.recheck.allow(t); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("Revalidate() error = %v, want %v", err, auth.ErrUnauthenticated)
	}

	expectWSClose(t, c)
}

// An open socket counts as activity: each revalidation extends the
// session's idle expiry, for the socket and for HTTP, but not past the
// session's maximum age of 7 days.
func TestWebSocketKeepsSessionAliveUntilMaxAge(t *testing.T) {
	f := newGatedWSFixture(t)
	session, _ := login(t, f.srv, "saud")
	c, _, _, err := dialWS(t, f, session, "")
	if err != nil {
		t.Fatal(err)
	}

	for range 14 {
		f.clock.Advance(11 * time.Hour)
		if err := f.recheck.allow(t); err != nil {
			t.Fatalf("Revalidate() error = %v, want a live session", err)
		}
	}
	// 165 hours after login, 11 hours after the last revalidation.
	f.clock.Advance(11 * time.Hour)
	if got := call(t, f.srv, http.MethodGet, "/me", session, "", ""); got.status != http.StatusOK {
		t.Errorf("GET /me 165 hours after login = %d %s, want 200", got.status, got.body)
	}
	wsSend(t, c, `{"type":"subscribe","topics":["status"]}`)
	wsExpect(t, c, `{"type":"snapshot","topic":"status","seq":1,"data":null}`)

	f.clock.Advance(4 * time.Hour)
	if err := f.recheck.allow(t); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("Revalidate() past the maximum age error = %v, want %v", err, auth.ErrUnauthenticated)
	}
	expectWSClose(t, c)
}

func TestWebSocketDemotedOwnerLosesMarketData(t *testing.T) {
	f := newGatedWSFixture(t)
	session, _ := login(t, f.srv, "saud")
	c, _, _, err := dialWS(t, f, session, "")
	if err != nil {
		t.Fatal(err)
	}
	wsSend(t, c, `{"type":"subscribe","topics":["quote:EMAAR","orders","status","chat:desk-1"]}`)
	wsExpect(t, c,
		`{"type":"snapshot","topic":"quote:EMAAR","seq":1,"data":null}`,
		`{"type":"snapshot","topic":"orders","seq":1,"data":null}`,
		`{"type":"snapshot","topic":"status","seq":1,"data":null}`,
		`{"type":"snapshot","topic":"chat:desk-1","seq":1,"data":null}`,
	)

	if _, err := f.pool.Exec(t.Context(), "UPDATE users SET role = 'member' WHERE username = 'saud'"); err != nil {
		t.Fatal(err)
	}
	if err := f.recheck.allow(t); err != nil {
		t.Fatalf("Revalidate() error = %v, want a live session", err)
	}

	wsExpect(t, c,
		`{"type":"error","topic":"orders","code":"forbidden","message":"your role may not read orders"}`,
		`{"type":"error","topic":"quote:EMAAR","code":"forbidden","message":"your role may not read quote"}`,
	)
	for _, p := range []struct {
		topic string
		data  any
	}{
		{"quote:EMAAR", map[string]string{"last": "11.4"}},
		{"orders", map[string]string{"id": "o1"}},
		{"status", map[string]string{"ibkr": "up"}},
		{"chat:desk-1", map[string]string{"id": "m1"}},
	} {
		if err := f.gateway.Publish(p.topic, p.data); err != nil {
			t.Fatal(err)
		}
	}
	wsExpect(t, c,
		`{"type":"data","topic":"status","seq":2,"data":{"ibkr":"up"}}`,
		`{"type":"data","topic":"chat:desk-1","seq":2,"data":{"id":"m1"}}`,
	)
}
