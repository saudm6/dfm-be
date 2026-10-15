package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Pinger reports whether the database answers. *pgxpool.Pool implements it.
type Pinger interface {
	Ping(ctx context.Context) error
}

const dbPingTimeout = 2 * time.Second

func healthz(logger *slog.Logger, db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), dbPingTimeout)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := db.Ping(ctx); err != nil {
			logger.LogAttrs(r.Context(), slog.LevelError, "health check: database ping failed", slog.String("error", err.Error()))
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable","db":"unavailable"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","db":"ok"}`))
	}
}
