package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/config"
	"github.com/saudm6/dfm-be/internal/dfm/dfmtest"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/instruments"
	"github.com/saudm6/dfm-be/internal/store"
	"github.com/saudm6/dfm-be/internal/store/storetest"
)

func TestRunErrors(t *testing.T) {
	// 01:00 on 2026-10-15 in Dubai, still 2026-10-14 in UTC.
	fixed := time.Date(2026, time.October, 14, 21, 0, 0, 0, time.UTC)
	clock = func() time.Time { return fixed }
	t.Cleanup(func() { clock = time.Now })
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
			name:    "calendar close before the calendar",
			args:    []string{"calendar", "close", "--date", "0000-01-01"},
			environ: nil,
			wantErr: "calendar close: --date 0000-01-01 is before 2025-01-01, the first day of the calendar",
		},
		{
			name:    "calendar close far ahead",
			args:    []string{"calendar", "close", "--date", "9999-01-01"},
			environ: nil,
			wantErr: "calendar close: --date 9999-01-01 is more than 2 years ahead; the last day accepted is 2028-10-15",
		},
		{
			name:    "sync of an unknown kind",
			args:    []string{"sync", "prices"},
			environ: nil,
			wantErr: usage,
		},
		{
			name:    "sync instruments without database url",
			args:    []string{"sync", "instruments"},
			environ: nil,
			wantErr: `config: env: required environment variable "DFM_DATABASE_URL" is not set`,
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

func TestParseClosureDate(t *testing.T) {
	// 01:00 on 2026-10-15 in Dubai, still 2026-10-14 in UTC.
	now := time.Date(2026, time.October, 14, 21, 0, 0, 0, time.UTC)
	tests := []struct {
		in      string
		want    string
		wantErr string
	}{
		{in: "2025-01-01", want: "2025-01-01T00:00:00+04:00"},
		{in: "2026-10-16", want: "2026-10-16T00:00:00+04:00"},
		{in: "2028-10-15", want: "2028-10-15T00:00:00+04:00"},
		{in: "2024-12-31", wantErr: "calendar close: --date 2024-12-31 is before 2025-01-01, the first day of the calendar"},
		{in: "0000-01-01", wantErr: "calendar close: --date 0000-01-01 is before 2025-01-01, the first day of the calendar"},
		{in: "2028-10-16", wantErr: "calendar close: --date 2028-10-16 is more than 2 years ahead; the last day accepted is 2028-10-15"},
		{in: "2062-10-16", wantErr: "calendar close: --date 2062-10-16 is more than 2 years ahead; the last day accepted is 2028-10-15"},
		{in: "2026-02-30", wantErr: `calendar close: --date "2026-02-30" is not YYYY-MM-DD`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseClosureDate(tt.in, now)

			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("parseClosureDate(%q) error = %v, want %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil || got.Format(time.RFC3339) != tt.want {
				t.Errorf("parseClosureDate(%q) = %s, %v, want %s", tt.in, got.Format(time.RFC3339), err, tt.want)
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
	scheduled := make(chan string, 1)
	go func() {
		dec := json.NewDecoder(logs)
		for {
			var line struct {
				Msg string `json:"msg"`
				At  string `json:"at"`
			}
			if dec.Decode(&line) != nil {
				close(scheduled)
				_, _ = io.Copy(io.Discard, logs)
				return
			}
			if line.Msg == "instrument sync scheduled" {
				scheduled <- line.At
			}
		}
	}()

	status, body := get(ctx, t, "http://"+addr+"/healthz")
	if want := "{\"status\":\"ok\",\"db\":\"ok\"}\n"; status != http.StatusOK || body != want {
		t.Errorf("GET /healthz = %d %q, want 200 %q", status, body, want)
	}

	before := time.Now()
	status, body = get(ctx, t, "http://"+addr+"/market/status")
	var market struct {
		Session  string    `json:"session"`
		NextOpen time.Time `json:"nextOpen"`
	}
	if err := json.Unmarshal([]byte(body), &market); err != nil || status != http.StatusOK {
		t.Fatalf("GET /market/status = %d %s (%v), want 200 JSON", status, body, err)
	}
	if !slices.Contains([]string{"pre-open", "continuous", "closing-auction", "closed"}, market.Session) {
		t.Errorf("session = %q, want a DFM session", market.Session)
	}
	if _, offset := market.NextOpen.Zone(); offset != 4*60*60 || market.NextOpen.Format("15:04") != "10:00" || !market.NextOpen.After(before) {
		t.Errorf("nextOpen = %s, want a later 10:00 +04:00", market.NextOpen.Format(time.RFC3339))
	}

	at, err := time.Parse(time.RFC3339, <-scheduled)
	if err != nil || at.Format("15:04 -07:00") != "09:00 +04:00" || !at.After(before) {
		t.Errorf("instrument sync scheduled at %s (%v), want a later 09:00 +04:00", at, err)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("run() after cancel error = %v, want nil", err)
	}
}

func TestRunSyncInstruments(t *testing.T) {
	clock = func() time.Time { return time.Date(2026, time.October, 15, 8, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { clock = time.Now })
	url := storetest.NewURL(t)
	if err := run(t.Context(), []string{"migrate"}, []string{"DFM_DATABASE_URL=" + url}, io.Discard); err != nil {
		t.Fatalf("run(migrate) error = %v", err)
	}
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())
	// Two lines keep the 2-per-second overview calls quick.
	srv.Replace("lite-securities-lists-en.json", writeFile(t, `{"Equities":[
		{"ID":"04e697f7-f602-6339-b0d9-ff00009be840","SecuritySymbol":"EMAAR","FullName":"Emaar Properties PJSC","Exchange":"DFM","Sector":"Real Estate","SecurityType":"Equity"},
		{"ID":"c8e597f7-f602-6339-b0d9-ff00009be840","SecuritySymbol":"EIB","FullName":"Emirates Islamic Bank PJSC","Exchange":"DFM","Sector":"Financials","SecurityType":"Equity"}]}`))
	environ := []string{"DFM_DATABASE_URL=" + url, "DFM_API_BASE_URL=" + srv.URL}
	tests := []struct {
		name    string
		wantLog string
	}{
		{"first sync", "instrument sync finished|11|11|8|8|0"},
		{"no upstream change", "instrument sync finished|11|0|8|0|0"},
	}
	runIDs := map[string]bool{}
	for _, tt := range tests {
		var logs bytes.Buffer
		if err := run(t.Context(), []string{"sync", "instruments"}, environ, &logs); err != nil {
			t.Fatalf("%s: run(sync instruments) error = %v", tt.name, err)
		}
		var line struct {
			Msg           string `json:"msg"`
			RunID         string `json:"run_id"`
			Instruments   int    `json:"instruments"`
			Written       int    `json:"written"`
			Boards        int    `json:"boards"`
			BoardsWritten int    `json:"boards_written"`
			Skipped       int    `json:"skipped"`
		}
		if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
			t.Fatalf("%s: decode log %q: %v", tt.name, logs.String(), err)
		}
		got := fmt.Sprintf("%s|%d|%d|%d|%d|%d", line.Msg, line.Instruments, line.Written, line.Boards, line.BoardsWritten, line.Skipped)
		if got != tt.wantLog {
			t.Errorf("%s: log = %q, want %q", tt.name, got, tt.wantLog)
		}
		if line.RunID == "" || runIDs[line.RunID] {
			t.Errorf("%s: run_id = %q, want a new id", tt.name, line.RunID)
		}
		runIDs[line.RunID] = true
	}

	pool, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	emaar, err := instruments.Get(t.Context(), pool, "EMAAR")
	if err != nil {
		t.Fatalf("Get(EMAAR) error = %v", err)
	}
	if got := fmt.Sprintf("%s %s %s %s", emaar.ISIN, emaar.Sector, emaar.Class, emaar.Exchange); got != "AEE000301011 Real Estate equity DFM" {
		t.Errorf("EMAAR = %q, want %q", got, "AEE000301011 Real Estate equity DFM")
	}
}

func TestRunSyncInstrumentsFails(t *testing.T) {
	url := storetest.NewURL(t)
	if err := run(t.Context(), []string{"migrate"}, []string{"DFM_DATABASE_URL=" + url}, io.Discard); err != nil {
		t.Fatalf("run(migrate) error = %v", err)
	}
	srv := dfmtest.NewServer(t, dfmtest.SecurityMasterDir())
	srv.FailNext("/web/widgets/v1/data", http.StatusForbidden)
	var logs bytes.Buffer

	err := run(t.Context(), []string{"sync", "instruments"}, []string{"DFM_DATABASE_URL=" + url, "DFM_API_BASE_URL=" + srv.URL}, &logs)

	want := "instruments: fetch security master: dfm: /web/widgets/v1/data: unexpected status 403"
	if err == nil || err.Error() != want {
		t.Fatalf("run(sync instruments) error = %v, want %q", err, want)
	}
	var line struct {
		Level string `json:"level"`
		Msg   string `json:"msg"`
		RunID string `json:"run_id"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("decode log %q: %v", logs.String(), err)
	}
	if line.Level != "ERROR" || line.Msg != "instrument sync failed" || line.RunID == "" || line.Error != want {
		t.Errorf("log = %+v, want an ERROR with the run id and %q", line, want)
	}
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
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
