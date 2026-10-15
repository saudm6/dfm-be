package orders_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/orders"
	"github.com/saudm6/dfm-be/internal/store/storetest"
)

var (
	orderID = uuid.MustParse("0199e7a4-9c3b-7d4e-8f00-000000000001")
	t0      = time.Date(2026, 10, 15, 6, 0, 0, 0, time.UTC)
)

// clock returns a clock that starts at t0 and moves on a second per call.
func clock() func() time.Time {
	var mu sync.Mutex
	next := t0
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now := next
		next = next.Add(time.Second)
		return now
	}
}

func newStore(t *testing.T) (*orders.Store, *pgxpool.Pool) {
	t.Helper()
	pool := storetest.NewPool(t)
	return orders.NewStore(pool, clock()), pool
}

func emaarBuy(id uuid.UUID) orders.Order {
	return orders.Order{
		ID:         id,
		Symbol:     "EMAAR",
		Side:       domain.Buy,
		Quantity:   decimal.RequireFromString("1000"),
		LimitPrice: decimal.RequireFromString("14.25"),
	}
}

var userCreate = orders.Change{Source: orders.SourceUser, Action: "create"}

func change(src orders.Source, action string) orders.Change {
	return orders.Change{Source: src, Action: action}
}

// eventsOf returns the events of id with their IDs and order IDs cleared.
func eventsOf(t *testing.T, s *orders.Store, id uuid.UUID) []orders.Event {
	t.Helper()
	events, err := s.Events(t.Context(), id)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	for i := range events {
		if events[i].OrderID != id {
			t.Errorf("event %d OrderID = %s, want %s", i, events[i].OrderID, id)
		}
		if i > 0 && events[i].ID <= events[i-1].ID {
			t.Errorf("event %d ID %d not after %d", i, events[i].ID, events[i-1].ID)
		}
		events[i].ID, events[i].OrderID = 0, uuid.Nil
		events[i].RecordedAt = events[i].RecordedAt.UTC()
	}
	return events
}

func sameOrder(t *testing.T, got, want orders.Order) {
	t.Helper()
	if got.ID != want.ID || got.Symbol != want.Symbol || got.Side != want.Side ||
		!got.Quantity.Equal(want.Quantity) || !got.LimitPrice.Equal(want.LimitPrice) ||
		got.State != want.State || !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("order = %+v, want %+v", got, want)
	}
}

func TestCreateRetryReturnsFirstOrder(t *testing.T) {
	s, _ := newStore(t)
	want := emaarBuy(orderID)
	want.State, want.CreatedAt, want.UpdatedAt = domain.Draft, t0, t0

	first, created, err := s.Create(t.Context(), emaarBuy(orderID), userCreate)
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if !created {
		t.Errorf("first Create() created = false, want true")
	}
	sameOrder(t, first, want)

	retry := emaarBuy(orderID)
	retry.Quantity = decimal.RequireFromString("1000.00")
	second, created, err := s.Create(t.Context(), retry, userCreate)
	if err != nil {
		t.Fatalf("retried Create() error = %v", err)
	}
	if created {
		t.Errorf("retried Create() created = true, want false")
	}
	sameOrder(t, second, want)

	got, err := s.Get(t.Context(), orderID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	sameOrder(t, got, want)

	wantEvents := []orders.Event{
		{Source: "user", Action: "create", To: "draft", Detail: json.RawMessage(`{}`), RecordedAt: t0},
	}
	if got := eventsOf(t, s, orderID); !reflect.DeepEqual(got, wantEvents) {
		t.Errorf("Events() = %+v, want %+v", got, wantEvents)
	}
}

func TestCreateRejectsReusedID(t *testing.T) {
	tests := []struct {
		name string
		edit func(*orders.Order)
	}{
		{name: "symbol", edit: func(o *orders.Order) { o.Symbol = "DEWA" }},
		{name: "side", edit: func(o *orders.Order) { o.Side = domain.Sell }},
		{name: "quantity", edit: func(o *orders.Order) { o.Quantity = decimal.RequireFromString("1001") }},
		{name: "limit price", edit: func(o *orders.Order) { o.LimitPrice = decimal.RequireFromString("14.26") }},
		{name: "market order", edit: func(o *orders.Order) { o.LimitPrice = decimal.Zero }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newStore(t)
			if _, _, err := s.Create(t.Context(), emaarBuy(orderID), userCreate); err != nil {
				t.Fatalf("first Create() error = %v", err)
			}
			o := emaarBuy(orderID)
			tt.edit(&o)

			_, _, err := s.Create(t.Context(), o, userCreate)

			want := "orders: create 0199e7a4-9c3b-7d4e-8f00-000000000001: client order ID belongs to a different order: 0199e7a4-9c3b-7d4e-8f00-000000000001"
			if err == nil || err.Error() != want {
				t.Fatalf("Create() error = %v, want %q", err, want)
			}
			if !errors.Is(err, orders.ErrIDReused) {
				t.Errorf("errors.Is(%v, ErrIDReused) = false, want true", err)
			}
			if got := len(eventsOf(t, s, orderID)); got != 1 {
				t.Errorf("Events() has %d rows, want 1", got)
			}
		})
	}
}

