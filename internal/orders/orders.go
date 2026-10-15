// Package orders stores orders and their audit log.
//
// An order is keyed by its client order ID, a UUIDv7 the caller creates with
// NewID before submitting, so a retried Create finds the first order instead
// of making a second one.
//
// Store.Transition is the only way to change an order's state. It applies the
// transition table in package domain and appends an order_events row in the
// same transaction as the state change. Store.Modify changes an open order's
// quantity and limit price the same way. Store.Record appends a row for a
// broker message or user action that changes nothing else. order_events is the
// record of truth for an order's history: the database rejects UPDATE, DELETE,
// and TRUNCATE on it. The database also rejects any change to an order's ID,
// symbol, or side after Create.
package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/store/dbgen"
)

var (
	// ErrNotFound is returned for an order ID that has no order.
	ErrNotFound = errors.New("order not found")
	// ErrIDReused is returned by Create when the client order ID already
	// belongs to an order with different terms.
	ErrIDReused = errors.New("client order ID belongs to a different order")
	// ErrNotModifiable is returned by Modify for an order in a terminal state.
	ErrNotModifiable = errors.New("order is not modifiable")
)

// NewID returns a new client order ID.
func NewID() (uuid.UUID, error) { return uuid.NewV7() }

// Order is one order and its current state.
type Order struct {
	ID         uuid.UUID // client order ID, a UUIDv7
	Symbol     domain.Symbol
	Side       domain.Side
	Quantity   decimal.Decimal
	LimitPrice decimal.Decimal // zero for a market order
	State      domain.OrderState
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Source says who or what caused an event.
type Source string

// Event sources: the user, the broker, or this service (for example a risk
// check or a reconciliation).
const (
	SourceUser   Source = "user"
	SourceBroker Source = "broker"
	SourceSystem Source = "system"
)

// Valid reports whether s is a known source.
func (s Source) Valid() bool {
	switch s {
	case SourceUser, SourceBroker, SourceSystem:
		return true
	}
	return false
}

// Change describes the cause of an event.
type Change struct {
	Source Source
	Action string          // a short verb, for example "submit", "fill", or "modify"
	Detail json.RawMessage // optional JSON, for example the fields of the broker's message
	// Raw is the optional message as received, kept verbatim, for example an
	// IBKR error text. Detail is stored as jsonb, which reorders its keys.
	Raw string
}

// Event is one row of an order's audit log.
//
// To is the state the event moved the order to, and is empty when the event
// left the state alone. From is empty only on the event that created the order.
type Event struct {
	ID         int64
	OrderID    uuid.UUID
	Source     Source
	Action     string
	From, To   domain.OrderState
	Detail     json.RawMessage
	Raw        string
	RecordedAt time.Time
}

// Store reads and writes orders and their events.
type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewStore returns a Store on pool that timestamps rows with now, or with
// time.Now when now is nil.
func NewStore(pool *pgxpool.Pool, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{pool: pool, now: now}
}

// Create stores o as a new draft order, ignoring o.State and the timestamps,
// and records c as the order's first event. It returns the stored order and
// whether this call created it. When an order with o.ID already exists and was
// created with the same symbol, side, quantity, and limit price, Create returns
// that order as it is now with created false, so retries are safe even after a
// Modify. When the terms differ it returns ErrIDReused.
func (s *Store) Create(ctx context.Context, o Order, c Change) (stored Order, created bool, err error) {
	if err := validate(o); err != nil {
		return Order{}, false, err
	}
	if err := validateChange(c); err != nil {
		return Order{}, false, err
	}
	now := s.now()
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbgen.New(tx)
		row, err := q.InsertOrder(ctx, dbgen.InsertOrderParams{
			ClientOrderID: o.ID,
			Symbol:        o.Symbol,
			Side:          o.Side,
			Quantity:      o.Quantity,
			LimitPrice:    nullDecimal(o.LimitPrice),
			State:         domain.Draft,
			CreatedAt:     now,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			existing, err := q.GetOrder(ctx, o.ID)
			if err != nil {
				return err
			}
			stored = fromRow(existing)
			if !createdWith(existing, o) {
				return fmt.Errorf("%w: %s", ErrIDReused, o.ID)
			}
			return nil
		}
		if err != nil {
			return err
		}
		stored, created = fromRow(row), true
		_, err = insertEvent(ctx, q, o.ID, "", domain.Draft, c, now)
		return err
	})
	if err != nil {
		return Order{}, false, fmt.Errorf("orders: create %s: %w", o.ID, err)
	}
	return stored, created, nil
}

