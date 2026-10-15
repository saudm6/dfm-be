package bus_test

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/bus"
	"github.com/saudm6/dfm-be/internal/domain"
)

// quote returns an EMAAR quote whose Volume is seq, so tests can tell events apart.
func quote(seq int64) bus.QuoteUpdatedEvent {
	return bus.QuoteUpdatedEvent{Quote: domain.Quote{
		Symbol:    "EMAAR",
		Last:      decimal.RequireFromString("14.25"),
		Volume:    seq,
		Trades:    1,
		AsOf:      time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC),
		Source:    domain.SourceDFMMarketWatch,
		Freshness: domain.Delayed,
	}}
}

// drain returns the Volume of every event buffered in s, without waiting.
func drain(s *bus.Subscription[bus.QuoteUpdatedEvent]) []int64 {
	got := []int64{}
	for {
		select {
		case e := <-s.C():
			got = append(got, e.Quote.Volume)
		default:
			return got
		}
	}
}

func TestFanOut(t *testing.T) {
	b := bus.New()
	subs := []*bus.Subscription[bus.QuoteUpdatedEvent]{
		bus.Subscribe(b, bus.QuoteUpdated, 8),
		bus.Subscribe(b, bus.QuoteUpdated, 8),
		bus.Subscribe(b, bus.QuoteUpdated, 8),
	}
	other := bus.Subscribe(b, bus.OrderChanged, 8)

	for seq := int64(1); seq <= 3; seq++ {
		bus.Publish(b, bus.QuoteUpdated, quote(seq))
	}

	for i, s := range subs {
		if got, want := drain(s), []int64{1, 2, 3}; !reflect.DeepEqual(got, want) {
			t.Errorf("subscriber %d got %v, want %v", i, got, want)
		}
		if got := s.Dropped(); got != 0 {
			t.Errorf("subscriber %d dropped %d, want 0", i, got)
		}
	}
	select {
	case e := <-other.C():
		t.Errorf("OrderChanged subscriber got %+v from a QuoteUpdated publish", e)
	default:
	}
}

func TestEventCarriesPayload(t *testing.T) {
	b := bus.New()
	s := bus.Subscribe(b, bus.SourceHealthChanged, 1)
	want := bus.SourceHealthChangedEvent{
		Source:    domain.SourceIBKR,
		Healthy:   false,
		Freshness: domain.Delayed,
		Detail:    "farm connection lost",
		At:        time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC),
	}
	bus.Publish(b, bus.SourceHealthChanged, want)
	if got := <-s.C(); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestOrderChangedCarriesOrderState(t *testing.T) {
	b := bus.New()
	s := bus.Subscribe(b, bus.OrderChanged, 1)
	want := bus.OrderChangedEvent{
		OrderID: "0199e7a4-9c3b-7d4e-8f00-000000000001",
		Symbol:  "EMAAR",
		State:   domain.PartFilled,
		At:      time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC),
	}
	bus.Publish(b, bus.OrderChanged, want)
	if got := <-s.C(); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestInOrderDeliveryWhileConsuming(t *testing.T) {
	const n = 10000
	tests := []struct {
		name   string
		buffer int
	}{
		{"buffer holds every event", n},
		{"small buffer", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := bus.New()
			s := bus.Subscribe(b, bus.QuoteUpdated, tt.buffer)
			received := make(chan []int64)
			go func() {
				var got []int64
				for e := range s.C() {
					got = append(got, e.Quote.Volume)
				}
				received <- got
			}()
			for seq := int64(1); seq <= n; seq++ {
				bus.Publish(b, bus.QuoteUpdated, quote(seq))
			}
			s.Close()
			got := <-received

			for i := 1; i < len(got); i++ {
				if got[i] <= got[i-1] {
					t.Fatalf("event %d arrived after %d", got[i], got[i-1])
				}
			}
			if total := uint64(len(got)) + s.Dropped(); total != n {
				t.Errorf("received %d + dropped %d = %d, want %d", len(got), s.Dropped(), total, n)
			}
			if got[len(got)-1] != n {
				t.Errorf("last event received is %d, want %d", got[len(got)-1], n)
			}
			if tt.buffer == n && s.Dropped() != 0 {
				t.Errorf("dropped %d with a buffer that holds every event, want 0", s.Dropped())
			}
		})
	}
}

