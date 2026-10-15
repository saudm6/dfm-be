package httpapi_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

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
}

func newWSFixture(t *testing.T) wsFixture {
	t.Helper()
	f := newAuthFixture(t)
	logs := &syncBuffer{}
	gateway := ws.New(slog.New(slog.DiscardHandler), ws.Topics, ws.Options{})
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
