// Package dfmtest serves recorded DFM web API responses for tests, so that no
// test calls the live endpoints.
package dfmtest

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// SecurityMasterDir returns the directory of the security master responses
// recorded from api2.dfm.ae on 2026-10-15.
func SecurityMasterDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "dfm", "security-master-2026-10-15")
}

// MarketWatchDir returns the directory of the market-watch responses
// (mw/v1/stocks, mw/v1/status, mw/v1/indices) recorded from api2.dfm.ae at
// 23:57 on 2026-10-15 in Dubai, after that day's close.
func MarketWatchDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "dfm", "market-watch-2026-10-15")
}

// Request is a request the server received.
type Request struct {
	Method    string
	Path      string
	Query     url.Values
	Form      url.Values
	UserAgent string
	At        time.Time
}

// Server answers DFM web API requests from the files in a directory:
//
//	POST /web/widgets/v1/data Command=LiteSecuritiesLists  lite-securities-lists-<Language>.json
//	GET  /mw/v1/stocks                                     mw-stocks.json
//	GET  /mw/v1/status                                     mw-status.json
//	GET  /mw/v1/indices                                    mw-indices.json
//	GET  /web/sf/v1/sectors?sf_culture=<c>                 sectors-<c>.json
//	GET  /cdn2/web/v1/get_security_overview?symbol=<s>     overview/<s>.json, or overview/<s>.<status>.json
type Server struct {
	*httptest.Server
	t   testing.TB
	dir string

	mu       sync.Mutex
	requests []Request
	failures map[string][]int
	files    map[string]string
}

// NewServer starts a server on dir, closed when t ends.
func NewServer(t testing.TB, dir string) *Server {
	t.Helper()
	s := &Server{t: t, dir: dir, failures: map[string][]int{}, files: map[string]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// FailNext makes the next requests to path answer with statuses, in order.
func (s *Server) FailNext(path string, statuses ...int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[path] = append(s.failures[path], statuses...)
}

// Replace serves file name relative to the directory from the file at path
// instead.
func (s *Server) Replace(name, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[name] = path
}

// Requests returns the requests received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, Request{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Form: r.PostForm,
		UserAgent: r.UserAgent(), At: time.Now(),
	})
	var fail int
	if f := s.failures[r.URL.Path]; len(f) > 0 {
		fail, s.failures[r.URL.Path] = f[0], f[1:]
	}
	s.mu.Unlock()
	if fail != 0 {
		w.WriteHeader(fail)
		return
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/web/widgets/v1/data":
		if r.PostForm.Get("Command") != "LiteSecuritiesLists" {
			// DFM answers an unknown command with an empty 200.
			return
		}
		s.file(w, "lite-securities-lists-"+r.PostForm.Get("Language")+".json", "text/plain; charset=utf-8")
	case r.Method == http.MethodGet && r.URL.Path == "/mw/v1/stocks":
		s.file(w, "mw-stocks.json", "application/json; charset=utf-8")
	case r.Method == http.MethodGet && r.URL.Path == "/mw/v1/status":
		s.file(w, "mw-status.json", "application/json; charset=utf-8")
	case r.Method == http.MethodGet && r.URL.Path == "/mw/v1/indices":
		s.file(w, "mw-indices.json", "application/json; charset=utf-8")
	case r.Method == http.MethodGet && r.URL.Path == "/web/sf/v1/sectors":
		s.file(w, "sectors-"+r.URL.Query().Get("sf_culture")+".json", "application/json; charset=utf-8")
	case r.Method == http.MethodGet && r.URL.Path == "/cdn2/web/v1/get_security_overview":
		s.overview(w, r.URL.Query().Get("symbol"))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"statusCode":404,"message":"Resource not found"}`))
	}
}

func (s *Server) overview(w http.ResponseWriter, symbol string) {
	name := filepath.Join("overview", symbol+".json")
	if _, err := os.Stat(s.path(name)); err == nil {
		s.file(w, name, "application/json; charset=utf-8")
		return
	}
	matches, _ := filepath.Glob(s.path(filepath.Join("overview", symbol+".*.json")))
	if len(matches) != 1 {
		s.t.Errorf("dfmtest: no recorded overview for %q", symbol)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	status, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(matches[0]), symbol+"."), ".json"))
	if err != nil {
		s.t.Errorf("dfmtest: overview file %s: %v", matches[0], err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	body, err := os.ReadFile(matches[0])
	if err != nil {
		s.t.Errorf("dfmtest: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *Server) path(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.files[filepath.ToSlash(name)]; ok {
		return p
	}
	return filepath.Join(s.dir, name)
}

func (s *Server) file(w http.ResponseWriter, name, contentType string) {
	body, err := os.ReadFile(s.path(name))
	if errors.Is(err, fs.ErrNotExist) {
		s.t.Errorf("dfmtest: no recorded response %s", name)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err != nil {
		s.t.Errorf("dfmtest: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(body)
}
