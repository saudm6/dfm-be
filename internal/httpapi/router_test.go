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
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/httpapi"
)

// fakeDB is a Pinger that returns err.
type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

// fakeCalendars loads a Monday to Friday DFM calendar closed on
// 2026-10-16, or fails with err.
type fakeCalendars struct{ err error }

func (f fakeCalendars) Load(context.Context) (*calendar.Calendar, error) {
	if f.err != nil {
		return nil, f.err
	}
	return calendar.New(calendar.Hours{
		PreOpen:        9*60 + 30,
		Open:           10 * 60,
		ClosingAuction: 14*60 + 45,
		Close:          15 * 60,
		Week:           []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
	}, []time.Time{domain.TradingDayStart(2026, time.October, 16)})
}

func at(t time.Time) func() time.Time { return func() time.Time { return t } }

// thursday is 2026-10-15 11:00 in Dubai, during continuous trading.
var thursday = time.Date(2026, time.October, 15, 7, 0, 0, 0, time.UTC)

func newRouter(logger *slog.Logger, db fakeDB) http.Handler {
	return httpapi.NewRouter(logger, db, fakeCalendars{}, at(thursday))
}

func TestRouter(t *testing.T) {
	tests := []struct {
		name            string
		db              fakeDB
		method          string
		path            string
		wantStatus      int
		wantContentType string
		wantBody        string
	}{
		{
			name:            "health check",
			method:          http.MethodGet,
			path:            "/healthz",
			wantStatus:      http.StatusOK,
			wantContentType: "application/json",
			wantBody:        `{"status":"ok","db":"ok"}`,
		},
		{
			name:            "health check database down",
			db:              fakeDB{err: errors.New("connection refused")},
			method:          http.MethodGet,
			path:            "/healthz",
			wantStatus:      http.StatusServiceUnavailable,
			wantContentType: "application/json",
			wantBody:        `{"status":"unavailable","db":"unavailable"}`,
		},
		{
			name:            "health check wrong method",
			method:          http.MethodPost,
			path:            "/healthz",
			wantStatus:      http.StatusMethodNotAllowed,
			wantContentType: "",
			wantBody:        "",
		},
		{
			name:            "unknown route",
			method:          http.MethodGet,
			path:            "/nope",
			wantStatus:      http.StatusNotFound,
			wantContentType: "text/plain; charset=utf-8",
			wantBody:        "404 page not found\n",
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
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
		})
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
	if line.Status != 200 || line.Bytes != 25 {
		t.Errorf("status, bytes = %d, %d, want %d, %d", line.Status, line.Bytes, 200, 25)
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

func TestMarketStatus(t *testing.T) {
	tests := []struct {
		name       string
		cals       fakeCalendars
		now        time.Time
		wantStatus int
		wantBody   string
	}{
		{
			name:       "continuous trading before a closed Friday",
			now:        thursday,
			wantStatus: http.StatusOK,
			wantBody:   `{"session":"continuous","next_open":"2026-10-19T10:00:00+04:00"}`,
		},
		{
			name:       "pre-open",
			now:        time.Date(2026, time.October, 15, 9, 30, 0, 0, domain.DubaiTZ),
			wantStatus: http.StatusOK,
			wantBody:   `{"session":"pre-open","next_open":"2026-10-15T10:00:00+04:00"}`,
		},
		{
			name:       "closing auction",
			now:        time.Date(2026, time.October, 15, 14, 50, 0, 0, domain.DubaiTZ),
			wantStatus: http.StatusOK,
			wantBody:   `{"session":"closing-auction","next_open":"2026-10-19T10:00:00+04:00"}`,
		},
		{
			name:       "unscheduled closure",
			now:        time.Date(2026, time.October, 16, 11, 0, 0, 0, domain.DubaiTZ),
			wantStatus: http.StatusOK,
			wantBody:   `{"session":"closed","next_open":"2026-10-19T10:00:00+04:00"}`,
		},
		{
			name:       "weekend",
			now:        time.Date(2026, time.October, 18, 11, 0, 0, 0, domain.DubaiTZ),
			wantStatus: http.StatusOK,
			wantBody:   `{"session":"closed","next_open":"2026-10-19T10:00:00+04:00"}`,
		},
		{
			name:       "calendar unavailable",
			cals:       fakeCalendars{err: errors.New("connection refused")},
			now:        thursday,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"error":"calendar unavailable"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			router := httpapi.NewRouter(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError})), fakeDB{}, tt.cals, at(tt.now))
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/market/status", nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want %q", got, "application/json")
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %s, want %s", got, tt.wantBody)
			}
			wantLog := tt.cals.err != nil
			if gotLog := bytes.Contains(logs.Bytes(), []byte(`"msg":"market status: load calendar failed","error":"connection refused"`)); gotLog != wantLog {
				t.Errorf("logged calendar error = %v, want %v; logs: %s", gotLog, wantLog, logs.String())
			}
		})
	}
}