func TestConcurrentRetriesCreateOneOrder(t *testing.T) {
	s, pool := newStore(t)
	const callers = 8

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		errs    []error
	)
	for range callers {
		wg.Go(func() {
			_, c, err := s.Create(t.Context(), emaarBuy(orderID), userCreate)
			mu.Lock()
			defer mu.Unlock()
			if c {
				created++
			}
			errs = append(errs, err)
		})
	}
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		t.Fatalf("Create() errors: %v", err)
	}
	if created != 1 {
		t.Errorf("%d of %d callers created the order, want 1", created, callers)
	}
	var rows, events int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM orders").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM order_events").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || events != 1 {
		t.Errorf("orders rows = %d, order_events rows = %d, want 1 and 1", rows, events)
	}
}

func TestCreateValidates(t *testing.T) {
	v4 := uuid.MustParse("6f1c2c9e-3b0a-4d7e-9f00-000000000001")
	tests := []struct {
		name    string
		edit    func(*orders.Order)
		change  orders.Change
		wantErr string
	}{
		{name: "nil ID", edit: func(o *orders.Order) { o.ID = uuid.Nil }, change: userCreate,
			wantErr: "orders: invalid order: client order ID 00000000-0000-0000-0000-000000000000 is not a UUIDv7"},
		{name: "UUIDv4", edit: func(o *orders.Order) { o.ID = v4 }, change: userCreate,
			wantErr: "orders: invalid order: client order ID 6f1c2c9e-3b0a-4d7e-9f00-000000000001 is not a UUIDv7"},
		{name: "symbol", edit: func(o *orders.Order) { o.Symbol = "emaar" }, change: userCreate,
			wantErr: `orders: invalid order: symbol "emaar" is not a trading code`},
		{name: "side", edit: func(o *orders.Order) { o.Side = "short" }, change: userCreate,
			wantErr: `orders: invalid order: unknown side "short"`},
		{name: "zero quantity", edit: func(o *orders.Order) { o.Quantity = decimal.Zero }, change: userCreate,
			wantErr: "orders: invalid order: quantity 0 is not positive"},
		{name: "negative limit", edit: func(o *orders.Order) { o.LimitPrice = decimal.RequireFromString("-1") }, change: userCreate,
			wantErr: "orders: invalid order: limit price -1 is negative"},
		{name: "source", edit: func(*orders.Order) {}, change: orders.Change{Source: "ibkr", Action: "create"},
			wantErr: `orders: invalid change: unknown source "ibkr"`},
		{name: "action", edit: func(*orders.Order) {}, change: orders.Change{Source: orders.SourceUser},
			wantErr: "orders: invalid change: action is missing"},
		{name: "detail", edit: func(*orders.Order) {}, change: orders.Change{Source: orders.SourceUser, Action: "create", Detail: json.RawMessage(`{`)},
			wantErr: "orders: invalid change: detail is not JSON"},
	}
	s, _ := newStore(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := emaarBuy(orderID)
			tt.edit(&o)
			_, _, err := s.Create(t.Context(), o, tt.change)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Create() error = %v, want %q", err, tt.wantErr)
			}
			if !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("errors.Is(%v, ErrInvalid) = false, want true", err)
			}
		})
	}
	if _, err := s.Get(t.Context(), orderID); !errors.Is(err, orders.ErrNotFound) {
		t.Errorf("Get() after rejected creates error = %v, want ErrNotFound", err)
	}
}

