package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/saudm6/dfm-be/internal/httpapi"
	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

// fakeDB is a Pinger that returns err.
type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func newRouter(logger *slog.Logger, db fakeDB) http.Handler {
	return httpapi.NewRouter(logger, db, fakeCalendars{}, at(thursday), nil)
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
