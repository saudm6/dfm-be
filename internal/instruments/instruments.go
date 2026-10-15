// Package instruments keeps the security master, the DFM instruments every
// other feature looks up, in the instruments table. Sync refreshes it from
// DFM's web API; Job runs Sync before each trading session.
package instruments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/saudm6/dfm-be/internal/dfm"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/store/dbgen"
)

// ErrNotFound is returned for a symbol the security master does not hold.
var ErrNotFound = errors.New("instruments: not found")

// Source supplies the security master; *dfm.Client is one.
type Source interface {
	SecurityMaster(ctx context.Context, now time.Time) (dfm.SecurityMaster, error)
}

// DB is a database the sync can open a transaction on, such as
// *pgxpool.Pool.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Result counts what a sync did.
type Result struct {
	// Instruments is the number of instruments the source listed.
	Instruments int
	// Written is the number of instruments inserted or changed.
	Written int
	// Delisted is the number of stored instruments the source no longer
	// lists, now marked delisted.
	Delisted int
	// Boards is the number of board codes the source listed.
	Boards int
	// BoardsWritten is the number of board codes inserted or changed.
	BoardsWritten int
	// BoardsMissing is the number of stored board codes the market watch
	// no longer shows, now set to 0 lines and marked missing.
	BoardsMissing int
	// Skipped is the number of source lines left out.
	Skipped int
}

// Sync fetches the security master from src as of now and upserts it by
// symbol in one transaction. Rows that did not change are not written, so a
// sync with no upstream change writes nothing. Instruments are never
// deleted: one DFM stops listing is marked delisted. Board line counts are
// recomputed each sync; a board the market watch no longer shows gets 0 lines
// and a missing-since time of now. Skipped lines are logged at WARN.
func Sync(ctx context.Context, db DB, src Source, now time.Time, logger *slog.Logger) (Result, error) {
	m, err := src.SecurityMaster(ctx, now)
	if err != nil {
		return Result{}, fmt.Errorf("instruments: fetch security master: %w", err)
	}
	for _, s := range m.Skipped {
		logger.WarnContext(ctx, "security master line skipped", "symbol", s.Symbol, "reason", s.Reason)
	}
	res := Result{Instruments: len(m.Instruments), Boards: len(m.Boards), Skipped: len(m.Skipped)}
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		q := dbgen.New(tx)
		present := make([]string, len(m.Instruments))
		for i, in := range m.Instruments {
			present[i] = string(in.Symbol)
			n, err := q.UpsertInstrument(ctx, upsertParams(in))
			if err != nil {
				return fmt.Errorf("upsert instrument %s: %w", in.Symbol, err)
			}
			res.Written += int(n)
		}
		n, err := q.DelistMissingInstruments(ctx, present)
		if err != nil {
			return fmt.Errorf("delist missing instruments: %w", err)
		}
		res.Delisted = int(n)
		for _, b := range m.Boards {
			n, err := q.UpsertBoard(ctx, dbgen.UpsertBoardParams{
				Code:      b.Code,
				Exchanges: texts(b.Exchanges),
				Classes:   texts(b.Classes),
				Lines:     int32(b.Lines),
			})
			if err != nil {
				return fmt.Errorf("upsert board %s: %w", b.Code, err)
			}
			res.BoardsWritten += int(n)
		}
		codes := make([]string, len(m.Boards))
		for i, b := range m.Boards {
			codes[i] = b.Code
		}
		n, err = q.MarkMissingBoards(ctx, dbgen.MarkMissingBoardsParams{
			MissingSince: pgtype.Timestamptz{Time: now, Valid: true},
			Present:      codes,
		})
		if err != nil {
			return fmt.Errorf("mark missing boards: %w", err)
		}
		res.BoardsMissing = int(n)
		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("instruments: %w", err)
	}
	return res, nil
}

func upsertParams(in domain.Instrument) dbgen.UpsertInstrumentParams {
	p := dbgen.UpsertInstrumentParams{
		Symbol:   in.Symbol,
		Exchange: in.Exchange,
		Isin:     in.ISIN,
		NameEn:   in.NameEN,
		NameAr:   in.NameAR,
		Class:    in.Class,
		Currency: in.Currency,
		Sector:   in.Sector,
		Board:    in.Board,
		Status:   in.Status,
	}
	if !in.ListedAt.IsZero() {
		p.ListedAt.Time, p.ListedAt.Valid = in.ListedAt, true
	}
	return p
}

func texts[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

// Get returns the instrument sym. ListedAt is in UTC.
func Get(ctx context.Context, db dbgen.DBTX, sym domain.Symbol) (domain.Instrument, error) {
	row, err := dbgen.New(db).GetInstrument(ctx, sym)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Instrument{}, fmt.Errorf("%w: %s", ErrNotFound, sym)
	}
	if err != nil {
		return domain.Instrument{}, fmt.Errorf("instruments: get %s: %w", sym, err)
	}
	in := domain.Instrument{
		Symbol:   row.Symbol,
		Exchange: row.Exchange,
		ISIN:     row.Isin,
		NameEN:   row.NameEn,
		NameAR:   row.NameAr,
		Class:    row.Class,
		Currency: row.Currency,
		Sector:   row.Sector,
		Board:    row.Board,
		Status:   row.Status,
	}
	if row.ListedAt.Valid {
		in.ListedAt = row.ListedAt.Time.UTC()
	}
	return in, nil
}

// Board is a stored board code.
type Board struct {
	dfm.Board
	// MissingSince is when a sync first found the board absent from the
	// market watch; Lines is then 0 and Exchanges and Classes are as last
	// seen. It is zero while the board is in the market watch.
	MissingSince time.Time
}

// Boards returns the board-code registry, ordered by code. MissingSince is
// in UTC.
func Boards(ctx context.Context, db dbgen.DBTX) ([]Board, error) {
	rows, err := dbgen.New(db).ListBoards(ctx)
	if err != nil {
		return nil, fmt.Errorf("instruments: list boards: %w", err)
	}
	out := make([]Board, len(rows))
	for i, r := range rows {
		out[i].Board = dfm.Board{
			Code:      r.Code,
			Exchanges: make([]domain.Exchange, 0, len(r.Exchanges)),
			Classes:   make([]domain.InstrumentClass, 0, len(r.Classes)),
			Lines:     int(r.Lines),
		}
		if r.MissingSince.Valid {
			out[i].MissingSince = r.MissingSince.Time.UTC()
		}
		for _, e := range r.Exchanges {
			out[i].Exchanges = append(out[i].Exchanges, domain.Exchange(e))
		}
		for _, c := range r.Classes {
			out[i].Classes = append(out[i].Classes, domain.InstrumentClass(c))
		}
	}
	return out, nil
}
