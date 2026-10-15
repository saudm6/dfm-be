package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/config"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/store"
	"github.com/saudm6/dfm-be/internal/store/storetest"
)

var allMigrations = []int64{20261015120000, 20261015125900}

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
			wantErr: usage,
		},
		{
			name:    "calendar close without date",
			args:    []string{"calendar", "close"},
			environ: nil,
			wantErr: usage,
		},
		{
			name:    "calendar close with empty reason",
			args:    []string{"calendar", "close", "--date", "2026-10-16", "--reason", ""},
			environ: nil,
			wantErr: usage,
		},
		{
			name:    "calendar close with unknown flag",
			args:    []string{"calendar", "close", "--date", "2026-10-16", "--all"},
			environ: nil,
			wantErr: usage,
		},
		{
			name:    "calendar close with bad date",
			args:    []string{"calendar", "close", "--date", "16/10/2026"},
			environ: nil,
			wantErr: `calendar close: --date "16/10/2026" is not YYYY-MM-DD`,
		},
		{
			name:    "calendar close without database url",
			args:    []string{"calendar", "close", "--date", "2026-10-16"},
			environ: nil,
			wantErr: `config: env: required environment variable "DFM_DATABASE_URL" is not set`,
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
		{name: "empty database", wantApplied: allMigrations},
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

func TestRunCalendarClose(t *testing.T) {
	url := storetest.NewURL(t)
	environ := []string{"DFM_DATABASE_URL=" + url}
	if err := run(t.Context(), []string{"migrate"}, environ, io.Discard); err != nil {
		t.Fatalf("run(migrate) error = %v", err)
	}
	tests := []struct {
		name    string
		args    []string
		wantLog string
	}{
		{"new closure", []string{"calendar", "close", "--date", "2026-10-16"}, "market closure recorded|2026-10-16"},
		{"same day again", []string{"calendar", "close", "--date=2026-10-16", "--reason", "Systems outage"}, "market already closed|2026-10-16"},
		{"seeded holiday", []string{"calendar", "close", "--date", "2026-12-02"}, "market already closed|2026-12-02"},
	}
	for _, tt := range tests {
		var logs bytes.Buffer
		if err := run(t.Context(), tt.args, environ, &logs); err != nil {
			t.Fatalf("%s: run(%v) error = %v", tt.name, tt.args, err)
		}
		var line struct {
			Msg  string `json:"msg"`
			Date string `json:"date"`
		}
		if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
			t.Fatalf("%s: decode log %q: %v", tt.name, logs.String(), err)
		}
		if got := line.Msg + "|" + line.Date; got != tt.wantLog {
			t.Errorf("%s: log = %q, want %q", tt.name, got, tt.wantLog)
		}
	}

	pool, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cfg, err := config.Load([]string{"DFM_HTTP_ADDR=:0", "DFM_DATABASE_URL=" + url})
	if err != nil {
		t.Fatal(err)
	}
	cal, err := calendar.Loader{DB: pool, Hours: cfg.Market.Hours()}.Load(t.Context())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	thursday := time.Date(2026, time.October, 15, 11, 0, 0, 0, domain.DubaiTZ)
	if got := cal.NextOpen(thursday).Format(time.RFC3339); got != "2026-10-19T10:00:00+04:00" {
		t.Errorf("NextOpen(Thursday before the closure) = %s, want 2026-10-19T10:00:00+04:00", got)
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

	status, body := get(ctx, t, "http://"+addr+"/healthz")
	if status != http.StatusOK || body != `{"status":"ok","db":"ok"}` {
		t.Errorf("GET /healthz = %d %s, want 200 {\"status\":\"ok\",\"db\":\"ok\"}", status, body)
	}

	before := time.Now()
	status, body = get(ctx, t, "http://"+addr+"/market/status")
	var market struct {
		Session  string    `json:"session"`
		NextOpen time.Time `json:"next_open"`
	}
	if err := json.Unmarshal([]byte(body), &market); err != nil || status != http.StatusOK {
		t.Fatalf("GET /market/status = %d %s (%v), want 200 JSON", status, body, err)
	}
	if !slices.Contains([]string{"pre-open", "continuous", "closing-auction", "closed"}, market.Session) {
		t.Errorf("session = %q, want a DFM session", market.Session)
	}
	if _, offset := market.NextOpen.Zone(); offset != 4*60*60 || market.NextOpen.Format("15:04") != "10:00" || !market.NextOpen.After(before) {
		t.Errorf("next_open = %s, want a later 10:00 +04:00", market.NextOpen.Format(time.RFC3339))
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("run() after cancel error = %v, want nil", err)
	}
}

func get(ctx context.Context, t *testing.T, url string) (status int, body string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(b)
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
			if !slices.Equal(line.Applied, allMigrations) {
				t.Errorf("startup applied %v, want %v", line.Applied, allMigrations)
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