// Transition moves order id to state to if the transition table allows it, and
// records c as the event that did so. It returns the updated order. An illegal
// transition returns an error wrapping domain.ErrIllegalTransition and changes
// nothing.
func (s *Store) Transition(ctx context.Context, id uuid.UUID, to domain.OrderState, c Change) (Order, error) {
	if err := validateChange(c); err != nil {
		return Order{}, err
	}
	var updated Order
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbgen.New(tx)
		row, err := lock(ctx, q, id)
		if err != nil {
			return err
		}
		from := row.State
		next, err := from.Transition(to)
		if err != nil {
			return err
		}
		now := s.now()
		row, err = q.SetOrderState(ctx, dbgen.SetOrderStateParams{ClientOrderID: id, State: next, UpdatedAt: now})
		if err != nil {
			return err
		}
		updated = fromRow(row)
		_, err = insertEvent(ctx, q, id, from, next, c, now)
		return err
	})
	if err != nil {
		return Order{}, fmt.Errorf("orders: transition %s to %q: %w", id, to, err)
	}
	return updated, nil
}

// Modify sets the quantity and limit price of order id, and records c as the
// event that did so. Its detail holds the old and new terms, with c.Detail, if
// any, under "detail". The state is unchanged. A zero limitPrice makes the order
// a market order. An order in a terminal state returns an error wrapping
// ErrNotModifiable and changes nothing.
func (s *Store) Modify(ctx context.Context, id uuid.UUID, quantity, limitPrice decimal.Decimal, c Change) (Order, error) {
	if err := validateTerms(quantity, limitPrice); err != nil {
		return Order{}, err
	}
	if err := validateChange(c); err != nil {
		return Order{}, err
	}
	var updated Order
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbgen.New(tx)
		row, err := lock(ctx, q, id)
		if err != nil {
			return err
		}
		if row.State.Terminal() {
			return fmt.Errorf("%w: state %q is terminal", ErrNotModifiable, row.State)
		}
		now := s.now()
		next, err := q.ModifyOrder(ctx, dbgen.ModifyOrderParams{
			ClientOrderID: id,
			Quantity:      quantity,
			LimitPrice:    nullDecimal(limitPrice),
			UpdatedAt:     now,
		})
		if err != nil {
			return err
		}
		updated = fromRow(next)
		detail, err := json.Marshal(modifyDetail{
			Old:    termsOf(fromRow(row)),
			New:    termsOf(updated),
			Detail: c.Detail,
		})
		if err != nil {
			return err
		}
		c.Detail = detail
		_, err = insertEvent(ctx, q, id, "", "", c, now)
		return err
	})
	if err != nil {
		return Order{}, fmt.Errorf("orders: modify %s: %w", id, err)
	}
	return updated, nil
}

type terms struct {
	Quantity   decimal.Decimal  `json:"quantity"`
	LimitPrice *decimal.Decimal `json:"limit_price"` // null for a market order
}

func termsOf(o Order) terms {
	t := terms{Quantity: o.Quantity}
	if !o.LimitPrice.IsZero() {
		t.LimitPrice = &o.LimitPrice
	}
	return t
}

type modifyDetail struct {
	Old    terms           `json:"old"`
	New    terms           `json:"new"`
	Detail json.RawMessage `json:"detail,omitempty"`
}

// Record appends c to the audit log of order id without changing its state.
// Use it for a broker message or user action that is not a transition, such as
// a modify request or a late commission report. It works in every state.
func (s *Store) Record(ctx context.Context, id uuid.UUID, c Change) (Event, error) {
	if err := validateChange(c); err != nil {
		return Event{}, err
	}
	var ev Event
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbgen.New(tx)
		if _, err := lock(ctx, q, id); err != nil {
			return err
		}
		var err error
		ev, err = insertEvent(ctx, q, id, "", "", c, s.now())
		return err
	})
	if err != nil {
		return Event{}, fmt.Errorf("orders: record on %s: %w", id, err)
	}
	return ev, nil
}

