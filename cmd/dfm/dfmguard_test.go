package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/saudm6/dfm-be/internal/dfm"
)

// unreachableDFM is a DFM API base URL nothing listens on.
const unreachableDFM = "http://127.0.0.1:1"

// liveClients counts the DFM clients tests built for the live API.
var liveClients atomic.Int64

// TestMain fails the run when a test builds a DFM client for the live API,
// and points that client at unreachableDFM, so no test reaches api2.dfm.ae.
// A test that starts the server or a sync sets DFM_API_BASE_URL.
func TestMain(m *testing.M) {
	build := newDFMClient
	newDFMClient = func(baseURL string) *dfm.Client {
		if liveDFM(baseURL) {
			liveClients.Add(1)
			baseURL = unreachableDFM
		}
		return build(baseURL)
	}
	code := m.Run()
	if n := liveClients.Load(); n > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: %d DFM clients were built for the live API %s; set DFM_API_BASE_URL in the test\n", n, dfm.DefaultBaseURL)
		code = 1
	}
	os.Exit(code)
}

// liveDFM tells whether a client at baseURL would call DFM's live API.
func liveDFM(baseURL string) bool {
	if baseURL == "" {
		return true
	}
	u, err := url.Parse(baseURL)
	return err != nil || strings.EqualFold(u.Hostname(), "api2.dfm.ae")
}

func TestLiveDFMGuard(t *testing.T) {
	tests := []struct {
		baseURL string
		want    bool
	}{
		{"", true},
		{dfm.DefaultBaseURL, true},
		{"https://API2.dfm.ae/", true},
		{"http://api2.dfm.ae:443", true},
		{unreachableDFM, false},
		{"http://127.0.0.1:52345", false},
	}
	for _, tt := range tests {
		if got := liveDFM(tt.baseURL); got != tt.want {
			t.Errorf("liveDFM(%q) = %t, want %t", tt.baseURL, got, tt.want)
		}
	}
}

func TestGuardRedirectsLiveClients(t *testing.T) {
	if got := newDFMClient(dfm.DefaultBaseURL).BaseURL; got != unreachableDFM {
		t.Errorf("client for the live API has BaseURL %q under test, want %q", got, unreachableDFM)
	}
	liveClients.Add(-1)
}
