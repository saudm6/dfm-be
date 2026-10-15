package analytics

import "errors"

// Errors returned by this package. Each returned error wraps one of them, so
// callers can map them with errors.Is.
var (
	// ErrUnordered means bars or points are not in strictly ascending time
	// order.
	ErrUnordered = errors.New("analytics: series is not in ascending time order")
	// ErrMixedSymbols means one bar series holds more than one symbol.
	ErrMixedSymbols = errors.New("analytics: bars belong to more than one symbol")
	// ErrNonPositivePrice means a price that must be positive is zero or
	// negative.
	ErrNonPositivePrice = errors.New("analytics: price is not positive")
	// ErrLengthMismatch means series that must be paired have different
	// lengths.
	ErrLengthMismatch = errors.New("analytics: series lengths differ")
	// ErrInsufficientData means a series is too short for the statistic or
	// window asked for.
	ErrInsufficientData = errors.New("analytics: not enough observations")
	// ErrBadWindow means a rolling window is below the minimum the statistic
	// accepts.
	ErrBadWindow = errors.New("analytics: window size is out of range")
	// ErrZeroVariance means a series is constant, so the statistic is
	// undefined.
	ErrZeroVariance = errors.New("analytics: series has zero variance")
	// ErrNotMeanReverting means the spread shows no pull back to its mean,
	// so it has no half-life.
	ErrNotMeanReverting = errors.New("analytics: series is not mean-reverting")
)
