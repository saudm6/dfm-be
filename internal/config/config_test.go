package config_test

import (
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/config"
)

const dbURL = "postgres://dfm:dfm@localhost:5432/dfm?sslmode=disable"

var defaultMarket = config.Market{
	PreOpen:        9*60 + 30,
	Open:           10 * 60,
	ClosingAuction: 14*60 + 45,
	Close:          15 * 60,
	TradingDays: []calendar.Weekday{
		calendar.Weekday(time.Monday), calendar.Weekday(time.Tuesday), calendar.Weekday(time.Wednesday),
		calendar.Weekday(time.Thursday), calendar.Weekday(time.Friday),
	},
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
		want    config.Config
	}{
		{
			name:    "defaults",
			environ: []string{"DFM_HTTP_ADDR=:8080", "DFM_DATABASE_URL=" + dbURL},
			want: config.Config{
				HTTPAddr: ":8080",
				Base:     config.Base{LogLevel: slog.LevelInfo, DatabaseURL: dbURL},
				Market:   defaultMarket,
			},
		},
		{
			name: "all set",
			environ: []string{
				"DFM_HTTP_ADDR=127.0.0.1:9000", "DFM_LOG_LEVEL=debug", "DFM_DATABASE_URL=" + dbURL,
				"DFM_SESSION_PRE_OPEN=09:00", "DFM_SESSION_OPEN=09:30", "DFM_SESSION_CLOSING_AUCTION=13:45",
				"DFM_SESSION_CLOSE=14:00", "DFM_TRADING_DAYS=Sun,Mon,Tue,Wed,Thu",
			},
			want: config.Config{
				HTTPAddr: "127.0.0.1:9000",
				Base:     config.Base{LogLevel: slog.LevelDebug, DatabaseURL: dbURL},
				Market: config.Market{
					PreOpen:        9 * 60,
					Open:           9*60 + 30,
					ClosingAuction: 13*60 + 45,
					Close:          14 * 60,
					TradingDays: []calendar.Weekday{
						calendar.Weekday(time.Sunday), calendar.Weekday(time.Monday), calendar.Weekday(time.Tuesday),
						calendar.Weekday(time.Wednesday), calendar.Weekday(time.Thursday),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Load(tt.environ)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
		wantErr string
	}{
		{
			name:    "missing required variables",
			environ: nil,
			wantErr: `config: env: required environment variable "DFM_HTTP_ADDR" is not set; required environment variable "DFM_DATABASE_URL" is not set`,
		},
		{
			name:    "empty required variable",
			environ: []string{"DFM_HTTP_ADDR=", "DFM_DATABASE_URL=" + dbURL},
			wantErr: `config: env: environment variable "DFM_HTTP_ADDR" should not be empty`,
		},
		{
			name:    "missing database url",
			environ: []string{"DFM_HTTP_ADDR=:8080"},
			wantErr: `config: env: required environment variable "DFM_DATABASE_URL" is not set`,
		},
		{
			name:    "invalid log level",
			environ: []string{"DFM_HTTP_ADDR=:8080", "DFM_LOG_LEVEL=loud", "DFM_DATABASE_URL=" + dbURL},
			wantErr: `config: env: parse error on field "LogLevel" of type "slog.Level": slog: level string "loud": unknown name`,
		},
		{
			name:    "invalid session time",
			environ: []string{"DFM_HTTP_ADDR=:8080", "DFM_DATABASE_URL=" + dbURL, "DFM_SESSION_OPEN=10am"},
			wantErr: `config: env: parse error on field "Open" of type "calendar.TimeOfDay": calendar: time of day "10am" is not HH:MM`,
		},
		{
			name:    "sessions out of order",
			environ: []string{"DFM_HTTP_ADDR=:8080", "DFM_DATABASE_URL=" + dbURL, "DFM_SESSION_CLOSE=14:00"},
			wantErr: `config: calendar: sessions 09:30, 10:00, 14:45, 14:00 are not in order within one day`,
		},
		{
			name:    "unknown trading day",
			environ: []string{"DFM_HTTP_ADDR=:8080", "DFM_DATABASE_URL=" + dbURL, "DFM_TRADING_DAYS=Mon,Funday"},
			wantErr: `config: env: parse error on field "TradingDays" of type "[]calendar.Weekday": calendar: "Funday" is not a day of the week`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(tt.environ)
			if err == nil {
				t.Fatalf("Load() error = nil, want %q", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("Load() error = %q, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadBase(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
		want    config.Base
		wantErr string
	}{
		{
			name:    "no http address needed",
			environ: []string{"DFM_DATABASE_URL=" + dbURL},
			want:    config.Base{LogLevel: slog.LevelInfo, DatabaseURL: dbURL},
		},
		{
			name:    "missing database url",
			environ: []string{"DFM_HTTP_ADDR=:8080"},
			wantErr: `config: env: required environment variable "DFM_DATABASE_URL" is not set`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.LoadBase(tt.environ)
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if gotErr != tt.wantErr {
				t.Fatalf("LoadBase() error = %q, want %q", gotErr, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("LoadBase() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
