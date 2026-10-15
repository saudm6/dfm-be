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
// the 15 legal transitions succeed and append an event, and each illegal one
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
		"working->part_filled", "working->filled", "working->cancelled", "working->rejected",
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
	_, err = s.Modify(ctx, orderID, decimal.RequireFromString("1"), decimal.Zero, change(orders.SourceUser, "modify"))
	checkNotFound(t, err, "orders: modify "+id+": order not found")

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

func TestWorkingOrderRejectedKeepsRawMessage(t *testing.T) {
	s, _ := newStore(t)
	ctx := t.Context()
	if _, _, err := s.Create(ctx, emaarBuy(orderID), userCreate); err != nil {
		t.Fatal(err)
	}
	for _, step := range []domain.OrderState{domain.Submitted, domain.Working} {
		if _, err := s.Transition(ctx, orderID, step, change(orders.SourceBroker, "status")); err != nil {
			t.Fatal(err)
		}
	}
	const reject = "Order rejected – Reason: price is outside the price band"
	got, err := s.Transition(ctx, orderID, domain.Rejected, orders.Change{
		Source: orders.SourceBroker,
		Action: "reject",
		Detail: json.RawMessage(`{"code":201}`),
		Raw:    reject,
	})
	if err != nil {
		t.Fatalf("Transition(working -> rejected) error = %v", err)
	}
	if got.State != domain.Rejected {
		t.Errorf("Transition() state = %q, want %q", got.State, domain.Rejected)
	}
	const raw = `{"z":1,"a":2,"a":3}`
	if _, err := s.Record(ctx, orderID, orders.Change{
		Source: orders.SourceBroker,
		Action: "status",
		Detail: json.RawMessage(raw),
		Raw:    raw,
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	events := eventsOf(t, s, orderID)
	sec := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Second) }
	wantTail := []orders.Event{
		{Source: "broker", Action: "reject", From: "working", To: "rejected", Detail: json.RawMessage(`{"code": 201}`), Raw: reject, RecordedAt: sec(3)},
		{Source: "broker", Action: "status", Detail: json.RawMessage(`{"a": 3, "z": 1}`), Raw: raw, RecordedAt: sec(4)},
	}
	if len(events) != 5 || !reflect.DeepEqual(events[3:], wantTail) {
		t.Errorf("Events() = %+v, want 5 events ending in %+v", events, wantTail)
	}
	for i, ev := range events[:3] {
		if ev.Raw != "" {
			t.Errorf("event %d Raw = %q, want empty", i, ev.Raw)
		}
	}
}