func TestSlowSubscriberDropsOldest(t *testing.T) {
	b := bus.New()
	slow := bus.Subscribe(b, bus.QuoteUpdated, 2)
	fast := bus.Subscribe(b, bus.QuoteUpdated, 5)

	done := make(chan struct{})
	go func() {
		for seq := int64(1); seq <= 5; seq++ {
			bus.Publish(b, bus.QuoteUpdated, quote(seq))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}

	if got := slow.Dropped(); got != 3 {
		t.Errorf("slow subscriber dropped %d, want 3", got)
	}
	if got, want := drain(slow), []int64{4, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("slow subscriber got %v, want %v", got, want)
	}
	if got := fast.Dropped(); got != 0 {
		t.Errorf("fast subscriber dropped %d, want 0", got)
	}
	if got, want := drain(fast), []int64{1, 2, 3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("fast subscriber got %v, want %v", got, want)
	}
}

func TestClose(t *testing.T) {
	b := bus.New()
	closing := bus.Subscribe(b, bus.QuoteUpdated, 4)
	staying := bus.Subscribe(b, bus.QuoteUpdated, 4)

	bus.Publish(b, bus.QuoteUpdated, quote(1))
	closing.Close()
	closing.Close()
	bus.Publish(b, bus.QuoteUpdated, quote(2))

	var got []int64
	for e := range closing.C() {
		got = append(got, e.Quote.Volume)
	}
	if want := []int64{1}; !reflect.DeepEqual(got, want) {
		t.Errorf("closed subscriber got %v, want %v", got, want)
	}
	if got, want := drain(staying), []int64{1, 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("remaining subscriber got %v, want %v", got, want)
	}
}

func TestPublishWithoutSubscribers(t *testing.T) {
	b := bus.New()
	bus.Publish(b, bus.AlertTriggered, bus.AlertTriggeredEvent{AlertID: "a1"})
	s := bus.Subscribe(b, bus.AlertTriggered, 1)
	select {
	case e := <-s.C():
		t.Errorf("late subscriber got %+v published before it subscribed", e)
	default:
	}
}

func TestConcurrentUse(t *testing.T) {
	b := bus.New()
	watcher := bus.Subscribe(b, bus.QuoteUpdated, 16)
	received := make(chan uint64)
	go func() {
		var n uint64
		for range watcher.C() {
			n++
		}
		received <- n
	}()

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for seq := int64(1); seq <= 2000; seq++ {
				bus.Publish(b, bus.QuoteUpdated, quote(seq))
			}
		})
	}
	for range 4 {
		wg.Go(func() {
			for range 200 {
				s := bus.Subscribe(b, bus.QuoteUpdated, 2)
				select {
				case <-s.C():
				default:
				}
				s.Close()
				for range s.C() {
				}
			}
		})
	}
	wg.Wait()
	watcher.Close()
	if total := <-received + watcher.Dropped(); total != 8000 {
		t.Errorf("watcher received + dropped = %d, want 8000", total)
	}
}

func TestSubscribePanicsOnEmptyBuffer(t *testing.T) {
	defer func() {
		if r := recover(); r != "bus: subscription buffer must be at least 1" {
			t.Errorf("recover() = %v, want the buffer panic", r)
		}
	}()
	bus.Subscribe(bus.New(), bus.QuoteUpdated, 0)
}

func TestTopicNames(t *testing.T) {
	tests := []struct{ got, want string }{
		{bus.QuoteUpdated.Name(), "quote.updated"},
		{bus.DisclosurePublished.Name(), "disclosure.published"},
		{bus.OrderChanged.Name(), "order.changed"},
		{bus.PositionChanged.Name(), "position.changed"},
		{bus.AlertTriggered.Name(), "alert.triggered"},
		{bus.SourceHealthChanged.Name(), "source.health.changed"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("topic name %q, want %q", tt.got, tt.want)
		}
	}
}
