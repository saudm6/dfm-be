// Package httpapi serves the HTTP API of the service.
package httpapi

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/saudm6/dfm-be/api"
	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/httpapi/gen"
	"github.com/saudm6/dfm-be/internal/ws"
)

// NewRouter returns the handler for every HTTP route of the service.
// Each request gets a request ID and is logged to logger when it completes.
// db is the service database; /healthz reports whether it answers.
// /market/status reports the DFM session at now from the calendar cals loads.
// sessions signs users in and out; every operation of api/openapi.yaml except
// those in api.PublicOperations needs one of its sessions.
// GET /ws upgrades a signed-in, same-origin request to a WebSocket served by
// gateway; with a nil gateway there is no /ws.
func NewRouter(logger *slog.Logger, db Pinger, cals CalendarLoader, now func() time.Time, sessions *auth.Service, gateway *ws.Gateway) http.Handler {
	r := newRouter(logger, server{logger: logger, db: db, cals: cals, now: now, sessions: sessions}, sessions)
	if gateway != nil {
		r.Get("/ws", serveWebSocket(logger, sessions, gateway))
	}
	return r
}

// newRouter serves the operations of api/openapi.yaml with ssi.
func newRouter(logger *slog.Logger, ssi gen.StrictServerInterface, sessions *auth.Service) *chi.Mux {
	r := newBaseRouter(logger)
	h := gen.NewStrictHandlerWithOptions(ssi, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  bodyDecodeError,
		ResponseErrorHandlerFunc: handlerError(logger),
	})
	gen.HandlerWithOptions(loginGuard{h}, gen.ChiServerOptions{
		BaseRouter:       r,
		Middlewares:      []gen.MiddlewareFunc{requireAuth(logger, sessions, gen.SessionCookieScopes, apiPublicRoutes)},
		ErrorHandlerFunc: paramBindError,
	})
	return r
}

// apiPublicRoutes are the routes of api.PublicOperations.
var apiPublicRoutes = mustPublicRoutes(api.OpenAPI, api.PublicOperations)

// newBaseRouter returns a router with the middleware and the JSON 404 and 405
// answers, ready for the generated routes.
func newBaseRouter(logger *slog.Logger) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(logRequests(logger))
	r.Use(recoverPanics(logger))
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no route for "+req.URL.Path)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		// Route on the same path chi does: the escaped form when there is one.
		path := req.URL.RawPath
		if path == "" {
			path = req.URL.Path
		}
		var allowed []string
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
			if r.Match(chi.NewRouteContext(), m, path) {
				allowed = append(allowed, m)
			}
		}
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", req.Method+" is not allowed on "+req.URL.Path)
	})
	return r
}

// recoverPanics answers a panicking handler with 500 and the Error body, and
// logs the panic with its stack. When the handler has already sent a status
// it aborts the connection instead, so the client sees a broken response
// rather than JSON appended to it; when the handler has hijacked the
// connection it closes it.
func recoverPanics(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw, ok := w.(*responseWriter)
			if !ok {
				rw = &responseWriter{ResponseWriter: w}
			}
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(rec)
				}
				logger.LogAttrs(r.Context(), slog.LevelError, "http handler panicked",
					slog.String("path", r.URL.Path),
					slog.String("panic", fmt.Sprint(rec)),
					slog.String("stack", string(debug.Stack())),
					slog.String("request_id", middleware.GetReqID(r.Context())),
				)
				switch {
				case rw.hijacked != nil:
					_ = rw.hijacked.Close()
				case rw.status != 0:
					panic(http.ErrAbortHandler)
				default:
					writeError(rw, http.StatusInternalServerError, "internal", "internal server error")
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

func logRequests(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w}
			defer func() {
				status := rw.status
				if status == 0 && rw.hijacked == nil {
					// The handler wrote nothing, so net/http sends 200.
					status = http.StatusOK
				}
				attrs := []slog.Attr{
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", status),
					slog.Int("bytes", rw.bytes),
					slog.Duration("duration", time.Since(start)),
					slog.String("request_id", middleware.GetReqID(r.Context())),
				}
				if rw.hijacked != nil {
					attrs = append(attrs, slog.Bool("hijacked", true))
				}
				logger.LogAttrs(r.Context(), slog.LevelInfo, "http request", attrs...)
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// responseWriter records what a handler did with the response: the status
// it sent, the body bytes it wrote, and the connection if it hijacked one,
// as a WebSocket upgrade does after sending 101.
type responseWriter struct {
	http.ResponseWriter
	status   int
	bytes    int
	hijacked net.Conn
}

func (w *responseWriter) WriteHeader(code int) {
	// Informational responses other than 101 precede the real status.
	if w.status == 0 && (code >= 200 || code == http.StatusSwitchingProtocols) {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the connection's writer.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, brw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = c
	}
	return c, brw, err
}
