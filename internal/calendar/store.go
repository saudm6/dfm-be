package calendar

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/store/dbgen"
)

// Loader builds the calendar from Hours and the holidays table.
type Loader struct {
	DB    dbgen.DBTX
	Hours Hours
}

// Load reads every closed day from the holidays table and returns the
// calendar as of now. Callers reload to see closures added since.
func (l Loader) Load(ctx context.Context) (*Calendar, error) {
	days, err := dbgen.New(l.DB).ListHolidays(ctx)
	if err != nil {
		return nil, fmt.Errorf("calendar: list holidays: %w", err)
	}
	closed := make([]time.Time, len(days))
	for i, d := range days {
		y, m, dd := d.Time.Date()
		closed[i] = domain.TradingDayStart(y, m, dd)
	}
	return New(l.Hours, closed)
}

// AddClosure records that DFM is closed on the Dubai date of day for
// reason, such as an unscheduled closure. It reports false when the day was
// already closed, and leaves the existing record unchanged.
func AddClosure(ctx context.Context, db dbgen.DBTX, day time.Time, reason string) (bool, error) {
	n, err := dbgen.New(db).AddClosure(ctx, dbgen.AddClosureParams{
		Day:    pgtype.Date{Time: dateOf(day).start(), Valid: true},
		Reason: reason,
	})
	if err != nil {
		return false, fmt.Errorf("calendar: add closure: %w", err)
	}
	return n == 1, nil
}
