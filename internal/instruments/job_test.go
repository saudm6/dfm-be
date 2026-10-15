package instruments_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/instruments"
)

var dfmHours = calendar.Hours{
	PreOpen:        9*60 + 30,
	Open:           10 * 60,
	ClosingAuction: 14*60 + 45,
	Close:          15 * 60,
	Week:           []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
}

func dubai(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, domain.DubaiTZ)
}

func newCalendar(t *testing.T, closed ...time.Time) *calendar.Calendar {
	t.Helper()
	cal, err := calendar.New(dfmHours, closed)
	if err != nil {
		t.Fatal(err)
	}
	return cal
}

func TestNextRun(t *testing.T) {
	// 2026-10-15 is a Thursday; 2026-12-02 and 2026-12-03 are UAE National Day.
	cal := newCalendar(t, dubai(time.December, 2, 0, 0), dubai(time.December, 3, 0, 0))
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"early on a trading day", dubai(time.October, 15, 6, 0), "2026-10-15T09:00:00+04:00"},
		{"just before the run", dubai(time.October, 15, 8, 59), "2026-10-15T09:00:00+04:00"},
		{"at the run", dubai(time.October, 15, 9, 0), "2026-10-16T09:00:00+04:00"},
		{"during the session", dubai(time.October, 15, 11, 0), "2026-10-16T09:00:00+04:00"},
		{"Friday after the close", dubai(time.October, 16, 16, 0), "2026-10-19T09:00:00+04:00"},
		{"Saturday", dubai(time.October, 17, 9, 0), "2026-10-19T09:00:00+04:00"},
		{"before a holiday", dubai(time.December, 1, 15, 0), "2026-12-04T09:00:00+04:00"},
		{"UTC input", time.Date(2026, time.October, 15, 1, 0, 0, 0, time.UTC), "2026-10-15T09:00:00+04:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := instruments.NextRun(cal, tt.now, time.Hour).Format(time.RFC3339); got != tt.want {
				t.Errorf("NextRun(%s) = %s, want %s", tt.now, got, tt.want)
			}
		})
	}
}

type loader struct {
	mu    sync.Mutex
	cals  []*calendar.Calendar
	loads int
}

// Load returns the calendars in turn, repeating the last.
func (l *loader) Load(context.Context) (*calendar.Calendar, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cal := l.cals[min(l.loads, len(l.cals)-1)]
	l.loads++
	return cal, nil
}

func TestJobSyncsWithARunIDUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var logs bytes.Buffer
	runs := 0
	job := instruments.Job{
		Sync: func(_ context.Context, logger *slog.Logger) error {
			runs++
			logger.Info("synced")
			if runs == 2 {
				cancel()
			}
			return nil
		},
		Calendar: &loader{cals: []*calendar.Calendar{newCalendar(t)}},
		Lead:     time.Hour,
		Logger:   slog.New(slog.NewJSONHandler(&logs, nil)),
		// A past time, so every run is due at once.
		Now: func() time.Time { return dubai(time.October, 15, 6, 0) },
	}

	if err := job.Run(ctx); err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}

	if runs != 2 {
		t.Errorf("synced %d times, want 2", runs)
	}
	var ids []string
	for _, l := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var line struct {
			Msg   string `json:"msg"`
			RunID string `json:"run_id"`
		}
		if err := json.Unmarshal([]byte(l), &line); err != nil {
			t.Fatalf("decode log %q: %v", l, err)
		}
		if line.Msg == "synced" {
			ids = append(ids, line.RunID)
		}
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] == ids[1] {
		t.Errorf("run ids = %q, want two distinct ids", ids)
	}
}

func TestJobSkipsADayClosedAfterScheduling(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var logs bytes.Buffer
	cals := &loader{cals: []*calendar.Calendar{
		newCalendar(t),
		newCalendar(t, dubai(time.October, 15, 0, 0)),
	}}
	job := instruments.Job{
		Sync: func(context.Context, *slog.Logger) error {
			t.Error("Sync ran on a closed day")
			return nil
		},
		Calendar: cals,
		Lead:     time.Hour,
		Logger:   slog.New(slog.NewJSONHandler(&logs, nil)),
		Now: func() time.Time {
			if strings.Contains(logs.String(), "market closed") {
				cancel()
			}
			return dubai(time.October, 15, 6, 0)
		},
	}

	if err := job.Run(ctx); err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
	if !strings.Contains(logs.String(), `"msg":"instrument sync cancelled: market closed","at":"2026-10-15T09:00:00+04:00"`) {
		t.Errorf("logs = %s, want the cancelled run", logs.String())
	}
}
