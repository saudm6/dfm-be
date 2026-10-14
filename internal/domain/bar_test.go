package domain_test

import (
	"reflect"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/domain"
)

var newYork = mustLoadLocation("America/New_York")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func minuteBar() domain.Bar {
	return domain.Bar{
		Symbol:    "EMAAR",
		Interval:  domain.OneMinute,
		Start:     time.Date(2026, 10, 14, 10, 1, 0, 0, gst),
		Open:      dec("14.05"),
		High:      dec("14.10"),
		Low:       dec("14.00"),
		Close:     dec("14.10"),
		Volume:    250000,
		Value:     dec("3512500.00"),
		Source:    domain.SourceIBKR,
		Freshness: domain.RealTime,
	}
}

func dayBar() domain.Bar {
	return domain.Bar{
		Symbol:    "EMAAR",
		Interval:  domain.OneDay,
		Start:     time.Date(2026, 10, 14, 0, 0, 0, 0, gst),
		Open:      dec("14.00"),
		High:      dec("14.25"),
		Low:       dec("13.90"),
		Close:     dec("14.20"),
		Volume:    9876543,
		Value:     dec("139753583.45"),
		Source:    domain.SourceDFMHistory,
		Freshness: domain.EndOfDay,
	}
}

func TestNewBar(t *testing.T) {
	tests := []struct {
		name    string
		base    func() domain.Bar
		edit    func(*domain.Bar)
		wantErr string
	}{
		{name: "valid minute bar", base: minuteBar, edit: func(*domain.Bar) {}},
		{name: "valid day bar", base: dayBar, edit: func(*domain.Bar) {}},
		{name: "day bar at Dubai midnight in Dubai", base: dayBar, edit: func(b *domain.Bar) {
			b.Start = time.Date(2026, 10, 14, 0, 0, 0, 0, gst)
		}},
		{name: "day bar at Dubai midnight in UTC", base: dayBar, edit: func(b *domain.Bar) {
			b.Start = time.Date(2026, 10, 13, 20, 0, 0, 0, time.UTC)
		}},
		{name: "day bar at Dubai midnight in New York", base: dayBar, edit: func(b *domain.Bar) {
			b.Start = time.Date(2026, 10, 13, 20, 0, 0, 0, time.UTC).In(newYork)
		}},
		{name: "day bar at UTC midnight", base: dayBar, edit: func(b *domain.Bar) {
			b.Start = time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
		}, wantErr: `domain: invalid bar: start 2026-10-14T00:00:00Z is not midnight in Dubai`},
		{name: "day bar at New York midnight", base: dayBar, edit: func(b *domain.Bar) {
			b.Start = time.Date(2026, 10, 14, 0, 0, 0, 0, newYork)
		}, wantErr: `domain: invalid bar: start 2026-10-14T00:00:00-04:00 is not midnight in Dubai`},
		{name: "day with no trades", base: dayBar, edit: func(b *domain.Bar) {
			b.Open, b.High, b.Low, b.Close, b.Value = decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
			b.Volume = 0
		}},
		{name: "no prices but volume and value", base: dayBar, edit: func(b *domain.Bar) {
			b.Open, b.High, b.Low, b.Close = decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
		}, wantErr: `domain: invalid bar: volume 9876543 and value 139753583.45 with no prices`},
		{name: "no prices but value", base: dayBar, edit: func(b *domain.Bar) {
			b.Open, b.High, b.Low, b.Close = decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
			b.Volume = 0
		}, wantErr: `domain: invalid bar: volume 0 and value 139753583.45 with no prices`},
		{name: "missing open", base: dayBar, edit: func(b *domain.Bar) { b.Open = decimal.Zero },
			wantErr: `domain: invalid bar: open is zero but other prices are set`},
		{name: "missing high", base: dayBar, edit: func(b *domain.Bar) { b.High = decimal.Zero },
			wantErr: `domain: invalid bar: high is zero but other prices are set`},
		{name: "missing low", base: dayBar, edit: func(b *domain.Bar) { b.Low = decimal.Zero },
			wantErr: `domain: invalid bar: low is zero but other prices are set`},
		{name: "missing close", base: dayBar, edit: func(b *domain.Bar) { b.Close = decimal.Zero },
			wantErr: `domain: invalid bar: close is zero but other prices are set`},
		{name: "zero value", base: minuteBar, edit: func(b *domain.Bar) { *b = domain.Bar{} },
			wantErr: `domain: invalid bar: symbol "" is not a trading code`},
		{name: "unknown interval", base: minuteBar, edit: func(b *domain.Bar) { b.Interval = "5m" },
			wantErr: `domain: invalid bar: unknown interval "5m"`},
		{name: "unknown source", base: minuteBar, edit: func(b *domain.Bar) { b.Source = "yahoo" },
			wantErr: `domain: invalid bar: unknown source "yahoo"`},
		{name: "unknown freshness", base: minuteBar, edit: func(b *domain.Bar) { b.Freshness = "live" },
			wantErr: `domain: invalid bar: unknown freshness "live"`},
		{name: "missing freshness", base: dayBar, edit: func(b *domain.Bar) { b.Freshness = "" },
			wantErr: `domain: invalid bar: unknown freshness ""`},
		{name: "missing start", base: minuteBar, edit: func(b *domain.Bar) { b.Start = time.Time{} },
			wantErr: `domain: invalid bar: start time is missing`},
		{name: "minute bar off the minute", base: minuteBar, edit: func(b *domain.Bar) {
			b.Start = time.Date(2026, 10, 14, 10, 1, 30, 0, gst)
		}, wantErr: `domain: invalid bar: start 2026-10-14T10:01:30+04:00 is not on a minute boundary`},
		{name: "day bar not at midnight", base: dayBar, edit: func(b *domain.Bar) {
			b.Start = time.Date(2026, 10, 14, 10, 0, 0, 0, gst)
		}, wantErr: `domain: invalid bar: start 2026-10-14T10:00:00+04:00 is not midnight in Dubai`},
		{name: "negative open", base: minuteBar, edit: func(b *domain.Bar) { b.Open = dec("-14.05") },
			wantErr: `domain: invalid bar: open -14.05 is negative`},
		{name: "negative high", base: minuteBar, edit: func(b *domain.Bar) { b.High = dec("-1") },
			wantErr: `domain: invalid bar: high -1 is negative`},
		{name: "negative low", base: minuteBar, edit: func(b *domain.Bar) { b.Low = dec("-0.01") },
			wantErr: `domain: invalid bar: low -0.01 is negative`},
		{name: "negative close", base: minuteBar, edit: func(b *domain.Bar) { b.Close = dec("-14.1") },
			wantErr: `domain: invalid bar: close -14.1 is negative`},
		{name: "negative value", base: minuteBar, edit: func(b *domain.Bar) { b.Value = dec("-1") },
			wantErr: `domain: invalid bar: value -1 is negative`},
		{name: "negative volume", base: minuteBar, edit: func(b *domain.Bar) { b.Volume = -1 },
			wantErr: `domain: invalid bar: volume -1 is negative`},
		{name: "high below low", base: minuteBar, edit: func(b *domain.Bar) { b.High = dec("13.95") },
			wantErr: `domain: invalid bar: high 13.95 is below low 14`},
		{name: "open above high", base: minuteBar, edit: func(b *domain.Bar) { b.Open = dec("14.15") },
			wantErr: `domain: invalid bar: open 14.15 is outside low 14 and high 14.1`},
		{name: "close below low", base: dayBar, edit: func(b *domain.Bar) { b.Close = dec("13.85") },
			wantErr: `domain: invalid bar: close 13.85 is outside low 13.9 and high 14.25`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.base()
			tt.edit(&in)
			got, err := domain.NewBar(in)
			checkErr(t, err, tt.wantErr)
			want := in
			if tt.wantErr != "" {
				want = domain.Bar{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("NewBar() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestTradingDayStart(t *testing.T) {
	tests := []struct {
		year  int
		month time.Month
		day   int
		want  string
	}{
		{year: 2026, month: time.October, day: 14, want: "2026-10-13T20:00:00Z"},
		{year: 2000, month: time.March, day: 26, want: "2000-03-25T20:00:00Z"},
		{year: 2027, month: time.January, day: 1, want: "2026-12-31T20:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := domain.TradingDayStart(tt.year, tt.month, tt.day)
			if s := got.UTC().Format(time.RFC3339Nano); s != tt.want {
				t.Errorf("TradingDayStart(%d, %d, %d) = %s, want %s", tt.year, tt.month, tt.day, s, tt.want)
			}
			if got.Location() != domain.DubaiTZ {
				t.Errorf("TradingDayStart location = %v, want DubaiTZ", got.Location())
			}
			b := dayBar()
			b.Start = got.UTC()
			if _, err := domain.NewBar(b); err != nil {
				t.Errorf("NewBar with Start %s: %v", tt.want, err)
			}
		})
	}
}
