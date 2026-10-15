// Package bus is the in-process event bus. Pollers, streams, the alert engine,
// P&L, and the WebSocket gateway publish and subscribe to typed topics without
// knowing about each other.
//
// Publish never blocks. Each subscription has its own bounded buffer. When a
// subscriber falls behind and its buffer is full, the bus drops that
// subscriber's oldest buffered event to make room for the new one and counts
// the drop. A slow subscriber therefore loses old events but always sees the
// latest, and never slows the publisher or other subscribers. Each subscriber
// receives events in the order they were published, minus any dropped.
package bus

import (
	"sync"
	"sync/atomic"
)

// Topic names a stream of events of type T. The topics are the package-level
// variables in topics.go.
type Topic[T any] struct {
	name string
}

// Name returns the topic's name, for logs and metrics.
func (t *Topic[T]) Name() string { return t.name }

// Bus routes published events to the subscribers of their topic. The zero
// value is not usable; call New. A Bus is safe for concurrent use.
type Bus struct {
	mu   sync.RWMutex
	subs map[any][]any
}

// New returns an empty Bus.
func New() *Bus {
	return &Bus{subs: make(map[any][]any)}
}

// Subscription receives the events of one topic. It is safe for concurrent
// use.
type Subscription[T any] struct {
	bus     *Bus
	topic   *Topic[T]
	ch      chan T
	mu      sync.Mutex
	closed  bool
	dropped atomic.Uint64
}

// Subscribe returns a subscription to t whose buffer holds up to buffer
// events. It panics if buffer is less than 1.
func Subscribe[T any](b *Bus, t *Topic[T], buffer int) *Subscription[T] {
	if buffer < 1 {
		panic("bus: subscription buffer must be at least 1")
	}
	s := &Subscription[T]{bus: b, topic: t, ch: make(chan T, buffer)}
	b.mu.Lock()
	defer b.mu.Unlock()
	// Copy on write, so Publish can range over a snapshot without holding b.mu.
	cur := b.subs[t]
	next := make([]any, len(cur), len(cur)+1)
	copy(next, cur)
	b.subs[t] = append(next, s)
	return s
}

// Publish delivers v to every current subscriber of t. It never blocks.
func Publish[T any](b *Bus, t *Topic[T], v T) {
	b.mu.RLock()
	subs := b.subs[t]
	b.mu.RUnlock()
	for _, c := range subs {
		c.(*Subscription[T]).deliver(v)
	}
}

func (s *Subscription[T]) deliver(v T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	// s.mu serialises publishers, and the receiver only ever empties the
	// buffer, so after one drop the send below always succeeds.
	for {
		select {
		case s.ch <- v:
			return
		default:
		}
		select {
		case <-s.ch:
			s.dropped.Add(1)
		default:
		}
	}
}

// C returns the channel that events arrive on. Close closes it.
func (s *Subscription[T]) C() <-chan T { return s.ch }

// Dropped returns how many events the bus has dropped for this subscriber
// because its buffer was full.
func (s *Subscription[T]) Dropped() uint64 { return s.dropped.Load() }

// Close unsubscribes and closes C. Events still buffered can be drained from C.
// Close is idempotent.
func (s *Subscription[T]) Close() {
	b := s.bus
	b.mu.Lock()
	cur := b.subs[s.topic]
	next := make([]any, 0, len(cur))
	for _, c := range cur {
		if c != any(s) {
			next = append(next, c)
		}
	}
	if len(next) == 0 {
		delete(b.subs, s.topic)
	} else {
		b.subs[s.topic] = next
	}
	b.mu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}