// Get returns order id.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (Order, error) {
	row, err := dbgen.New(s.pool).GetOrder(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("orders: get %s: %w", id, err)
	}
	return fromRow(row), nil
}

// Events returns the audit log of order id, oldest first. It is empty for an
// unknown order.
func (s *Store) Events(ctx context.Context, id uuid.UUID) ([]Event, error) {
	rows, err := dbgen.New(s.pool).ListOrderEvents(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("orders: events of %s: %w", id, err)
	}
	events := make([]Event, len(rows))
	for i, r := range rows {
		events[i] = eventFromRow(r)
	}
	return events, nil
}

func lock(ctx context.Context, q *dbgen.Queries, id uuid.UUID) (dbgen.Order, error) {
	row, err := q.LockOrder(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Order{}, ErrNotFound
	}
	return row, err
}

func insertEvent(ctx context.Context, q *dbgen.Queries, id uuid.UUID, from, to domain.OrderState, c Change, at time.Time) (Event, error) {
	detail := c.Detail
	if detail == nil {
		detail = json.RawMessage(`{}`)
	}
	row, err := q.InsertOrderEvent(ctx, dbgen.InsertOrderEventParams{
		ClientOrderID: id,
		Source:        string(c.Source),
		Action:        c.Action,
		FromState:     optionalText(string(from)),
		ToState:       optionalText(string(to)),
		Detail:        detail,
		RawMessage:    optionalText(c.Raw),
		RecordedAt:    at,
	})
	if err != nil {
		return Event{}, err
	}
	return eventFromRow(row), nil
}

func nullDecimal(d decimal.Decimal) decimal.NullDecimal {
	return decimal.NullDecimal{Decimal: d, Valid: !d.IsZero()}
}

func optionalText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func fromRow(r dbgen.Order) Order {
	return Order{
		ID:         r.ClientOrderID,
		Symbol:     r.Symbol,
		Side:       r.Side,
		Quantity:   r.Quantity,
		LimitPrice: r.LimitPrice.Decimal,
		State:      r.State,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}

func eventFromRow(r dbgen.OrderEvent) Event {
	return Event{
		ID:         r.ID,
		OrderID:    r.ClientOrderID,
		Source:     Source(r.Source),
		Action:     r.Action,
		From:       domain.OrderState(r.FromState.String),
		To:         domain.OrderState(r.ToState.String),
		Detail:     json.RawMessage(r.Detail),
		Raw:        r.RawMessage.String,
		RecordedAt: r.RecordedAt,
	}
}

// createdWith reports whether r was created with the terms of o.
func createdWith(r dbgen.Order, o Order) bool {
	return r.Symbol == o.Symbol && r.Side == o.Side &&
		r.CreatedQuantity.Equal(o.Quantity) && r.CreatedLimitPrice.Decimal.Equal(o.LimitPrice)
}

func validate(o Order) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("orders: %w order: %s", domain.ErrInvalid, fmt.Sprintf(format, args...))
	}
	switch {
	case o.ID.Version() != 7 || o.ID.Variant() != uuid.RFC4122:
		return fail("client order ID %s is not a UUIDv7", o.ID)
	case !o.Symbol.Valid():
		return fail("symbol %q is not a trading code", o.Symbol)
	case !o.Side.Valid():
		return fail("unknown side %q", o.Side)
	}
	return validateTerms(o.Quantity, o.LimitPrice)
}

func validateTerms(quantity, limitPrice decimal.Decimal) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("orders: %w order: %s", domain.ErrInvalid, fmt.Sprintf(format, args...))
	}
	switch {
	case !quantity.IsPositive():
		return fail("quantity %s is not positive", quantity)
	case limitPrice.IsNegative():
		return fail("limit price %s is negative", limitPrice)
	}
	return nil
}

func validateChange(c Change) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("orders: %w change: %s", domain.ErrInvalid, fmt.Sprintf(format, args...))
	}
	switch {
	case !c.Source.Valid():
		return fail("unknown source %q", c.Source)
	case c.Action == "":
		return fail("action is missing")
	case c.Detail != nil && !json.Valid(c.Detail):
		return fail("detail is not JSON")
	}
	return nil
}
