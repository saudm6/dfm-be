package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/httpapi"
)

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

// hangingCalendars never loads: Load waits for its context to end. It sends
// how long its context had left when called.
type hangingCalendars struct{ called chan<- time.Duration }

func (h hangingCalendars) Load(ctx context.Context) (*calendar.Calendar, error) {
	left := time.Duration(-1)
	if deadline, ok := ctx.Deadline(); ok {
		left = time.Until(deadline)
	}
	h.called <- left
	<-ctx.Done()
	return nil, ctx.Err()
}

func at(t time.Time) func() time.Time { return func() time.Time { return t } }

// thursday is 2026-10-15 11:00 in Dubai, during continuous trading.
var thursday = time.Date(2026, time.October, 15, 7, 0, 0, 0, time.UTC)

func TestMarketStatus(t *testing.T) {
	tests := []struct {
		name       string
		cals       fakeCalendars
		now        time.Time
		wantStatus int
		wantBody   string
		wantLog    string
	}{
		{
			name:       "continuous trading before a closed Friday",
			now:        thursday,
			wantStatus: http.StatusOK,
			wantBody:   "{\"session\":\"continuous\",\"nextOpen\":\"2026-10-19T10:00:00+04:00\"}\n",
		},
		{
			name:       "pre-open",
			now:        time.Date(2026, time.October, 15, 9, 30, 0, 0, domain.DubaiTZ),
			wantStatus: http.StatusOK,
			wantBody:   "{\"session\":\"pre-open\",\"nextOpen\":\"2026-10-15T10:00:00+04:00\"}\n",
		},
		{
			name:       "closing auction",
			now:        time.Date(2026, time.October, 15, 14, 50, 0, 0, domain.DubaiTZ),
			wantStatus: http.StatusOK,
			wantBody:   "{\"session\":\"closing-auction\",\"nextOpen\":\"2026-10-19T10:00:00+04:00\"}\n",
		},
		{
			name:       "unscheduled closure",
			now:        time.Date(2026, time.October, 16, 11, 0, 0, 0, domain.DubaiTZ),
			wantStatus: http.StatusOK,
			wantBody:   "{\"session\":\"closed\",\"nextOpen\":\"2026-10-19T10:00:00+04:00\"}\n",
		},
		{
			name:       "weekend",
			now:        time.Date(2026, time.October, 18, 11, 0, 0, 0, domain.DubaiTZ),
			wantStatus: http.StatusOK,
			wantBody:   "{\"session\":\"closed\",\"nextOpen\":\"2026-10-19T10:00:00+04:00\"}\n",
		},
		{
			name:       "calendar unavailable",
			cals:       fakeCalendars{err: errors.New("connection refused")},
			now:        thursday,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "{\"code\":\"unavailable\",\"message\":\"trading calendar unavailable\"}\n",
			wantLog:    "{\"level\":\"ERROR\",\"msg\":\"market status: load calendar failed\",\"error\":\"connection refused\",\"request_id\":\"req-1\"}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			router := httpapi.NewRouter(errorLogger(&logs), fakeDB{}, tt.cals, at(tt.now))
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/market/status", nil)
			req.Header.Set("X-Request-Id", "req-1")

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want %q", got, "application/json")
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
			if got := logs.String(); got != tt.wantLog {
				t.Errorf("logs = %q, want %q", got, tt.wantLog)
			}
		})
	}
}

func TestMarketStatusTimesOutLoad(t *testing.T) {
	var logs bytes.Buffer
	called := make(chan time.Duration, 1)
	router := httpapi.NewRouter(errorLogger(&logs), fakeDB{}, hangingCalendars{called: called}, at(thursday))
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/market/status", nil)
	req.Header.Set("X-Request-Id", "req-1")

	start := time.Now()
	router.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if left := <-called; left < 1900*time.Millisecond || left > 2*time.Second {
		t.Errorf("Load had %s left, want 2s", left)
	}
	if elapsed > 3*time.Second {
		t.Errorf("request took %s, want about 2s", elapsed)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if got, want := rec.Body.String(), "{\"code\":\"unavailable\",\"message\":\"trading calendar unavailable\"}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if got, want := logs.String(), "{\"level\":\"ERROR\",\"msg\":\"market status: load calendar failed\",\"error\":\"context deadline exceeded\",\"request_id\":\"req-1\"}\n"; got != want {
		t.Errorf("logs = %q, want %q", got, want)
	}
}

func TestMarketStatusClientCancelIsNotAnError(t *testing.T) {
	var logs bytes.Buffer
	called := make(chan time.Duration, 1)
	router := httpapi.NewRouter(errorLogger(&logs), fakeDB{}, hangingCalendars{called: called}, at(thursday))
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-called
		cancel()
	}()

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, http.MethodGet, "/market/status", nil))

	if got := logs.String(); got != "" {
		t.Errorf("logs = %q, want none at ERROR", got)
	}
}

// errorLogger logs records at ERROR and above to w as JSON, without times.
func errorLogger(w *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelError,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
}
