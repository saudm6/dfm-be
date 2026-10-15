package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

// server implements every operation in api/openapi.yaml. Each feature puts
// its handler methods in its own file in this package.
type server struct{}

var _ gen.StrictServerInterface = server{}

// writeError sends the contract's Error body.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gen.Error{Code: code, Message: message})
}

// badRequest answers requests whose parameters or body do not decode.
func badRequest(w http.ResponseWriter, _ *http.Request, err error) {
	writeError(w, http.StatusBadRequest, "bad_request", err.Error())
}

// internalError answers when a handler fails. The cause is logged, not sent.
func internalError(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		logger.LogAttrs(r.Context(), slog.LevelError, "http handler failed",
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
	}
}