func TestOrdersRejectsIDsThatAreNotUUIDv7(t *testing.T) {
	_, pool := newStore(t)
	tests := []struct {
		name    string
		id      string
		wantErr string
	}{
		{name: "nil", id: "00000000-0000-0000-0000-000000000000",
			wantErr: `ERROR: new row for relation "orders" violates check constraint "orders_client_order_id_check" (SQLSTATE 23514)`},
		{name: "version 7 nibble with NCS variant", id: "0199e7a4-9c3b-7d4e-0f00-000000000001",
			wantErr: `ERROR: new row for relation "orders" violates check constraint "orders_client_order_id_check" (SQLSTATE 23514)`},
		{name: "UUIDv4", id: "6f1c2c9e-3b0a-4d7e-9f00-000000000001",
			wantErr: `ERROR: new row for relation "orders" violates check constraint "orders_client_order_id_check" (SQLSTATE 23514)`},
		{name: "UUIDv7", id: "0199e7a4-9c3b-7d4e-8f00-000000000001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), `INSERT INTO orders (client_order_id, symbol, side, quantity, limit_price, created_quantity, created_limit_price, state, created_at, updated_at)
				VALUES ($1, 'EMAAR', 'buy', 1000, 14.25, 1000, 14.25, 'draft', now(), now())`, tt.id)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("INSERT %s error = %v, want nil", tt.id, err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("INSERT %s error = %v, want %q", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestOrderIdentityIsImmutable(t *testing.T) {
	s, pool := newStore(t)
	ctx := t.Context()
	if _, _, err := s.Create(ctx, emaarBuy(orderID), userCreate); err != nil {
		t.Fatal(err)
	}
	const wantErr = "ERROR: orders terms are immutable: order 0199e7a4-9c3b-7d4e-8f00-000000000001 cannot change its id, symbol, side, or created terms (SQLSTATE 42501)"
	tests := []struct {
		name string
		set  string
	}{
		{name: "id", set: "client_order_id = '0199e7a4-9c3b-7d4e-8f00-000000000002'"},
		{name: "symbol", set: "symbol = 'DEWA'"},
		{name: "side", set: "side = 'sell'"},
		{name: "created quantity", set: "created_quantity = 1001"},
		{name: "created quantity scale", set: "created_quantity = 1000.00"},
		{name: "created limit price", set: "created_limit_price = 14.26"},
		{name: "created limit price to market", set: "created_limit_price = NULL"},
		{name: "side with state", set: "side = 'sell', state = 'submitted'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, "UPDATE orders SET "+tt.set)
			if err == nil || err.Error() != wantErr {
				t.Fatalf("UPDATE orders SET %s error = %v, want %q", tt.set, err, wantErr)
			}
		})
	}

	got, err := s.Transition(ctx, orderID, domain.Submitted, change(orders.SourceUser, "submit"))
	if err != nil {
		t.Fatalf("Transition() after rejected updates error = %v", err)
	}
	want := emaarBuy(orderID)
	want.State, want.CreatedAt, want.UpdatedAt = domain.Submitted, t0, t0.Add(time.Second)
	sameOrder(t, got, want)
}

func TestModifyWorkingOrder(t *testing.T) {
	s, _ := newStore(t)
	ctx := t.Context()
	if _, _, err := s.Create(ctx, emaarBuy(orderID), userCreate); err != nil {
		t.Fatal(err)
	}
	for _, step := range []domain.OrderState{domain.Submitted, domain.Working} {
		if _, err := s.Transition(ctx, orderID, step, change(orders.SourceBroker, "status")); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Modify(ctx, orderID, decimal.RequireFromString("1500"), decimal.RequireFromString("14.10"), change(orders.SourceUser, "modify"))
	if err != nil {
		t.Fatalf("Modify() error = %v", err)
	}

	want := emaarBuy(orderID)
	want.Quantity, want.LimitPrice = decimal.RequireFromString("1500"), decimal.RequireFromString("14.10")
	want.State, want.CreatedAt, want.UpdatedAt = domain.Working, t0, t0.Add(3*time.Second)
	sameOrder(t, got, want)
	stored, err := s.Get(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	sameOrder(t, stored, want)
	events := eventsOf(t, s, orderID)
	wantLast := orders.Event{
		Source:     "user",
		Action:     "modify",
		Detail:     json.RawMessage(`{"new": {"quantity": "1500", "limit_price": "14.1"}, "old": {"quantity": "1000", "limit_price": "14.25"}}`),
		RecordedAt: t0.Add(3 * time.Second),
	}
	if len(events) != 4 || !reflect.DeepEqual(events[3], wantLast) {
		t.Errorf("Events() = %+v, want 4 events ending in %+v", events, wantLast)
	}

	retry := emaarBuy(orderID)
	retry.Quantity = decimal.RequireFromString("1000.00")
	again, created, err := s.Create(ctx, retry, userCreate)
	if err != nil || created {
		t.Fatalf("retried Create() after Modify = created %v, error %v, want false and nil", created, err)
	}
	sameOrder(t, again, want)
	modified := emaarBuy(orderID)
	modified.Quantity, modified.LimitPrice = want.Quantity, want.LimitPrice
	if _, _, err := s.Create(ctx, modified, userCreate); !errors.Is(err, orders.ErrIDReused) {
		t.Errorf("Create() with the modified terms error = %v, want ErrIDReused", err)
	}
	if n := len(eventsOf(t, s, orderID)); n != 4 {
		t.Errorf("Events() after retried creates has %d rows, want 4", n)
	}
}

func TestModifyDetail(t *testing.T) {
	s, _ := newStore(t)
	ctx := t.Context()
	if _, _, err := s.Create(ctx, emaarBuy(orderID), userCreate); err != nil {
		t.Fatal(err)
	}

	_, err := s.Modify(ctx, orderID, decimal.RequireFromString("1000"), decimal.Zero, orders.Change{
		Source: orders.SourceUser,
		Action: "modify",
		Detail: json.RawMessage(`{"reason":"go to market"}`),
		Raw:    "make it a market order",
	})
	if err != nil {
		t.Fatalf("Modify() error = %v", err)
	}

	got, err := s.Get(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LimitPrice.IsZero() {
		t.Errorf("LimitPrice = %s, want 0 (market)", got.LimitPrice)
	}
	events := eventsOf(t, s, orderID)
	want := orders.Event{
		Source:     "user",
		Action:     "modify",
		Detail:     json.RawMessage(`{"new": {"quantity": "1000", "limit_price": null}, "old": {"quantity": "1000", "limit_price": "14.25"}, "detail": {"reason": "go to market"}}`),
		Raw:        "make it a market order",
		RecordedAt: t0.Add(time.Second),
	}
	if len(events) != 2 || !reflect.DeepEqual(events[1], want) {
		t.Errorf("Events() = %+v, want 2 events ending in %+v", events, want)
	}
}

// TestModifyByState modifies an order in each state: open orders change, and
// terminal ones fail and keep their terms and audit log.
func TestModifyByState(t *testing.T) {
	s, _ := newStore(t)
	ctx := t.Context()
	for _, state := range domain.OrderStates() {
		t.Run(string(state), func(t *testing.T) {
			id, err := orders.NewID()
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.Create(ctx, emaarBuy(id), userCreate); err != nil {
				t.Fatal(err)
			}
			for _, step := range pathTo[state] {
				if _, err := s.Transition(ctx, id, step, change(orders.SourceSystem, "setup")); err != nil {
					t.Fatalf("setup Transition(%q) error = %v", step, err)
				}
			}
			before, err := s.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			eventsBefore := len(eventsOf(t, s, id))

			got, err := s.Modify(ctx, id, decimal.RequireFromString("900"), decimal.RequireFromString("14.20"), change(orders.SourceUser, "modify"))

			if !state.Terminal() {
				if err != nil {
					t.Fatalf("Modify() error = %v", err)
				}
				if got.State != state || !got.Quantity.Equal(decimal.RequireFromString("900")) {
					t.Errorf("Modify() = %+v, want state %q and quantity 900", got, state)
				}
				if n := len(eventsOf(t, s, id)); n != eventsBefore+1 {
					t.Errorf("%d events after Modify, want %d", n, eventsBefore+1)
				}
				return
			}
			wantErr := fmt.Sprintf("orders: modify %s: order is not modifiable: state %q is terminal", id, state)
			if err == nil || err.Error() != wantErr {
				t.Fatalf("Modify() error = %v, want %q", err, wantErr)
			}
			if !errors.Is(err, orders.ErrNotModifiable) {
				t.Errorf("errors.Is(%v, ErrNotModifiable) = false, want true", err)
			}
			after, err := s.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			sameOrder(t, after, before)
			if n := len(eventsOf(t, s, id)); n != eventsBefore {
				t.Errorf("%d events after a rejected Modify, want %d", n, eventsBefore)
			}
		})
	}
}

func TestModifyValidates(t *testing.T) {
	s, _ := newStore(t)
	ctx := t.Context()
	if _, _, err := s.Create(ctx, emaarBuy(orderID), userCreate); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name            string
		quantity, limit string
		change          orders.Change
		wantErr         string
	}{
		{name: "zero quantity", quantity: "0", limit: "14.25", change: change(orders.SourceUser, "modify"),
			wantErr: "orders: invalid order: quantity 0 is not positive"},
		{name: "negative limit", quantity: "1000", limit: "-1", change: change(orders.SourceUser, "modify"),
			wantErr: "orders: invalid order: limit price -1 is negative"},
		{name: "action", quantity: "1000", limit: "14.25", change: orders.Change{Source: orders.SourceUser},
			wantErr: "orders: invalid change: action is missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Modify(ctx, orderID, decimal.RequireFromString(tt.quantity), decimal.RequireFromString(tt.limit), tt.change)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Modify() error = %v, want %q", err, tt.wantErr)
			}
			if !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("errors.Is(%v, ErrInvalid) = false, want true", err)
			}
		})
	}
	if n := len(eventsOf(t, s, orderID)); n != 1 {
		t.Errorf("Events() has %d rows after rejected modifies, want 1", n)
	}
}
