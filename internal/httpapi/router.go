// Package httpapi serves the HTTP API of the service.
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

// NewRouter returns the handler for every HTTP route of the service.
// Each request gets a request ID and is logged to logger when it completes.
func NewRouter(logger *slog.Logger) http.Handler {
	return newRouter(logger, server{})
}

// newRouter serves the operations of api/openapi.yaml with ssi.
func newRouter(logger *slog.Logger, ssi gen.StrictServerInterface) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(logRequests(logger))
	r.Use(middleware.Recoverer)

	h := gen.NewStrictHandlerWithOptions(ssi, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  badRequest,
		ResponseErrorHandlerFunc: internalError(logger),
	})
	gen.HandlerWithOptions(h, gen.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: badRequest,
	})
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
