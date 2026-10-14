package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestRunStopsWhenRequiredVariableMissing(t *testing.T) {
	want := `config: env: required environment variable "DFM_HTTP_ADDR" is not set`

	err := run(t.Context(), nil, io.Discard)

	if err == nil || err.Error() != want {
		t.Fatalf("run() error = %v, want %q", err, want)
	}
}

func TestRunServesHealthzUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	logs, logw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := run(ctx, []string{"DFM_HTTP_ADDR=127.0.0.1:0"}, logw)
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
	if resp.StatusCode != http.StatusOK || string(body) != `{"status":"ok"}` {
		t.Errorf("GET /healthz = %d %s, want 200 {\"status\":\"ok\"}", resp.StatusCode, body)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("run() after cancel error = %v, want nil", err)
	}
}

// listenAddr reads JSON log lines until the server reports its address.
func listenAddr(t *testing.T, logs io.Reader) string {
	t.Helper()
	dec := json.NewDecoder(logs)
	for {
		var line struct {
			Msg  string `json:"msg"`
			Addr string `json:"addr"`
		}
		if err := dec.Decode(&line); err != nil {
			t.Fatalf("server never logged its address: %v", err)
		}
		if line.Msg == "http server listening" {
			return line.Addr
		}
	}
}