func TestLifecycleIsAudited(t *testing.T) {
	s, _ := newStore(t)
	ctx := t.Context()
	if _, _, err := s.Create(ctx, emaarBuy(orderID), userCreate); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	steps := []struct {
		to     domain.OrderState
		change orders.Change
	}{
		{to: domain.Submitted, change: change(orders.SourceUser, "submit")},
		{to: domain.Working, change: change(orders.SourceBroker, "status")},
		{to: domain.PartFilled, change: orders.Change{Source: orders.SourceBroker, Action: "fill", Detail: json.RawMessage(`{"execId": "0001", "shares": "400"}`)}},
		{to: domain.PartFilled, change: orders.Change{Source: orders.SourceBroker, Action: "fill", Detail: json.RawMessage(`{"execId": "0002", "shares": "100"}`)}},
		{to: domain.Filled, change: orders.Change{Source: orders.SourceBroker, Action: "fill", Detail: json.RawMessage(`{"execId": "0003", "shares": "500"}`)}},
	}
	for _, st := range steps {
		if _, err := s.Transition(ctx, orderID, st.to, st.change); err != nil {
			t.Fatalf("Transition(%q) error = %v", st.to, err)
		}
	}
	commission := orders.Change{Source: orders.SourceBroker, Action: "commission", Detail: json.RawMessage(`{"commission":"35.62","currency":"AED"}`)}
	ev, err := s.Record(ctx, orderID, commission)
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if ev.From != "" || ev.To != "" || ev.Action != "commission" {
		t.Errorf("Record() = %+v, want an event with no states and action commission", ev)
	}

	got, err := s.Get(ctx, orderID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	want := emaarBuy(orderID)
	want.State, want.CreatedAt, want.UpdatedAt = domain.Filled, t0, t0.Add(5*time.Second)
	sameOrder(t, got, want)

	sec := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Second) }
	wantEvents := []orders.Event{
		{Source: "user", Action: "create", To: "draft", Detail: json.RawMessage(`{}`), RecordedAt: sec(0)},
		{Source: "user", Action: "submit", From: "draft", To: "submitted", Detail: json.RawMessage(`{}`), RecordedAt: sec(1)},
		{Source: "broker", Action: "status", From: "submitted", To: "working", Detail: json.RawMessage(`{}`), RecordedAt: sec(2)},
		{Source: "broker", Action: "fill", From: "working", To: "part_filled", Detail: json.RawMessage(`{"execId": "0001", "shares": "400"}`), RecordedAt: sec(3)},
		{Source: "broker", Action: "fill", From: "part_filled", To: "part_filled", Detail: json.RawMessage(`{"execId": "0002", "shares": "100"}`), RecordedAt: sec(4)},
		{Source: "broker", Action: "fill", From: "part_filled", To: "filled", Detail: json.RawMessage(`{"execId": "0003", "shares": "500"}`), RecordedAt: sec(5)},
		{Source: "broker", Action: "commission", Detail: json.RawMessage(`{"currency": "AED", "commission": "35.62"}`), RecordedAt: sec(6)},
	}
	if got := eventsOf(t, s, orderID); !reflect.DeepEqual(got, wantEvents) {
		t.Errorf("Events() =\n%+v\nwant\n%+v", got, wantEvents)
	}
}

