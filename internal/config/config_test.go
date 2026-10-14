package config_test

import (
	"log/slog"
	"testing"

	"github.com/saudm6/dfm-be/internal/config"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
		want    config.Config
	}{
		{
			name:    "defaults",
			environ: []string{"DFM_HTTP_ADDR=:8080"},
			want:    config.Config{HTTPAddr: ":8080", LogLevel: slog.LevelInfo},
		},
		{
			name:    "all set",
			environ: []string{"DFM_HTTP_ADDR=127.0.0.1:9000", "DFM_LOG_LEVEL=debug"},
			want:    config.Config{HTTPAddr: "127.0.0.1:9000", LogLevel: slog.LevelDebug},
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
			name:    "missing required variable",
			environ: nil,
			wantErr: `config: env: required environment variable "DFM_HTTP_ADDR" is not set`,
		},
		{
			name:    "empty required variable",
			environ: []string{"DFM_HTTP_ADDR="},
			wantErr: `config: env: environment variable "DFM_HTTP_ADDR" should not be empty`,
		},
		{
			name:    "invalid log level",
			environ: []string{"DFM_HTTP_ADDR=:8080", "DFM_LOG_LEVEL=loud"},
			wantErr: `config: env: parse error on field "LogLevel" of type "slog.Level": slog: level string "loud": unknown name`,
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
