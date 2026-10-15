package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

// CalendarLoader returns the current DFM trading calendar.
// calendar.Loader implements it.
type CalendarLoader interface {
	Load(ctx context.Context) (*calendar.Calendar, error)
}

const calendarLoadTimeout = 2 * time.Second

// GetMarketStatus loads the calendar on every request, so a closure added
// with `dfm calendar close` shows without a restart.
func (s server) GetMarketStatus(ctx context.Context, _ gen.GetMarketStatusRequestObject) (gen.GetMarketStatusResponseObject, error) {
	loadCtx, cancel := context.WithTimeout(ctx, calendarLoadTimeout)
	defer cancel()
	cal, err := s.cals.Load(loadCtx)
	if err != nil {
		// A client that went away is not a server fault.
		if ctx.Err() == nil {
			s.logger.LogAttrs(ctx, slog.LevelError, "market status: load calendar failed", slog.String("error", err.Error()))
		}
		return gen.GetMarketStatus503JSONResponse{Code: "unavailable", Message: "trading calendar unavailable"}, nil
	}
	t := s.now()
	return gen.GetMarketStatus200JSONResponse{
		Session:  cal.Session(t),
		NextOpen: cal.NextOpen(t).In(domain.DubaiTZ),
	}, nil
}
