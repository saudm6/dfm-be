package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// jobFunc is a job that runs f.
type jobFunc func(ctx context.Context) error

func (f jobFunc) Run(ctx context.Context) error { return f(ctx) }

// failingListener fails every Accept.
type failingListener struct{ net.Listener }

func (failingListener) Accept() (net.Conn, error) { return nil, errors.New("accept failed") }

func TestRunServer(t *testing.T) {
	tests := []struct {
		name       string
		failAccept bool
		failJob    bool
		cancel     bool
		wantErr    string
	}{
		{name: "job error stops the server and is returned", failJob: true, wantErr: "job failed"},
		{name: "serve error stops the jobs", failAccept: true, wantErr: "serve http: accept failed"},
		{name: "cancel stops everything and returns nil", cancel: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var lc net.ListenConfig
			ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			base := "http://" + ln.Addr().String()
			if tt.failAccept {
				ln = failingListener{ln}
			}
			srv := &http.Server{
				Handler:           http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
				ReadHeaderTimeout: time.Second,
			}
			stopped := make(chan struct{})
			jobs := []job{jobFunc(func(ctx context.Context) error {
				<-ctx.Done()
				close(stopped)
				return nil
			})}
			if tt.failJob {
				jobs = append(jobs, jobFunc(func(ctx context.Context) error {
					if status := statusOf(ctx, t, base); status != http.StatusNoContent {
						t.Errorf("GET / before the job failed = %d, want 204", status)
					}
					return errors.New("job failed")
				}))
			}
			done := make(chan error, 1)
			go func() { done <- runServer(ctx, slog.New(slog.DiscardHandler), srv, ln, jobs) }()
			if tt.cancel {
				if status := statusOf(ctx, t, base); status != http.StatusNoContent {
					t.Errorf("GET / before cancel = %d, want 204", status)
				}
				cancel()
			}

			var got error
			select {
			case got = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("runServer did not return")
			}

			if gotErr := errText(got); gotErr != tt.wantErr {
				t.Errorf("runServer() error = %q, want %q", gotErr, tt.wantErr)
			}
			select {
			case <-stopped:
			default:
				t.Error("the job still runs after runServer returned")
			}
			if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("Serve() after runServer returned = %v, want http.ErrServerClosed", err)
			}
		})
	}
}

// statusOf returns the status of GET url, or 0 when the request fails.
func statusOf(ctx context.Context, t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("GET %s: %v", url, err)
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
