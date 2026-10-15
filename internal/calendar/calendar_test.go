package calendar_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/domain"
)

var weekdays = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}

var dfmHours = calendar.Hours{
	PreOpen:        9*60 + 30,
	Open:           10 * 60,
	ClosingAuction: 14*60 + 45,
	Close:          15 * 60,
	Week:           weekdays,
}

// dubai returns the instant at the given Dubai wall-clock time.
func dubai(year int, month time.Month, day, hour, minute, sec int) time.Time {
	return time.Date(year, month, day, hour, minute, sec, 0, domain.DubaiTZ)
}

// testCalendar is closed on UAE National Day 2026 (2-3 December).
func testCalendar(t *testing.T) *calendar.Calendar {
	t.Helper()
	cal, err := calendar.New(dfmHours, []time.Time{
		domain.TradingDayStart(2026, time.December, 2),
		domain.TradingDayStart(2026, time.December, 3),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return cal
}

func TestSession(t *testing.T) {
	cal := testCalendar(t)
	tests := []struct {
		name string
		at   time.Time
		want calendar.Session
	}{
		{"before pre-open", dubai(2026, time.October, 15, 9, 29, 59), calendar.Closed},
		{"pre-open starts", dubai(2026, time.October, 15, 9, 30, 0), calendar.PreOpen},
		{"last pre-open nanosecond", dubai(2026, time.October, 15, 9, 59, 59).Add(999999999), calendar.PreOpen},
		{"continuous starts", dubai(2026, time.October, 15, 10, 0, 0), calendar.Continuous},
		{"continuous before auction", dubai(2026, time.October, 15, 14, 44, 59), calendar.Continuous},
		{"closing auction starts", dubai(2026, time.October, 15, 14, 45, 0), calendar.ClosingAuction},
		{"trade at last", dubai(2026, time.October, 15, 14, 59, 59), calendar.ClosingAuction},
		{"close", dubai(2026, time.October, 15, 15, 0, 0), calendar.Closed},
		{"midnight", dubai(2026, time.October, 15, 0, 0, 0), calendar.Closed},
		{"UTC instant in continuous", time.Date(2026, time.October, 15, 6, 0, 0, 0, time.UTC), calendar.Continuous},
		{"UTC instant before pre-open", time.Date(2026, time.October, 15, 5, 29, 0, 0, time.UTC), calendar.Closed},
		{"Friday trades", dubai(2026, time.October, 16, 11, 0, 0), calendar.Continuous},
		{"Saturday", dubai(2026, time.October, 17, 11, 0, 0), calendar.Closed},
		{"Sunday", dubai(2026, time.October, 18, 11, 0, 0), calendar.Closed},
		{"holiday", dubai(2026, time.December, 2, 11, 0, 0), calendar.Closed},
		{"holiday pre-open", dubai(2026, time.December, 3, 9, 45, 0), calendar.Closed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cal.Session(tt.at); got != tt.want {
				t.Errorf("Session(%s) = %q, want %q", tt.at, got, tt.want)
			}
		})
	}
}

func TestIsTradingDay(t *testing.T) {
	cal := testCalendar(t)
	tests := []struct {
		name string
		day  time.Time
		want bool
	}{
		{"Thursday", domain.TradingDayStart(2026, time.October, 15), true},
		{"Friday", domain.TradingDayStart(2026, time.October, 16), true},
		{"Saturday", domain.TradingDayStart(2026, time.October, 17), false},
		{"Sunday", domain.TradingDayStart(2026, time.October, 18), false},
		{"Monday", domain.TradingDayStart(2026, time.October, 19), true},
		{"holiday", domain.TradingDayStart(2026, time.December, 2), false},
		{"second holiday", domain.TradingDayStart(2026, time.December, 3), false},
		{"day after holidays", domain.TradingDayStart(2026, time.December, 4), true},
		{"late on a holiday", dubai(2026, time.December, 2, 23, 59, 59), false},
		{"UTC evening before a Saturday is Saturday in Dubai", time.Date(2026, time.October, 16, 20, 0, 0, 0, time.UTC), false},
		{"UTC evening before a Monday is Monday in Dubai", time.Date(2026, time.October, 18, 20, 0, 0, 0, time.UTC), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cal.IsTradingDay(tt.day); got != tt.want {
				t.Errorf("IsTradingDay(%s) = %v, want %v", tt.day, got, tt.want)
			}
		})
	}
}

