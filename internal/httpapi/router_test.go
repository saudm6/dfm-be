package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/httpapi"
	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

// fakeDB is a Pinger that returns err.
type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func newRouter(logger *slog.Logger, db fakeDB) http.Handler {
	return httpapi.NewRouter(logger, db, fakeCalendars{}, at(thursday), nil, nil)
}

func TestRouter(t *testing.T) {
	spec, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name            string
		db              fakeDB
		method          string
		path            string
		wantStatus      int
		wantContentType string
		wantAllow       string
		wantBody        string
	}{
		{
			name:            "health check",
			method:          http.MethodGet,
			path:            "/healthz",
			wantStatus:      http.StatusOK,
			wantContentType: "application/json",
			wantBody:        "{\"status\":\"ok\",\"db\":\"ok\"}\n",
		},
		{
			name:            "health check database down",
			db:              fakeDB{err: errors.New("connection refused")},
			method:          http.MethodGet,
			path:            "/healthz",
			wantStatus:      http.StatusServiceUnavailable,
			wantContentType: "application/json",
			wantBody:        "{\"status\":\"unavailable\",\"db\":\"unavailable\"}\n",
		},
		{
			name:            "health check wrong method",
			method:          http.MethodPost,
			path:            "/healthz",
			wantStatus:      http.StatusMethodNotAllowed,
			wantContentType: "application/json",
			wantAllow:       "GET",
			wantBody:        "{\"code\":\"method_not_allowed\",\"message\":\"POST is not allowed on /healthz\"}\n",
		},
		{
			name:            "openapi spec",
			method:          http.MethodGet,
			path:            "/openapi.yaml",
			wantStatus:      http.StatusOK,
			wantContentType: "application/yaml",
			wantBody:        string(spec),
		},
		{
			name:            "unknown route",
			method:          http.MethodGet,
			path:            "/nope",
			wantStatus:      http.StatusNotFound,
			wantContentType: "application/json",
			wantBody:        "{\"code\":\"not_found\",\"message\":\"no route for /nope\"}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newRouter(slog.New(slog.DiscardHandler), tt.db)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantContentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantContentType)
			}
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

func TestRouterServesSpecWithLength(t *testing.T) {
	spec, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(newRouter(slog.New(slog.DiscardHandler), fakeDB{}))
	defer srv.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/openapi.yaml", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.ContentLength != int64(len(spec)) {
		t.Errorf("Content-Length = %d, want %d", resp.ContentLength, len(spec))
	}
}

// failingServer serves the contract but its health check fails. It embeds
// the interface so operations added to the spec need no stub here; calling
// one of those panics.
type failingServer struct {
	gen.StrictServerInterface
}

func (failingServer) GetHealth(context.Context, gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	return nil, errors.New("database unreachable")
}

