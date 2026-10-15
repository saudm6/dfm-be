package config_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/config"
)

const dbURL = "postgres://dfm:dfm@localhost:5432/dfm?sslmode=disable"

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
				Session:  config.Session{SessionIdleTimeout: 12 * time.Hour, SessionMaxAge: 168 * time.Hour, CookieSecure: true},
				Base:     config.Base{LogLevel: slog.LevelInfo, DatabaseURL: dbURL},
			},
		},
		{
			name: "all set",
			environ: []string{
				"DFM_HTTP_ADDR=127.0.0.1:9000", "DFM_LOG_LEVEL=debug", "DFM_DATABASE_URL=" + dbURL,
				"DFM_SESSION_IDLE_TIMEOUT=30m", "DFM_SESSION_MAX_AGE=24h", "DFM_COOKIE_SECURE=false",
			},
			want: config.Config{
				HTTPAddr: "127.0.0.1:9000",
				Session:  config.Session{SessionIdleTimeout: 30 * time.Minute, SessionMaxAge: 24 * time.Hour, CookieSecure: false},
				Base:     config.Base{LogLevel: slog.LevelDebug, DatabaseURL: dbURL},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Load(tt.environ)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got != tt.want {
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
			name:    "zero idle timeout",
			environ: []string{"DFM_HTTP_ADDR=:8080", "DFM_DATABASE_URL=" + dbURL, "DFM_SESSION_IDLE_TIMEOUT=0s"},
			wantErr: `config: DFM_SESSION_IDLE_TIMEOUT and DFM_SESSION_MAX_AGE must be positive`,
		},
		{
			name:    "negative max age",
			environ: []string{"DFM_HTTP_ADDR=:8080", "DFM_DATABASE_URL=" + dbURL, "DFM_SESSION_MAX_AGE=-1h"},
			wantErr: `config: DFM_SESSION_IDLE_TIMEOUT and DFM_SESSION_MAX_AGE must be positive`,
		},
		{
			name:    "invalid cookie switch",
			environ: []string{"DFM_HTTP_ADDR=:8080", "DFM_DATABASE_URL=" + dbURL, "DFM_COOKIE_SECURE=maybe"},
			wantErr: `config: env: parse error on field "CookieSecure" of type "bool": strconv.ParseBool: parsing "maybe": invalid syntax`,
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