func TestNextOpen(t *testing.T) {
	cal := testCalendar(t)
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{"before pre-open", dubai(2026, time.October, 15, 8, 0, 0), "2026-10-15T10:00:00+04:00"},
		{"in pre-open", dubai(2026, time.October, 15, 9, 45, 0), "2026-10-15T10:00:00+04:00"},
		{"at the open", dubai(2026, time.October, 15, 10, 0, 0), "2026-10-16T10:00:00+04:00"},
		{"Thursday after close", dubai(2026, time.October, 15, 16, 0, 0), "2026-10-16T10:00:00+04:00"},
		{"Friday after close skips the weekend", dubai(2026, time.October, 16, 15, 0, 0), "2026-10-19T10:00:00+04:00"},
		{"Saturday", dubai(2026, time.October, 17, 12, 0, 0), "2026-10-19T10:00:00+04:00"},
		{"Sunday midnight UTC", time.Date(2026, time.October, 18, 20, 0, 0, 0, time.UTC), "2026-10-19T10:00:00+04:00"},
		{"before holidays", dubai(2026, time.December, 1, 15, 30, 0), "2026-12-04T10:00:00+04:00"},
		{"on a holiday", dubai(2026, time.December, 2, 9, 45, 0), "2026-12-04T10:00:00+04:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cal.NextOpen(tt.at).Format(time.RFC3339); got != tt.want {
				t.Errorf("NextOpen(%s) = %s, want %s", tt.at, got, tt.want)
			}
		})
	}
}

func TestConfiguredWeekAndHours(t *testing.T) {
	sunToThu := calendar.Hours{
		PreOpen:        9 * 60,
		Open:           9*60 + 30,
		ClosingAuction: 13*60 + 45,
		Close:          14 * 60,
		Week:           []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday},
	}
	cal, err := calendar.New(sunToThu, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tests := []struct {
		name        string
		at          time.Time
		wantSession calendar.Session
		wantNext    string
	}{
		{"Thursday continuous", dubai(2026, time.October, 15, 9, 30, 0), calendar.Continuous, "2026-10-18T09:30:00+04:00"},
		{"Thursday closing auction", dubai(2026, time.October, 15, 13, 45, 0), calendar.ClosingAuction, "2026-10-18T09:30:00+04:00"},
		{"Friday is off", dubai(2026, time.October, 16, 11, 0, 0), calendar.Closed, "2026-10-18T09:30:00+04:00"},
		{"Sunday pre-open", dubai(2026, time.October, 18, 9, 0, 0), calendar.PreOpen, "2026-10-18T09:30:00+04:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cal.Session(tt.at); got != tt.wantSession {
				t.Errorf("Session(%s) = %q, want %q", tt.at, got, tt.wantSession)
			}
			if got := cal.NextOpen(tt.at).Format(time.RFC3339); got != tt.wantNext {
				t.Errorf("NextOpen(%s) = %s, want %s", tt.at, got, tt.wantNext)
			}
		})
	}
}

