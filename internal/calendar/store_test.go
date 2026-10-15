package calendar_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/store/storetest"
)

func TestLoadSeededHolidays(t *testing.T) {
	cal, err := calendar.Loader{DB: storetest.NewPool(t), Hours: dfmHours}.Load(t.Context())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	tests := []struct {
		name        string
		at          time.Time
		wantTrading bool
		wantSession calendar.Session
		wantNext    string
	}{
		{"Eid Al Adha", dubai(2026, time.May, 27, 11, 0, 0), false, calendar.Closed, "2026-06-01T10:00:00+04:00"},
		{"after close before Arafah", dubai(2026, time.May, 25, 15, 0, 0), true, calendar.Closed, "2026-06-01T10:00:00+04:00"},
		{"UAE National Day", dubai(2026, time.December, 2, 9, 30, 0), false, calendar.Closed, "2026-12-04T10:00:00+04:00"},
		{"Hijri New Year as moved by DFM", dubai(2026, time.June, 15, 10, 0, 0), false, calendar.Closed, "2026-06-16T10:00:00+04:00"},
		{"published Hijri New Year DFM traded", dubai(2026, time.June, 16, 10, 0, 0), true, calendar.Continuous, "2026-06-17T10:00:00+04:00"},
		{"seeded unscheduled closure", dubai(2026, time.March, 2, 12, 0, 0), false, calendar.Closed, "2026-03-04T10:00:00+04:00"},
		{"ordinary Thursday", dubai(2026, time.October, 15, 14, 50, 0), true, calendar.ClosingAuction, "2026-10-16T10:00:00+04:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cal.IsTradingDay(tt.at); got != tt.wantTrading {
				t.Errorf("IsTradingDay(%s) = %v, want %v", tt.at, got, tt.wantTrading)
			}
			if got := cal.Session(tt.at); got != tt.wantSession {
				t.Errorf("Session(%s) = %q, want %q", tt.at, got, tt.wantSession)
			}
			if got := cal.NextOpen(tt.at).Format(time.RFC3339); got != tt.wantNext {
				t.Errorf("NextOpen(%s) = %s, want %s", tt.at, got, tt.wantNext)
			}
		})
	}
}

// TestSeedMatchesDFMIndexHistory checks the seeded calendar against every day
// DFM published a DFM Index close, from the history floor of 2 January 2025
// to 15 October 2026.
func TestSeedMatchesDFMIndexHistory(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/dfm/dfmgi-2025-01-01_2026-10-15.json")
	if err != nil {
		t.Fatal(err)
	}
	var history struct {
		Chart struct {
			Points []struct {
				Item1 string `json:"Item1"`
			} `json:"points"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(raw, &history); err != nil {
		t.Fatal(err)
	}
	traded := make(map[string]bool, len(history.Chart.Points))
	for _, p := range history.Chart.Points {
		traded[p.Item1[:len(time.DateOnly)]] = true
	}
	if len(traded) != 447 {
		t.Fatalf("fixture has %d trading days, want 447", len(traded))
	}

	cal, err := calendar.Loader{DB: storetest.NewPool(t), Hours: dfmHours}.Load(t.Context())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for d := domain.TradingDayStart(2025, time.January, 2); !d.After(domain.TradingDayStart(2026, time.October, 15)); d = d.AddDate(0, 0, 1) {
		key := d.Format(time.DateOnly)
		if got := cal.IsTradingDay(d); got != traded[key] {
			t.Errorf("IsTradingDay(%s) = %v, DFM published an index close: %v", key, got, traded[key])
		}
	}
}

func TestAddClosure(t *testing.T) {
	pool := storetest.NewPool(t)
	loader := calendar.Loader{DB: pool, Hours: dfmHours}
	friday := domain.TradingDayStart(2026, time.October, 16)
	tests := []struct {
		name      string
		day       time.Time
		wantAdded bool
	}{
		{"new closure", friday, true},
		{"same day again", dubai(2026, time.October, 16, 13, 0, 0), false},
		{"seeded holiday", domain.TradingDayStart(2026, time.December, 2), false},
	}
	for _, tt := range tests {
		added, err := calendar.AddClosure(t.Context(), pool, tt.day, "Unscheduled closure")
		if err != nil {
			t.Fatalf("%s: AddClosure() error = %v", tt.name, err)
		}
		if added != tt.wantAdded {
			t.Errorf("%s: AddClosure() = %v, want %v", tt.name, added, tt.wantAdded)
		}
	}

	cal, err := loader.Load(t.Context())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	thursdayTrading := dubai(2026, time.October, 15, 11, 0, 0)
	if got := cal.IsTradingDay(friday); got {
		t.Errorf("IsTradingDay(closed Friday) = true, want false")
	}
	if got := cal.Session(dubai(2026, time.October, 16, 11, 0, 0)); got != calendar.Closed {
		t.Errorf("Session(closed Friday 11:00) = %q, want %q", got, calendar.Closed)
	}
	if got := cal.NextOpen(thursdayTrading).Format(time.RFC3339); got != "2026-10-19T10:00:00+04:00" {
		t.Errorf("NextOpen(Thursday 11:00) = %s, want 2026-10-19T10:00:00+04:00", got)
	}
}
