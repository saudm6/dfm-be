package domain

import (
	"errors"
	"fmt"
)

// OrderState is where an order is in its life. Change it only with
// OrderState.Transition, which applies the transition table.
type OrderState string

// Order states. Filled, Cancelled, and Rejected are terminal.
const (
	Draft      OrderState = "draft"
	Submitted  OrderState = "submitted"
	Working    OrderState = "working"
	PartFilled OrderState = "part_filled"
	Filled     OrderState = "filled"
	Cancelled  OrderState = "cancelled"
	Rejected   OrderState = "rejected"
)

// ErrIllegalTransition is wrapped by every error OrderState.Transition returns.
var ErrIllegalTransition = errors.New("illegal order transition")

// orderTransitions is the transition table: the states each state may move to.
// A state missing from the table is terminal. PartFilled may move to itself
// because each further partial fill is a transition. Rejected is reachable
// only before the broker accepts the order; a working order that ends without
// filling is Cancelled.
var orderTransitions = map[OrderState][]OrderState{
	Draft:      {Submitted, Cancelled, Rejected},
	Submitted:  {Working, PartFilled, Filled, Cancelled, Rejected},
	Working:    {PartFilled, Filled, Cancelled},
	PartFilled: {PartFilled, Filled, Cancelled},
}

// OrderStates returns every order state, in life-cycle order.
func OrderStates() []OrderState {
	return []OrderState{Draft, Submitted, Working, PartFilled, Filled, Cancelled, Rejected}
}

// ParseOrderState returns the OrderState named s.
func ParseOrderState(s string) (OrderState, error) { return parseEnum[OrderState]("order state", s) }

// Valid reports whether s is a known order state.
func (s OrderState) Valid() bool {
	switch s {
	case Draft, Submitted, Working, PartFilled, Filled, Cancelled, Rejected:
		return true
	}
	return false
}

// Terminal reports whether s is a known state that no transition leaves.
func (s OrderState) Terminal() bool {
	return s.Valid() && len(orderTransitions[s]) == 0
}

// CanTransition reports whether the transition table allows s to move to to.
func (s OrderState) CanTransition(to OrderState) bool {
	for _, next := range orderTransitions[s] {
		if next == to {
			return true
		}
	}
	return false
}

// Transition returns to if the transition table allows s to move to it, and
// otherwise an error wrapping ErrIllegalTransition.
func (s OrderState) Transition(to OrderState) (OrderState, error) {
	if !s.CanTransition(to) {
		return "", fmt.Errorf("domain: %w: %q -> %q", ErrIllegalTransition, s, to)
	}
	return to, nil
}

// MarshalText returns s as text.
func (s OrderState) MarshalText() ([]byte, error) { return []byte(s), nil }

// UnmarshalText sets s to ParseOrderState(text), so decoding rejects unknown values.
func (s *OrderState) UnmarshalText(text []byte) error {
	return unmarshalText(s, ParseOrderState, text)
}

// Side says whether an order buys or sells.
type Side string

// Order sides.
const (
	Buy  Side = "buy"
	Sell Side = "sell"
)

// ParseSide returns the Side named s.
func ParseSide(s string) (Side, error) { return parseEnum[Side]("side", s) }

// Valid reports whether s is a known side.
func (s Side) Valid() bool {
	switch s {
	case Buy, Sell:
		return true
	}
	return false
}

// MarshalText returns s as text.
func (s Side) MarshalText() ([]byte, error) { return []byte(s), nil }

// UnmarshalText sets s to ParseSide(text), so decoding rejects unknown values.
func (s *Side) UnmarshalText(text []byte) error { return unmarshalText(s, ParseSide, text) }