func TestNewRejectsInvalidHours(t *testing.T) {
	tests := []struct {
		name    string
		hours   calendar.Hours
		wantErr string
	}{
		{
			name:    "open before pre-open",
			hours:   calendar.Hours{PreOpen: 10 * 60, Open: 9*60 + 30, ClosingAuction: 14*60 + 45, Close: 15 * 60, Week: weekdays},
			wantErr: "calendar: sessions 10:00, 09:30, 14:45, 15:00 are not in order within one day",
		},
		{
			name:    "empty closing auction",
			hours:   calendar.Hours{PreOpen: 9*60 + 30, Open: 10 * 60, ClosingAuction: 15 * 60, Close: 15 * 60, Week: weekdays},
			wantErr: "calendar: sessions 09:30, 10:00, 15:00, 15:00 are not in order within one day",
		},
		{
			name:    "no trading days",
			hours:   calendar.Hours{PreOpen: 9*60 + 30, Open: 10 * 60, ClosingAuction: 14*60 + 45, Close: 15 * 60},
			wantErr: "calendar: the trading week has no days",
		},
		{
			name:    "not a weekday",
			hours:   calendar.Hours{PreOpen: 9*60 + 30, Open: 10 * 60, ClosingAuction: 14*60 + 45, Close: 15 * 60, Week: []time.Weekday{7}},
			wantErr: "calendar: 7 is not a day of the week",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := calendar.New(tt.hours, nil)
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("New() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		in      string
		want    calendar.TimeOfDay
		wantErr string
	}{
		{in: "09:30", want: 570},
		{in: "00:00", want: 0},
		{in: "23:59", want: 1439},
		{in: "9:30", wantErr: `calendar: time of day "9:30" is not HH:MM`},
		{in: "24:00", wantErr: `calendar: time of day "24:00" is not HH:MM`},
		{in: "10:00:00", wantErr: `calendar: time of day "10:00:00" is not HH:MM`},
		{in: "", wantErr: `calendar: time of day "" is not HH:MM`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := calendar.ParseTimeOfDay(tt.in)
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if got != tt.want || gotErr != tt.wantErr {
				t.Errorf("ParseTimeOfDay(%q) = %d, %q, want %d, %q", tt.in, got, gotErr, tt.want, tt.wantErr)
			}
		})
	}
}

func TestTimeOfDayString(t *testing.T) {
	if got := calendar.TimeOfDay(14*60 + 5).String(); got != "14:05" {
		t.Errorf("String() = %q, want %q", got, "14:05")
	}
}

func TestWeekdayUnmarshalText(t *testing.T) {
	tests := []struct {
		in      string
		want    calendar.Weekday
		wantErr string
	}{
		{in: "Mon", want: calendar.Weekday(time.Monday)},
		{in: "friday", want: calendar.Weekday(time.Friday)},
		{in: " SUN ", want: calendar.Weekday(time.Sunday)},
		{in: "Mo", wantErr: `calendar: "Mo" is not a day of the week`},
		{in: "Fri,Sat", wantErr: `calendar: "Fri,Sat" is not a day of the week`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			var got calendar.Weekday
			err := got.UnmarshalText([]byte(tt.in))
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if got != tt.want || gotErr != tt.wantErr {
				t.Errorf("UnmarshalText(%q) = %d, %q, want %d, %q", tt.in, got, gotErr, tt.want, tt.wantErr)
			}
		})
	}
}

func TestParseSession(t *testing.T) {
	tests := []struct {
		in      string
		want    calendar.Session
		wantErr string
	}{
		{in: "pre-open", want: calendar.PreOpen},
		{in: "continuous", want: calendar.Continuous},
		{in: "closing-auction", want: calendar.ClosingAuction},
		{in: "closed", want: calendar.Closed},
		{in: "Closed", wantErr: `calendar: invalid session: unknown value "Closed"`},
		{in: "", wantErr: `calendar: invalid session: unknown value ""`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := calendar.ParseSession(tt.in)
			var bound calendar.Session
			bindErr := bound.Bind(tt.in)
			var decoded struct{ S calendar.Session }
			jsonErr := json.Unmarshal([]byte(`{"S":"`+tt.in+`"}`), &decoded)

			if tt.wantErr == "" {
				if err != nil || bindErr != nil || jsonErr != nil {
					t.Fatalf("ParseSession, Bind, json errors = %v, %v, %v, want nil", err, bindErr, jsonErr)
				}
				if got != tt.want || bound != tt.want || decoded.S != tt.want {
					t.Errorf("ParseSession, Bind, json = %q, %q, %q, want %q", got, bound, decoded.S, tt.want)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr || !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("ParseSession(%q) error = %v, want %q wrapping domain.ErrInvalid", tt.in, err, tt.wantErr)
			}
			if !errors.Is(bindErr, domain.ErrInvalid) || !errors.Is(jsonErr, domain.ErrInvalid) {
				t.Errorf("Bind, json errors = %v, %v, want domain.ErrInvalid", bindErr, jsonErr)
			}
		})
	}
}
