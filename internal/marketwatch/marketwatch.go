// Package marketwatch keeps a snapshot of every DFM instrument from DFM's
// market watch: the latest delayed quote per instrument, the market's
// breadth, and the DFM General Index minute by minute. Movers, the heat map,
// and fallback quotes read it.
//
// Poller polls while the market trades and once after each close. It keeps
// the snapshot in memory, stores each quote in the market_watch_quotes table
// when DFM's modification time changes, and publishes a bus.QuoteUpdated
// event per changed quote.
package marketwatch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/saudm6/dfm-be/internal/bus"
	"github.com/saudm6/dfm-be/internal/calendar"
	"github.com/saudm6/dfm-be/internal/dfm"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/store/dbgen"
)

// Defaults of Options.
const (
	DefaultInterval   = 30 * time.Second
	DefaultMaxBackoff = 5 * time.Minute
	// DefaultCloseDelay waits for DFM's post-close updates: in the recording
	// of 2026-10-15 the last line changed at 15:25:00.993, 25 minutes after
	// the 15:00 close.
	DefaultCloseDelay = 30 * time.Minute
)

// retryLoad is how long Run waits after failing to load the calendar.
const retryLoad = time.Minute

// Source reads DFM's market watch; *dfm.Client is one.
type Source interface {
	MarketWatch(ctx context.Context) (dfm.MarketWatch, error)
	MarketStatus(ctx context.Context) (dfm.MarketStatus, error)
	IndexSeries(ctx context.Context) ([]dfm.IndexPoint, error)
}

// CalendarLoader loads the trading calendar; calendar.Loader is one.
type CalendarLoader interface {
	Load(ctx context.Context) (*calendar.Calendar, error)
}

// DB is the database the snapshot is stored in, such as *pgxpool.Pool.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Options configures a Poller.
type Options struct {
	Source   Source
	DB       DB
	Bus      *bus.Bus
	Calendar CalendarLoader
	// PreOpenLead is how long before continuous trading the pre-open
	// session starts; Run wakes that long before calendar.NextOpen.
	PreOpenLead time.Duration
	Logger      *slog.Logger
	// Interval is the time between polls while the market trades; zero
	// means DefaultInterval.
	Interval time.Duration
	// MaxBackoff caps the wait after failed polls; zero means
	// DefaultMaxBackoff.
	MaxBackoff time.Duration
	// CloseDelay is how long after the close the closing poll runs; zero
	// means DefaultCloseDelay.
	CloseDelay time.Duration
	// Now is the time source; nil means time.Now.
	Now func() time.Time
	// Wait sleeps for d and reports false if ctx ends first; nil means a
	// timer.
	Wait func(ctx context.Context, d time.Duration) bool
}

// Health is how the market-watch source has fared, for source health
// reporting.
type Health struct {
	// LastSuccess is when the last poll that fully succeeded ended.
	LastSuccess time.Time
	// LastFailure is when the last failed poll ended, and LastError its
	// error.
	LastFailure time.Time
	LastError   string
	// ConsecutiveFailures counts failed polls since the last success.
	ConsecutiveFailures int
}

// Result counts what one poll did.
type Result struct {
	// Lines is the number of market-watch lines that parsed.
	Lines int
	// Changed is the number of quotes whose modification time changed,
	// stored and published; Unchanged the number skipped as unchanged.
	Changed, Unchanged int
	// Unknown is the number of lines whose symbol is not an instrument of
	// the security master, such as debt lines and futures DFM's securities
	// lists leave out.
	Unknown int
	// Skipped is the number of lines that did not parse.
	Skipped int
}

// Poller polls DFM's market watch and keeps the snapshot. Create it with
// New; it is safe for concurrent use.
type Poller struct {
	opts Options

	mu     sync.RWMutex
	quotes map[domain.Symbol]domain.Quote
	status *dfm.MarketStatus
	series []dfm.IndexPoint
	health Health
}

// New returns a poller with an empty snapshot.
func New(opts Options) *Poller {
	opts.Interval = cmp.Or(opts.Interval, DefaultInterval)
	opts.MaxBackoff = cmp.Or(opts.MaxBackoff, DefaultMaxBackoff)
	opts.CloseDelay = cmp.Or(opts.CloseDelay, DefaultCloseDelay)
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Wait == nil {
		opts.Wait = wait
	}
	return &Poller{opts: opts, quotes: make(map[domain.Symbol]domain.Quote)}
}

// Quote returns the latest quote of sym, if the snapshot has one.
func (p *Poller) Quote(sym domain.Symbol) (domain.Quote, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	q, ok := p.quotes[sym]
	return q, ok
}

