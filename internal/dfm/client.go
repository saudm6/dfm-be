// Package dfm reads DFM's public web API at api2.dfm.ae. The endpoints are
// undocumented: every response shape here comes from responses recorded in
// testdata/dfm, and the contract tests parse those recordings so that a
// format change fails a test before it fails at runtime.
package dfm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the origin of DFM's web API.
const DefaultBaseURL = "https://api2.dfm.ae"

// UserAgent identifies this service to DFM on every request.
const UserAgent = "dfm-be/1.0 (+https://github.com/saudm6/dfm-be)"

// DefaultOverviewInterval spaces security overview calls to 2 per second.
const DefaultOverviewInterval = 500 * time.Millisecond

const (
	attempts          = 3
	defaultRetryDelay = time.Second
	maxBody           = 32 << 20
)

// Client calls DFM's web API. Set its fields before first use and share it by
// pointer.
type Client struct {
	// BaseURL is the API origin; empty means DefaultBaseURL.
	BaseURL string
	// HTTP sends the requests; nil means a client with a 30 s timeout.
	HTTP *http.Client
	// OverviewInterval is the least time between two security overview
	// calls; zero means DefaultOverviewInterval.
	OverviewInterval time.Duration
	// RetryDelay is the wait before retrying a request that failed in
	// transport or with a 5xx status; zero means 1 s.
	RetryDelay time.Duration

	overviews throttle
}

var defaultHTTP = &http.Client{Timeout: 30 * time.Second}

// statusError is a response status the caller did not expect.
type statusError struct {
	url    string
	status int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("dfm: %s: unexpected status %d", e.url, e.status)
}

// get sends GET path?query and returns the status and body of the response.
// pace, when not nil, runs before every attempt, so that retries keep to the
// same rate as first attempts.
func (c *Client) get(ctx context.Context, path string, query url.Values, pace func(context.Context) error) (int, []byte, error) {
	u := c.url(path)
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return c.send(ctx, pace, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	})
}

// postForm sends form to path as application/x-www-form-urlencoded.
func (c *Client) postForm(ctx context.Context, path string, form url.Values) (int, []byte, error) {
	u := c.url(path)
	body := form.Encode()
	return c.send(ctx, nil, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		return req, err
	})
}

// getJSON decodes the body of a 200 response to GET path?query into dst.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, dst any) error {
	status, body, err := c.get(ctx, path, query, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return &statusError{url: path, status: status}
	}
	return decode(path, body, dst)
}

func decode(what string, body []byte, dst any) error {
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("dfm: decode %s: %w", what, err)
	}
	return nil
}

func (c *Client) url(path string) string {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	return strings.TrimRight(base, "/") + path
}

// send performs the request newReq builds, retrying transport errors and 5xx
// responses. Other statuses are returned to the caller. pace, when not nil,
// runs before every attempt.
func (c *Client) send(ctx context.Context, pace func(context.Context) error, newReq func() (*http.Request, error)) (int, []byte, error) {
	hc := c.HTTP
	if hc == nil {
		hc = defaultHTTP
	}
	delay := c.RetryDelay
	if delay == 0 {
		delay = defaultRetryDelay
	}
	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			if err := sleep(ctx, delay); err != nil {
				return 0, nil, err
			}
		}
		if pace != nil {
			if err := pace(ctx); err != nil {
				return 0, nil, err
			}
		}
		req, err := newReq()
		if err != nil {
			return 0, nil, fmt.Errorf("dfm: %w", err)
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept", "application/json")
		status, body, err := roundTrip(hc, req)
		switch {
		case err != nil:
			lastErr = fmt.Errorf("dfm: %s %s: %w", req.Method, req.URL.Path, err)
			if ctx.Err() != nil {
				return 0, nil, lastErr
			}
		case status >= http.StatusInternalServerError:
			lastErr = &statusError{url: req.URL.Path, status: status}
		default:
			return status, body, nil
		}
	}
	return 0, nil, lastErr
}

func roundTrip(hc *http.Client, req *http.Request) (int, []byte, error) {
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// throttle hands out start times at least every apart.
type throttle struct {
	mu   sync.Mutex
	next time.Time
}

// wait blocks until the caller may start its call.
func (t *throttle) wait(ctx context.Context, every time.Duration) error {
	t.mu.Lock()
	now := time.Now()
	at := t.next
	if at.Before(now) {
		at = now
	}
	t.next = at.Add(every)
	t.mu.Unlock()
	return sleep(ctx, time.Until(at))
}
