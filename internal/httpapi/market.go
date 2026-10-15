package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/domain"
)

// CalendarLoader returns the current DFM trading calendar.
// calendar.Loader implements it.
type CalendarLoader interface {
	Load(ctx context.Context) (*calendar.Calendar, error)
}

type marketStatus struct {
	Session  calendar.Session `json:"session"`
	NextOpen time.Time        `json:"next_open"`
}

func getMarketStatus(logger *slog.Logger, cals CalendarLoader, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		cal, err := cals.Load(r.Context())
		if err != nil {
			logger.LogAttrs(r.Context(), slog.LevelError, "market status: load calendar failed", slog.String("error", err.Error()))
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"calendar unavailable"}`))
			return
		}
		t := now()
		body, err := json.Marshal(marketStatus{
			Session:  cal.Session(t),
			NextOpen: cal.NextOpen(t).In(domain.DubaiTZ),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	}
}
