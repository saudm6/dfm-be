package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

// Pinger reports whether the database answers. *pgxpool.Pool implements it.
type Pinger interface {
	Ping(ctx context.Context) error
}

const dbPingTimeout = 2 * time.Second

func (s server) GetHealth(ctx context.Context, _ gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	pingCtx, cancel := context.WithTimeout(ctx, dbPingTimeout)
	defer cancel()
	if err := s.db.Ping(pingCtx); err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "health check: database ping failed", slog.String("error", err.Error()))
		return gen.GetHealth503JSONResponse{Status: gen.HealthStatusUnavailable, DB: gen.HealthDBUnavailable}, nil
	}
	return gen.GetHealth200JSONResponse{Status: gen.HealthStatusOK, DB: gen.HealthDBOK}, nil
}
