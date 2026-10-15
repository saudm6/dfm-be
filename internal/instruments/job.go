package instruments

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/saudm6/dfm-be/internal/calendar"
)

// retryLoad is how long Job waits after failing to load the calendar.
const retryLoad = time.Minute

// CalendarLoader loads the trading calendar; calendar.Loader is one.
type CalendarLoader interface {
	Load(ctx context.Context) (*calendar.Calendar, error)
}

// Job runs a sync on each trading day, Lead before continuous trading opens.
type Job struct {
	// Sync runs one sync. Its logger carries the run's run_id.
	Sync     func(ctx context.Context, logger *slog.Logger) error
	Calendar CalendarLoader
	Lead     time.Duration
	Logger   *slog.Logger
	// Now is the time source; nil means time.Now.
	Now func() time.Time
}

// NextRun returns when the sync after now runs: Lead before the first open
// that is more than Lead after now.
func NextRun(cal *calendar.Calendar, now time.Time, lead time.Duration) time.Time {
	return cal.NextOpen(now.Add(lead)).Add(-lead)
}

// Run syncs before every trading session until ctx is cancelled, then
// returns nil. A failed sync is logged and retried at the next session. The
// calendar is reloaded before each run, so a closure added since the run was
// scheduled cancels it.
func (j Job) Run(ctx context.Context) error {
	now := j.Now
	if now == nil {
		now = time.Now
	}
	for {
		cal, err := j.Calendar.Load(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			j.Logger.ErrorContext(ctx, "instrument sync: load calendar", "error", err)
			if !wait(ctx, retryLoad) {
				return nil
			}
			continue
		}
		at := NextRun(cal, now(), j.Lead)
		j.Logger.InfoContext(ctx, "instrument sync scheduled", "at", at)
		if !wait(ctx, time.Until(at)) {
			return nil
		}
		if cal, err = j.Calendar.Load(ctx); err == nil && !cal.IsTradingDay(at.Add(j.Lead)) {
			j.Logger.InfoContext(ctx, "instrument sync cancelled: market closed", "at", at)
			continue
		}
		j.runOnce(ctx)
	}
}

func (j Job) runOnce(ctx context.Context) {
	logger := j.Logger.With("run_id", uuid.NewString())
	logger.InfoContext(ctx, "instrument sync started")
	if err := j.Sync(ctx, logger); err != nil && ctx.Err() == nil {
		logger.ErrorContext(ctx, "instrument sync failed", "error", err)
	}
}

// wait sleeps for d and reports false if ctx was cancelled first.
func wait(ctx context.Context, d time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