// pathTo lists the transitions that take a new draft order to each state.
var pathTo = map[domain.OrderState][]domain.OrderState{
	domain.Draft:      nil,
	domain.Submitted:  {domain.Submitted},
	domain.Working:    {domain.Submitted, domain.Working},
	domain.PartFilled: {domain.Submitted, domain.Working, domain.PartFilled},
	domain.Filled:     {domain.Submitted, domain.Filled},
	domain.Cancelled:  {domain.Cancelled},
	domain.Rejected:   {domain.Rejected},
}

// TestStoreWalksTransitionTable tries every pair of states through the store:
// the 14 legal transitions succeed and append an event, and each illegal one
// fails and leaves the order and its audit log as they were.
func TestStoreWalksTransitionTable(t *testing.T) {
	s, _ := newStore(t)
	ctx := t.Context()
	var legal []string
	for _, from := range domain.OrderStates() {
		for _, to := range domain.OrderStates() {
			id, err := orders.NewID()
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.Create(ctx, emaarBuy(id), userCreate); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			for _, step := range pathTo[from] {
				if _, err := s.Transition(ctx, id, step, change(orders.SourceSystem, "setup")); err != nil {
					t.Fatalf("%s: setup Transition(%q) error = %v", from, step, err)
				}
			}
			before, err := s.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			eventsBefore := len(eventsOf(t, s, id))

			got, err := s.Transition(ctx, id, to, change(orders.SourceUser, "walk"))

			pair := fmt.Sprintf("%s->%s", from, to)
			if err == nil {
				legal = append(legal, pair)
				if got.State != to {
					t.Errorf("%s: Transition() state = %q, want %q", pair, got.State, to)
				}
				events := eventsOf(t, s, id)
				if last := events[len(events)-1]; len(events) != eventsBefore+1 || last.From != from || last.To != to || last.Action != "walk" {
					t.Errorf("%s: last of %d events = %+v, want one new %s event", pair, len(events), last, pair)
				}
				continue
			}
			want := fmt.Sprintf("orders: transition %s to %q: domain: illegal order transition: %q -> %q", id, to, from, to)
			if err.Error() != want {
				t.Errorf("%s: Transition() error = %q, want %q", pair, err, want)
			}
			if !errors.Is(err, domain.ErrIllegalTransition) {
				t.Errorf("%s: errors.Is(%v, ErrIllegalTransition) = false, want true", pair, err)
			}
			after, err := s.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			sameOrder(t, after, before)
			if n := len(eventsOf(t, s, id)); n != eventsBefore {
				t.Errorf("%s: %d events after a rejected transition, want %d", pair, n, eventsBefore)
			}
		}
	}
	wantLegal := []string{
		"draft->submitted", "draft->cancelled", "draft->rejected",
		"submitted->working", "submitted->part_filled", "submitted->filled", "submitted->cancelled", "submitted->rejected",
		"working->part_filled", "working->filled", "working->cancelled",
		"part_filled->part_filled", "part_filled->filled", "part_filled->cancelled",
	}
	if !reflect.DeepEqual(legal, wantLegal) {
		t.Errorf("legal transitions = %q, want %q", legal, wantLegal)
	}
}

