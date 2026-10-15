// Package httpapi serves the HTTP API of the service.
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter returns the handler for every HTTP route of the service.
// Each request gets a request ID and is logged to logger when it completes.
// db is the service database; /healthz reports whether it answers.
// /market/status reports the DFM session at now from the calendar cals loads.
func NewRouter(logger *slog.Logger, db Pinger, cals CalendarLoader, now func() time.Time) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(logRequests(logger))
	r.Use(middleware.Recoverer)

	r.Get("/healthz", healthz(logger, db))
	r.Get("/market/status", getMarketStatus(logger, cals, now))
	return r
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