func TestRouterHandlerErrorSendsErrorBody(t *testing.T) {
	var logs bytes.Buffer
	router := httpapi.NewRouterWithServer(slog.New(slog.NewJSONHandler(&logs, nil)), failingServer{}, nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got, want := rec.Body.String(), "{\"code\":\"internal\",\"message\":\"internal server error\"}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}

	var line struct {
		Level string `json:"level"`
		Msg   string `json:"msg"`
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(&logs).Decode(&line); err != nil {
		t.Fatalf("decode log line: %v", err)
	}
	if line.Level != "ERROR" || line.Msg != "http handler failed" || line.Path != "/healthz" || line.Error != "database unreachable" {
		t.Errorf("log line = %+v, want ERROR \"http handler failed\" /healthz \"database unreachable\"", line)
	}
}

func TestRouterLogsEachRequest(t *testing.T) {
	var logs bytes.Buffer
	router := newRouter(slog.New(slog.NewJSONHandler(&logs, nil)), fakeDB{})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz?probe=1", nil))

	var line struct {
		Level     string `json:"level"`
		Msg       string `json:"msg"`
		Method    string `json:"method"`
		Path      string `json:"path"`
		Status    int    `json:"status"`
		Bytes     int    `json:"bytes"`
		Duration  *int64 `json:"duration"`
		RequestID string `json:"request_id"`
	}
	dec := json.NewDecoder(&logs)
	if err := dec.Decode(&line); err != nil {
		t.Fatalf("decode log line: %v", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Errorf("want exactly one log line, next decode error = %v", err)
	}

	if line.Level != "INFO" || line.Msg != "http request" {
		t.Errorf("level, msg = %q, %q, want %q, %q", line.Level, line.Msg, "INFO", "http request")
	}
	if line.Method != http.MethodGet || line.Path != "/healthz" {
		t.Errorf("method, path = %q, %q, want %q, %q", line.Method, line.Path, http.MethodGet, "/healthz")
	}
	if line.Status != 200 || line.Bytes != 26 {
		t.Errorf("status, bytes = %d, %d, want %d, %d", line.Status, line.Bytes, 200, 26)
	}
	if line.Duration == nil {
		t.Error("duration missing from log line")
	}
	if line.RequestID == "" {
		t.Error("request_id missing from log line")
	}
}

func TestHealthzLogsDatabaseError(t *testing.T) {
	var logs bytes.Buffer
	router := newRouter(slog.New(slog.NewJSONHandler(&logs, nil)), fakeDB{err: errors.New("connection refused")})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))

	var line struct {
		Level string `json:"level"`
		Msg   string `json:"msg"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(&logs).Decode(&line); err != nil {
		t.Fatalf("decode log line: %v", err)
	}
	want := "ERROR|health check: database ping failed|connection refused"
	if got := line.Level + "|" + line.Msg + "|" + line.Error; got != want {
		t.Errorf("first log line = %q, want %q", got, want)
	}
}

func TestRouterPanicSendsErrorBody(t *testing.T) {
	var logs bytes.Buffer
	router := httpapi.NewRouterWithServer(slog.New(slog.NewJSONHandler(&logs, nil)), failingServer{}, nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.yaml", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got, want := rec.Body.String(), "{\"code\":\"internal\",\"message\":\"internal server error\"}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}

	var line struct {
		Level string `json:"level"`
		Msg   string `json:"msg"`
		Path  string `json:"path"`
		Stack string `json:"stack"`
	}
	if err := json.NewDecoder(&logs).Decode(&line); err != nil {
		t.Fatalf("decode log line: %v", err)
	}
	if line.Level != "ERROR" || line.Msg != "http handler panicked" || line.Path != "/openapi.yaml" {
		t.Errorf("log line = %+v, want ERROR \"http handler panicked\" /openapi.yaml", line)
	}
	if line.Stack == "" {
		t.Error("stack missing from log line")
	}
}

// chi routes on the escaped path, so the Allow header must be computed on it
// too: /bars/1m%2Fx matches /bars/{interval}, /bars/1m/x matches nothing.
func TestMethodNotAllowedOnEscapedPath(t *testing.T) {
	router := httpapi.NewParamTestRouter(slog.New(slog.DiscardHandler), securityServer{}, nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/bars/1m%2Fx", nil))

	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET" {
		t.Errorf("PUT /bars/1m%%2Fx = %d Allow %q, want 405 Allow %q", rec.Code, rec.Header().Get("Allow"), "GET")
	}
}

// A handler that panics after sending its status gets its connection
// aborted, so the client sees a broken response, not JSON appended to it.
func TestPanicAfterPartialWriteAbortsResponse(t *testing.T) {
	var logs bytes.Buffer
	r := httpapi.NewBaseRouter(slog.New(slog.NewJSONHandler(&logs, nil)))
	r.Get("/partial", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		_ = http.NewResponseController(w).Flush()
		panic("boom")
	})
	srv, served := serveOnce(r)
	defer srv.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/partial", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)

	if !errors.Is(err, io.ErrUnexpectedEOF) || string(body) != "partial" {
		t.Errorf("body = %q, %v, want %q, %v", body, err, "partial", io.ErrUnexpectedEOF)
	}
	<-served
	if !strings.Contains(logs.String(), `"msg":"http handler panicked","path":"/partial","panic":"boom"`) {
		t.Errorf("logs = %s, want the panic logged", logs.String())
	}
}

// A handler that panics after hijacking its connection gets the connection
// closed; there is no HTTP response to write on it.
func TestPanicAfterHijackClosesConnection(t *testing.T) {
	var logs bytes.Buffer
	r := httpapi.NewBaseRouter(slog.New(slog.NewJSONHandler(&logs, nil)))
	r.Get("/hijack", func(w http.ResponseWriter, _ *http.Request) {
		if _, _, err := http.NewResponseController(w).Hijack(); err != nil {
			t.Error(err)
		}
		panic("boom")
	})
	srv, served := serveOnce(r)
	defer srv.Close()
	var d net.Dialer
	conn, err := d.DialContext(t.Context(), "tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("GET /hijack HTTP/1.1\r\nHost: test\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}

	got, err := io.ReadAll(conn)

	if err != nil || len(got) != 0 {
		t.Errorf("read = %q, %v, want the connection closed with nothing written", got, err)
	}
	<-served
	if !strings.Contains(logs.String(), `"status":0,"bytes":0`) || !strings.Contains(logs.String(), `"hijacked":true`) {
		t.Errorf("logs = %s, want an access log line with status 0 and hijacked", logs.String())
	}
}

// serveOnce serves h and closes served once h has returned or panicked for
// the first request, so its log lines are complete.
func serveOnce(h http.Handler) (*httptest.Server, <-chan struct{}) {
	served := make(chan struct{})
	var once sync.Once
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer once.Do(func() { close(served) })
		h.ServeHTTP(w, r)
	})), served
}
