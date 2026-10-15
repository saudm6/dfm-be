// Package httpapi serves the HTTP API of the service.
package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

// NewRouter returns the handler for every HTTP route of the service.
// Each request gets a request ID and is logged to logger when it completes.
// db is the service database; /healthz reports whether it answers.
// /market/status reports the DFM session at now from the calendar cals loads.
func NewRouter(logger *slog.Logger, db Pinger, cals CalendarLoader, now func() time.Time) http.Handler {
	return newRouter(logger, server{logger: logger, db: db, cals: cals, now: now})
}

// newRouter serves the operations of api/openapi.yaml with ssi.
func newRouter(logger *slog.Logger, ssi gen.StrictServerInterface) http.Handler {
	r := newBaseRouter(logger)
	h := gen.NewStrictHandlerWithOptions(ssi, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  bodyDecodeError,
		ResponseErrorHandlerFunc: handlerError(logger),
	})
	gen.HandlerWithOptions(h, gen.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: paramBindError,
	})
	return r
}

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
		var allowed []string
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
			if r.Match(chi.NewRouteContext(), m, req.URL.Path) {
				allowed = append(allowed, m)
			}
		}
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", req.Method+" is not allowed on "+req.URL.Path)
	})
	return r
}

// recoverPanics answers a panicking handler with 500 and the Error body, and
// logs the panic with its stack.
func recoverPanics(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				// A WebSocket handler has hijacked the connection; there is no response to write.
				if r.Header.Get("Connection") == "Upgrade" {
					return
				}
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func logRequests(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				status := ww.Status()
				if status == 0 {
					// The handler wrote nothing, so net/http sends 200.
					status = http.StatusOK
				}
				logger.LogAttrs(r.Context(), slog.LevelInfo, "http request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", status),
					slog.Int("bytes", ww.BytesWritten()),
					slog.Duration("duration", time.Since(start)),
					slog.String("request_id", middleware.GetReqID(r.Context())),
				)
			}()
			next.ServeHTTP(ww, r)
		})
	}
}