// Quotes returns the latest quote of every instrument, ordered by symbol.
func (p *Poller) Quotes() []domain.Quote {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]domain.Quote, 0, len(p.quotes))
	for _, sym := range slices.Sorted(maps.Keys(p.quotes)) {
		out = append(out, p.quotes[sym])
	}
	return out
}

// Status returns the last market status read, and false before the first.
func (p *Poller) Status() (dfm.MarketStatus, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.status == nil {
		return dfm.MarketStatus{}, false
	}
	return *p.status, true
}

// IndexSeries returns the last DFM General Index series read, oldest first.
func (p *Poller) IndexSeries() []dfm.IndexPoint {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return slices.Clone(p.series)
}

// Health returns how the polls have fared.
func (p *Poller) Health() Health {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

// Load fills the snapshot's quotes from the market_watch_quotes table, so
// a restart neither starts empty nor republishes unchanged quotes.
func (p *Poller) Load(ctx context.Context) error {
	rows, err := dbgen.New(p.opts.DB).ListMarketWatchQuotes(ctx)
	if err != nil {
		return fmt.Errorf("marketwatch: load snapshot: %w", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range rows {
		q := domain.Quote{
			Symbol: r.Symbol, Last: r.Last, Bid: r.Bid, Ask: r.Ask, BidSize: r.BidSize, AskSize: r.AskSize,
			Volume: r.Volume, Trades: r.Trades, Value: r.Value, PrevClose: r.PrevClose,
			AsOf: r.AsOf.In(domain.DubaiTZ), Source: domain.SourceDFMMarketWatch, Freshness: domain.Delayed,
		}
		if r.LastTradeAt.Valid {
			q.LastTradeAt = r.LastTradeAt.Time.In(domain.DubaiTZ)
		}
		p.quotes[q.Symbol] = q
	}
	return nil
}

// Poll reads the market watch, the market status, and the index series
// once. Quotes of instruments in the security master whose modification
// time changed are stored, put in the snapshot, and published; the DFM
// General Index quote comes from the market status. What was read is kept
// even when another read fails; the error joins every failure. Poll does
// not update Health; Run does.
func (p *Poller) Poll(ctx context.Context, logger *slog.Logger) (Result, error) {
	known, err := dbgen.New(p.opts.DB).ListInstrumentSymbols(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("marketwatch: list instruments: %w", err)
	}
	isKnown := make(map[domain.Symbol]bool, len(known))
	for _, s := range known {
		isKnown[s] = true
	}

	var errs []error
	var res Result
	var quotes []domain.Quote
	if mw, err := p.opts.Source.MarketWatch(ctx); err != nil {
		errs = append(errs, err)
	} else {
		quotes = mw.Quotes
		res.Lines, res.Skipped = len(mw.Quotes), len(mw.Skipped)
		for _, s := range mw.Skipped {
			logger.WarnContext(ctx, "market watch line skipped", "symbol", s.Symbol, "reason", s.Reason)
		}
	}
	status, statusErr := p.opts.Source.MarketStatus(ctx)
	if statusErr != nil {
		errs = append(errs, statusErr)
	} else if iq, err := status.IndexQuote(); err != nil {
		errs = append(errs, fmt.Errorf("marketwatch: index quote: %w", err))
	} else {
		quotes = append(quotes, iq)
	}
	series, seriesErr := p.opts.Source.IndexSeries(ctx)
	if seriesErr != nil {
		errs = append(errs, seriesErr)
	}

	changed := p.changed(quotes, isKnown, &res)
	if err := p.store(ctx, changed); err != nil {
		return res, errors.Join(append(errs, err)...)
	}
	res.Changed = len(changed)

	p.mu.Lock()
	for _, q := range changed {
		p.quotes[q.Symbol] = q
	}
	if statusErr == nil {
		p.status = &status
	}
	if seriesErr == nil {
		p.series = series
	}
	p.mu.Unlock()
	for _, q := range changed {
		bus.Publish(p.opts.Bus, bus.QuoteUpdated, bus.QuoteUpdatedEvent{Quote: q})
	}
	return res, errors.Join(errs...)
}

// changed returns the quotes of known instruments whose modification time
// differs from the snapshot's, counting the others in res.
func (p *Poller) changed(quotes []domain.Quote, known map[domain.Symbol]bool, res *Result) []domain.Quote {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []domain.Quote
	for _, q := range quotes {
		switch old, ok := p.quotes[q.Symbol]; {
		case !known[q.Symbol]:
			res.Unknown++
		case ok && old.AsOf.Equal(q.AsOf):
			res.Unchanged++
		default:
			out = append(out, q)
		}
	}
	return out
}

func (p *Poller) store(ctx context.Context, quotes []domain.Quote) error {
	if len(quotes) == 0 {
		return nil
	}
	err := pgx.BeginFunc(ctx, p.opts.DB, func(tx pgx.Tx) error {
		q := dbgen.New(tx)
		for _, quote := range quotes {
			arg := dbgen.UpsertMarketWatchQuoteParams{
				Symbol: quote.Symbol, Last: quote.Last, Bid: quote.Bid, Ask: quote.Ask,
				BidSize: quote.BidSize, AskSize: quote.AskSize, Volume: quote.Volume, Trades: quote.Trades,
				Value: quote.Value, PrevClose: quote.PrevClose, AsOf: quote.AsOf,
				LastTradeAt: pgtype.Timestamptz{Time: quote.LastTradeAt, Valid: !quote.LastTradeAt.IsZero()},
			}
			if _, err := q.UpsertMarketWatchQuote(ctx, arg); err != nil {
				return fmt.Errorf("upsert %s: %w", quote.Symbol, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("marketwatch: store quotes: %w", err)
	}
	return nil
}

// Run loads the stored snapshot, then polls until ctx is cancelled and
// returns nil. It polls once at start, every Interval while the calendar
// says DFM is in pre-open, continuous trading, or the closing auction, and
// once CloseDelay after each session ends; otherwise it sleeps until the
// next pre-open. A failed poll is logged and retried after a wait that
// doubles from 2 Intervals up to MaxBackoff, until one succeeds. The
// calendar is reloaded before each step, so a closure added later stops
// polling on that day.
func (p *Poller) Run(ctx context.Context) error {
	logger := p.opts.Logger
	if err := p.Load(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		logger.ErrorContext(ctx, "market watch: load snapshot", "error", err)
	}
	// owed is a poll due while the market is closed: at start and after
	// each session. dueAt is when; zero means now.
	owed, inSession := true, false
	var dueAt time.Time
	for {
		cal, err := p.opts.Calendar.Load(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.ErrorContext(ctx, "market watch: load calendar", "error", err)
			if !p.opts.Wait(ctx, retryLoad) {
				return nil
			}
			continue
		}
		now := p.opts.Now()
		var next time.Duration
		switch {
		case cal.Session(now) != calendar.Closed:
			owed, inSession = true, true
			next = p.pollOnce(ctx)
		case owed && inSession:
			inSession, dueAt = false, now.Add(p.opts.CloseDelay)
			next = p.opts.CloseDelay
		case owed && now.Before(dueAt):
			next = dueAt.Sub(now)
		case owed:
			if next = p.pollOnce(ctx); p.Health().ConsecutiveFailures == 0 {
				owed = false
				next = p.untilPreOpen(cal, now)
			}
		default:
			next = p.untilPreOpen(cal, now)
		}
		if ctx.Err() != nil || !p.opts.Wait(ctx, next) {
			return nil
		}
	}
}

// untilPreOpen returns the time from now to the next pre-open session.
func (p *Poller) untilPreOpen(cal *calendar.Calendar, now time.Time) time.Duration {
	lead := p.opts.PreOpenLead
	return cal.NextOpen(now.Add(lead)).Add(-lead).Sub(now)
}

// pollOnce polls with a logger that carries a new run_id, records the
// outcome in Health, and returns the wait before the next poll.
func (p *Poller) pollOnce(ctx context.Context) time.Duration {
	logger := p.opts.Logger.With("run_id", uuid.NewString())
	res, err := p.Poll(ctx, logger)
	if ctx.Err() != nil {
		return 0
	}
	now := p.opts.Now()
	p.mu.Lock()
	if err == nil {
		p.health.LastSuccess = now
		p.health.ConsecutiveFailures = 0
	} else {
		p.health.LastFailure = now
		p.health.LastError = err.Error()
		p.health.ConsecutiveFailures++
	}
	h := p.health
	p.mu.Unlock()

	attrs := []any{"lines", res.Lines, "changed", res.Changed, "unchanged", res.Unchanged,
		"unknown", res.Unknown, "skipped", res.Skipped}
	if err == nil {
		logger.InfoContext(ctx, "market watch poll finished", attrs...)
		return p.opts.Interval
	}
	backoff := p.backoff(h.ConsecutiveFailures)
	logger.ErrorContext(ctx, "market watch poll failed", append(attrs,
		"error", err, "consecutive_failures", h.ConsecutiveFailures, "retry_in", backoff.String())...)
	return backoff
}

// backoff returns the wait after n consecutive failed polls: Interval
// doubled n times, at most MaxBackoff.
func (p *Poller) backoff(n int) time.Duration {
	d := p.opts.Interval
	for range n {
		if d >= p.opts.MaxBackoff {
			break
		}
		d *= 2
	}
	return min(d, p.opts.MaxBackoff)
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
