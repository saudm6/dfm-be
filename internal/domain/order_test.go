package domain_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/saudm6/dfm-be/internal/domain"
)

// legal is the transition table written out literally, so that a change to
// the table in order.go must also change this test.
var legal = map[[2]domain.OrderState]bool{
	{"draft", "submitted"}:         true,
	{"draft", "cancelled"}:         true,
	{"draft", "rejected"}:          true,
	{"submitted", "working"}:       true,
	{"submitted", "part_filled"}:   true,
	{"submitted", "filled"}:        true,
	{"submitted", "cancelled"}:     true,
	{"submitted", "rejected"}:      true,
	{"working", "part_filled"}:     true,
	{"working", "filled"}:          true,
	{"working", "cancelled"}:       true,
	{"part_filled", "part_filled"}: true,
	{"part_filled", "filled"}:      true,
	{"part_filled", "cancelled"}:   true,
}

func TestOrderStates(t *testing.T) {
	want := []domain.OrderState{"draft", "submitted", "working", "part_filled", "filled", "cancelled", "rejected"}
	if got := domain.OrderStates(); !slices.Equal(got, want) {
		t.Errorf("OrderStates() = %q, want %q", got, want)
	}
}

func TestTransitionTable(t *testing.T) {
	var legalSeen, illegalSeen int
	for _, from := range domain.OrderStates() {
		for _, to := range domain.OrderStates() {
			t.Run(fmt.Sprintf("%s to %s", from, to), func(t *testing.T) {
				got, err := from.Transition(to)
				if legal[[2]domain.OrderState{from, to}] {
					legalSeen++
					if err != nil {
						t.Fatalf("Transition() error = %v, want nil", err)
					}
					if got != to {
						t.Errorf("Transition() = %q, want %q", got, to)
					}
					if !from.CanTransition(to) {
						t.Errorf("CanTransition() = false, want true")
					}
					return
				}
				illegalSeen++
				wantErr := fmt.Sprintf("domain: illegal order transition: %q -> %q", from, to)
				if err == nil || err.Error() != wantErr {
					t.Fatalf("Transition() error = %v, want %q", err, wantErr)
				}
				if !errors.Is(err, domain.ErrIllegalTransition) {
					t.Errorf("errors.Is(%v, ErrIllegalTransition) = false, want true", err)
				}
				if got != "" {
					t.Errorf("Transition() = %q, want empty", got)
				}
				if from.CanTransition(to) {
					t.Errorf("CanTransition() = true, want false")
				}
			})
		}
	}
	if legalSeen != 14 || illegalSeen != 35 {
		t.Errorf("walked %d legal and %d illegal transitions, want 14 and 35", legalSeen, illegalSeen)
	}
}

func TestTransitionRejectsUnknownStates(t *testing.T) {
	tests := []struct {
		from, to domain.OrderState
		wantErr  string
	}{
		{from: "", to: "draft", wantErr: `domain: illegal order transition: "" -> "draft"`},
		{from: "draft", to: "", wantErr: `domain: illegal order transition: "draft" -> ""`},
		{from: "draft", to: "Submitted", wantErr: `domain: illegal order transition: "draft" -> "Submitted"`},
		{from: "pending", to: "working", wantErr: `domain: illegal order transition: "pending" -> "working"`},
		{from: "working", to: "expired", wantErr: `domain: illegal order transition: "working" -> "expired"`},
	}
	for _, tt := range tests {
		t.Run(tt.wantErr, func(t *testing.T) {
			_, err := tt.from.Transition(tt.to)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Transition() error = %v, want %q", err, tt.wantErr)
			}
			if !errors.Is(err, domain.ErrIllegalTransition) {
				t.Errorf("errors.Is(%v, ErrIllegalTransition) = false, want true", err)
			}
		})
	}
}

func TestOrderStateTerminal(t *testing.T) {
	tests := []struct {
		s    domain.OrderState
		want bool
	}{
		{s: "draft", want: false},
		{s: "submitted", want: false},
		{s: "working", want: false},
		{s: "part_filled", want: false},
		{s: "filled", want: true},
		{s: "cancelled", want: true},
		{s: "rejected", want: true},
		{s: "unknown", want: false},
	}
	for _, tt := range tests {
		if got := tt.s.Terminal(); got != tt.want {
			t.Errorf("%q.Terminal() = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestParseOrderEnums(t *testing.T) {
	tests := []struct {
		name    string
		parse   func(string) (string, error)
		in      string
		want    string
		wantErr string
	}{
		{name: "state draft", parse: wrap(domain.ParseOrderState), in: "draft", want: "draft"},
		{name: "state submitted", parse: wrap(domain.ParseOrderState), in: "submitted", want: "submitted"},
		{name: "state working", parse: wrap(domain.ParseOrderState), in: "working", want: "working"},
		{name: "state part_filled", parse: wrap(domain.ParseOrderState), in: "part_filled", want: "part_filled"},
		{name: "state filled", parse: wrap(domain.ParseOrderState), in: "filled", want: "filled"},
		{name: "state cancelled", parse: wrap(domain.ParseOrderState), in: "cancelled", want: "cancelled"},
		{name: "state rejected", parse: wrap(domain.ParseOrderState), in: "rejected", want: "rejected"},
		{name: "state unknown", parse: wrap(domain.ParseOrderState), in: "canceled", wantErr: `domain: invalid order state: unknown value "canceled"`},
		{name: "state empty", parse: wrap(domain.ParseOrderState), in: "", wantErr: `domain: invalid order state: unknown value ""`},
		{name: "side buy", parse: wrap(domain.ParseSide), in: "buy", want: "buy"},
		{name: "side sell", parse: wrap(domain.ParseSide), in: "sell", want: "sell"},
		{name: "side unknown", parse: wrap(domain.ParseSide), in: "BUY", wantErr: `domain: invalid side: unknown value "BUY"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.parse(tt.in)
			checkErr(t, err, tt.wantErr)
			if got != tt.want {
				t.Errorf("parse(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestOrderEnumJSON(t *testing.T) {
	type order struct {
		State domain.OrderState
		Side  domain.Side
	}
	const valid = `{"State":"part_filled","Side":"sell"}`
	want := order{State: domain.PartFilled, Side: domain.Sell}

	var got order
	if err := json.Unmarshal([]byte(valid), &got); err != nil {
		t.Fatalf("Unmarshal(valid) error = %v", err)
	}
	if got != want {
		t.Errorf("Unmarshal(valid) = %+v, want %+v", got, want)
	}
	out, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if string(out) != valid {
		t.Errorf("Marshal = %s, want %s", out, valid)
	}

	for _, in := range []string{`{"State":"open"}`, `{"Side":"short"}`} {
		var v order
		if err := json.Unmarshal([]byte(in), &v); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Unmarshal(%s) error = %v, want ErrInvalid", in, err)
		}
	}
}
