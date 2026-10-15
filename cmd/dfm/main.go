// Command dfm runs the DFM trader terminal backend.
//
// With no arguments it migrates the database and serves the HTTP API.
// "dfm migrate" applies pending database migrations and exits.
// "dfm calendar close --date YYYY-MM-DD" records an unscheduled closure of DFM.
// "dfm sync instruments" refreshes the security master from DFM's web API.
//
// The server runs its background jobs next to the HTTP server, each on its
// own goroutine in one errgroup bound to the run context: cancelling the
// context, or any of them failing, stops all of them.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/config"
	"github.com/saudm6/dfm-be/internal/dfm"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/httpapi"
	"github.com/saudm6/dfm-be/internal/instruments"
	"github.com/saudm6/dfm-be/internal/store"
)

const (
	shutdownTimeout = 10 * time.Second
	usage           = `usage: dfm [migrate | calendar close --date YYYY-MM-DD [--reason TEXT] | sync instruments]`
	// instrumentSyncBeforePreOpen is how long before the pre-open session
	// the daily instrument sync starts.
	instrumentSyncBeforePreOpen = 30 * time.Minute
)

// clock is the time source of every command; tests replace it.
var clock = time.Now

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Environ(), os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dfm:", err)
		os.Exit(1)
	}
}

// run executes the command named by args. It writes JSON logs to stdout.
func run(ctx context.Context, args, environ []string, stdout io.Writer) error {
	switch {
	case len(args) == 0:
		return serve(ctx, environ, stdout)
	case len(args) == 1 && args[0] == "migrate":
		return migrate(ctx, environ, stdout)
	case len(args) >= 2 && args[0] == "calendar" && args[1] == "close":
		return closeMarket(ctx, args[2:], environ, stdout)
	case len(args) == 2 && args[0] == "sync" && args[1] == "instruments":
		return syncInstruments(ctx, environ, stdout)
	default:
		return errors.New(usage)
	}
}

// closeMarket records that DFM is closed on the day given by --date.
func closeMarket(ctx context.Context, args, environ []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("dfm calendar close", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dateArg := fs.String("date", "", "the closed day in Dubai, YYYY-MM-DD")
	reason := fs.String("reason", "Unscheduled closure", "why DFM is closed")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 || *dateArg == "" || *reason == "" {
		return errors.New(usage)
	}
	day, err := parseClosureDate(*dateArg, clock())
	if err != nil {
		return err
	}

	cfg, err := config.LoadBase(environ)
	if err != nil {
		return err
	}
	logger := newLogger(stdout, cfg.LogLevel)
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	added, err := calendar.AddClosure(ctx, pool, day, *reason)
	if err != nil {
		return err
	}
	msg := "market closure recorded"
	if !added {
		msg = "market already closed"
	}
	logger.Info(msg, "date", *dateArg)
	return nil
}

// firstCalendarDay is the first day the seeded holidays table covers.
var firstCalendarDay = domain.TradingDayStart(2025, time.January, 1)

// parseClosureDate parses the --date of `dfm calendar close`: a Dubai date
// from firstCalendarDay to two years after the Dubai date of now. The range
// catches typos such as a wrong year.
func parseClosureDate(s string, now time.Time) (time.Time, error) {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("calendar close: --date %q is not YYYY-MM-DD", s)
	}
	day := domain.TradingDayStart(d.Year(), d.Month(), d.Day())
	y, m, dd := now.In(domain.DubaiTZ).Date()
	last := domain.TradingDayStart(y+2, m, dd)
	switch {
	case day.Before(firstCalendarDay):
		return time.Time{}, fmt.Errorf("calendar close: --date %s is before %s, the first day of the calendar",
			s, firstCalendarDay.Format(time.DateOnly))
	case day.After(last):
		return time.Time{}, fmt.Errorf("calendar close: --date %s is more than 2 years ahead; the last day accepted is %s",
			s, last.Format(time.DateOnly))
	}
	return day, nil
}

// syncInstruments refreshes the security master once.
func syncInstruments(ctx context.Context, environ []string, stdout io.Writer) error {
	cfg, err := config.LoadSync(environ)
	if err != nil {
		return err
	}
	logger := newLogger(stdout, cfg.LogLevel).With("run_id", uuid.NewString())
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := instrumentSync(pool, &dfm.Client{BaseURL: cfg.DFM.APIBaseURL})(ctx, logger); err != nil {
		logger.ErrorContext(ctx, "instrument sync failed", "error", err)
		return err
	}
	return nil
}

// instrumentSync returns a sync of the security master from src into pool
// that logs its counts.
func instrumentSync(pool *pgxpool.Pool, src instruments.Source) func(context.Context, *slog.Logger) error {
	return func(ctx context.Context, logger *slog.Logger) error {
		res, err := instruments.Sync(ctx, pool, src, clock(), logger)
		if err != nil {
			return err
		}
		logger.InfoContext(ctx, "instrument sync finished",
			"instruments", res.Instruments, "written", res.Written, "delisted", res.Delisted,
			"boards", res.Boards, "boards_written", res.BoardsWritten, "skipped", res.Skipped)
		return nil
	}
}

// migrate applies pending migrations and exits.
func migrate(ctx context.Context, environ []string, stdout io.Writer) error {
	cfg, err := config.LoadBase(environ)
	if err != nil {
		return err
	}
	logger := newLogger(stdout, cfg.LogLevel)
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return applyMigrations(ctx, logger, pool)
}

// serve migrates the database, then serves the HTTP API and runs the
// background jobs until ctx is cancelled, and shuts down gracefully.
func serve(ctx context.Context, environ []string, stdout io.Writer) error {
	cfg, err := config.Load(environ)
	if err != nil {
		return err
	}
	logger := newLogger(stdout, cfg.LogLevel)

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := applyMigrations(ctx, logger, pool); err != nil {
		return err
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}
	hours := cfg.Market.Hours()
	cals := calendar.Loader{DB: pool, Hours: hours}
	srv := &http.Server{
		Handler:           httpapi.NewRouter(logger, pool, cals, clock),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	jobs := []job{
		instruments.Job{
			Sync:     instrumentSync(pool, &dfm.Client{BaseURL: cfg.DFM.APIBaseURL}),
			Calendar: cals,
			Lead:     time.Duration(hours.Open-hours.PreOpen)*time.Minute + instrumentSyncBeforePreOpen,
			Logger:   logger,
			Now:      func() time.Time { return clock() },
		},
	}
	logger.Info("http server listening", "addr", ln.Addr().String())
	return runServer(ctx, logger, srv, ln, jobs)
}

// job is a background task of the server. Run returns nil once ctx is
// cancelled, and an error only when the task cannot go on; it logs and
// retries failures it can recover from.
type job interface {
	Run(ctx context.Context) error
}

// runServer serves srv on ln and runs jobs, each on its own goroutine, until
// ctx is cancelled or one of them fails. Then the jobs' context is cancelled
// and srv shuts down gracefully. It returns the first failure.
func runServer(ctx context.Context, logger *slog.Logger, srv *http.Server, ln net.Listener, jobs []job) error {
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve http: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down http server: %w", err)
		}
		return nil
	})
	for _, j := range jobs {
		g.Go(func() error { return j.Run(gctx) })
	}
	return g.Wait()
}

func newLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}

func applyMigrations(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool) error {
	applied, err := store.Migrate(ctx, pool)
	if err != nil {
		return err
	}
	logger.Info("database migrated", "applied", applied)
	return nil
}
