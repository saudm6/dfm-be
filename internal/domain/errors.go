package domain

import (
	"errors"
	"fmt"
)

// ErrInvalid is wrapped by every validation error in this package.
var ErrInvalid = errors.New("invalid")

func invalidf(kind, format string, args ...any) error {
	return fmt.Errorf("domain: %w %s: %s", ErrInvalid, kind, fmt.Sprintf(format, args...))
}
