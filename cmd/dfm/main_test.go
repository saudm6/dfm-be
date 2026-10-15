package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/saudm6/dfm-be/internal/store/storetest"
)

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		environ []string
		wantErr string
	}{
		{
			name:    "serve without configuration",
			args:    nil,
			environ: nil,
			wantErr: `config: env: required environment variable "DFM_HTTP_ADDR" is not set; required environment variable "DFM_DATABASE_URL" is not set`,
		},
		{
			name:    "migrate without database url",
			args:    []string{"migrate"},
			environ: []string{"DFM_HTTP_ADDR=:0"},
			wantErr: `config: env: required environment variable "DFM_DATABASE_URL" is not set`,
		},
		{
			name:    "unknown command",
			args:    []string{"seed"},
			environ: nil,
			wantErr: `usage: dfm [migrate]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(t.Context(), tt.args, tt.environ, io.Discard)

			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("run() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunMigrateIsIdempotent(t *testing.T) {
	environ := []string{"DFM_DATABASE_URL=" + storetest.NewURL(t)}
	tests := []struct {
		name        string
		wantApplied []int64
	}{
		{name: "empty database", wantApplied: storetest.MigrationVersions(t)},
		{name: "second run", wantApplied: []int64{}},
	}
	for _, tt := range tests {
		var logs bytes.Buffer
		if err := run(t.Context(), []string{"migrate"}, environ, &logs); err != nil {
			t.Fatalf("%s: run(migrate) error = %v", tt.name, err)
		}
		var line struct {
			Msg     string  `json:"msg"`
			Applied []int64 `json:"applied"`
		}
		if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
			t.Fatalf("%s: decode log %q: %v", tt.name, logs.String(), err)
		}
		if line.Msg != "database migrated" || !slices.Equal(line.Applied, tt.wantApplied) {
			t.Errorf("%s: log = %q %v, want %q %v", tt.name, line.Msg, line.Applied, "database migrated", tt.wantApplied)
		}
	}
}

func TestRunServesHealthzUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	environ := []string{"DFM_HTTP_ADDR=127.0.0.1:0", "DFM_DATABASE_URL=" + storetest.NewURL(t)}
	logs, logw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := run(ctx, nil, environ, logw)
		_ = logw.Close()
		done <- err
	}()

	addr := listenAddr(t, logs)
	go func() { _, _ = io.Copy(io.Discard, logs) }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if want := "{\"status\":\"ok\",\"db\":\"ok\"}\n"; resp.StatusCode != http.StatusOK || string(body) != want {
		t.Errorf("GET /healthz = %d %q, want 200 %q", resp.StatusCode, body, want)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("run() after cancel error = %v, want nil", err)
	}
}

// listenAddr reads JSON log lines until the server reports its address. It
// fails unless the server migrated the database before listening.
func listenAddr(t *testing.T, logs io.Reader) string {
	t.Helper()
	dec := json.NewDecoder(logs)
	migrated := false
	for {
		var line struct {
			Msg     string  `json:"msg"`
			Addr    string  `json:"addr"`
			Applied []int64 `json:"applied"`
		}
		if err := dec.Decode(&line); err != nil {
			t.Fatalf("server never logged its address: %v", err)
		}
		switch line.Msg {
		case "database migrated":
			if want := storetest.MigrationVersions(t); !slices.Equal(line.Applied, want) {
				t.Errorf("startup applied %v, want %v", line.Applied, want)
			}
			migrated = true
		case "http server listening":
			if !migrated {
				t.Error("server listened before migrating the database")
			}
			return line.Addr
		}
	}
}
