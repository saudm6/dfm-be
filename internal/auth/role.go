package auth

import (
	"fmt"

	"github.com/saudm6/dfm-be/internal/domain"
)

// Role is what an account may do.
type Role string

// The roles. Owner is the one person whose broker and market data the app
// uses; members can chat and keep layouts.
const (
	Owner  Role = "owner"
	Member Role = "member"
)

// ParseRole returns the Role named s.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !r.Valid() {
		return "", fmt.Errorf("%w role: unknown value %q", domain.ErrInvalid, s)
	}
	return r, nil
}

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	return r == Owner || r == Member
}

// MarketData reports whether r may see quotes, history, orders, and
// positions.
func (r Role) MarketData() bool {
	return r == Owner
}

// MarshalText returns r as text.
func (r Role) MarshalText() ([]byte, error) { return []byte(r), nil }

// UnmarshalText sets r to ParseRole(text), so decoding rejects unknown values.
func (r *Role) UnmarshalText(text []byte) error { return r.Bind(string(text)) }

// Bind sets r to ParseRole(src). The generated HTTP server calls it for
// query parameters.
func (r *Role) Bind(src string) error {
	v, err := ParseRole(src)
	if err != nil {
		return err
	}
	*r = v
	return nil
}