func TestConcurrentTransitionsOneWins(t *testing.T) {
	s, _ := newStore(t)
	ctx := t.Context()
	if _, _, err := s.Create(ctx, emaarBuy(orderID), userCreate); err != nil {
		t.Fatal(err)
	}
	for _, step := range []domain.OrderState{domain.Submitted, domain.Working} {
		if _, err := s.Transition(ctx, orderID, step, change(orders.SourceSystem, "setup")); err != nil {
			t.Fatal(err)
		}
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners []domain.OrderState
	)
	for i := range 8 {
		to := domain.Filled
		if i%2 == 1 {
			to = domain.Cancelled
		}
		wg.Go(func() {
			_, err := s.Transition(ctx, orderID, to, change(orders.SourceBroker, "race"))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners = append(winners, to)
			case !errors.Is(err, domain.ErrIllegalTransition):
				t.Errorf("Transition(%q) error = %v, want nil or ErrIllegalTransition", to, err)
			}
		})
	}
	wg.Wait()

	if len(winners) != 1 {
		t.Fatalf("%d transitions out of working succeeded (%q), want 1", len(winners), winners)
	}
	got, err := s.Get(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != winners[0] {
		t.Errorf("state = %q, want the winner %q", got.State, winners[0])
	}
	events := eventsOf(t, s, orderID)
	if len(events) != 4 || events[3].From != domain.Working || events[3].To != winners[0] {
		t.Errorf("events = %+v, want create, submit, working, and one working->%s", events, winners[0])
	}
}

func TestUnknownOrder(t *testing.T) {
	s, _ := newStore(t)
	ctx := t.Context()
	const id = "0199e7a4-9c3b-7d4e-8f00-000000000001"

	_, err := s.Get(ctx, orderID)
	checkNotFound(t, err, "orders: get "+id+": order not found")
	_, err = s.Transition(ctx, orderID, domain.Submitted, change(orders.SourceUser, "submit"))
	checkNotFound(t, err, `orders: transition `+id+` to "submitted": order not found`)
	_, err = s.Record(ctx, orderID, change(orders.SourceBroker, "status"))
	checkNotFound(t, err, "orders: record on "+id+": order not found")

	events, err := s.Events(ctx, orderID)
	if err != nil || len(events) != 0 {
		t.Errorf("Events() = %v, %v, want none and nil", events, err)
	}
}

func checkNotFound(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
	if !errors.Is(err, orders.ErrNotFound) {
		t.Errorf("errors.Is(%v, ErrNotFound) = false, want true", err)
	}
}

func TestOrderEventsIsAppendOnly(t *testing.T) {
	s, pool := newStore(t)
	ctx := t.Context()
	if _, _, err := s.Create(ctx, emaarBuy(orderID), userCreate); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(ctx, orderID, domain.Submitted, change(orders.SourceUser, "submit")); err != nil {
		t.Fatal(err)
	}
	before := eventsOf(t, s, orderID)

	tests := []struct {
		name    string
		sql     string
		wantErr string
	}{
		{
			name:    "update",
			sql:     "UPDATE order_events SET to_state = 'filled'",
			wantErr: "ERROR: order_events is append-only: UPDATE is not allowed (SQLSTATE 42501)",
		},
		{
			name:    "delete",
			sql:     "DELETE FROM order_events",
			wantErr: "ERROR: order_events is append-only: DELETE is not allowed (SQLSTATE 42501)",
		},
		{
			name:    "truncate",
			sql:     "TRUNCATE order_events",
			wantErr: "ERROR: order_events is append-only: TRUNCATE is not allowed (SQLSTATE 42501)",
		},
		{
			name:    "delete the order",
			sql:     "DELETE FROM orders",
			wantErr: `ERROR: update or delete on table "orders" violates foreign key constraint "order_events_client_order_id_fkey" on table "order_events" (SQLSTATE 23503)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, tt.sql)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Exec(%q) error = %v, want %q", tt.sql, err, tt.wantErr)
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Errorf("error %v is not a *pgconn.PgError", err)
			}
		})
	}

	if after := eventsOf(t, s, orderID); !reflect.DeepEqual(after, before) {
		t.Errorf("events after rejected writes = %+v, want %+v", after, before)
	}
	if _, err := s.Get(ctx, orderID); err != nil {
		t.Errorf("Get() after rejected writes error = %v", err)
	}
}
