// Package ta computes technical studies over bars. Charts and alerts both call
// it, so each study has exactly one implementation.
//
// Every study is a pure function of a []domain.Bar in time order and returns
// a Series of the same length, where element i is the study's value at bar i.
// Values are float64: studies divide, take square roots, and smooth
// recursively, so exact decimal arithmetic buys nothing a chart or an alert
// threshold can use.
//
// # Warm-up
//
// A study needs a number of bars before its first value. Those leading
// elements are empty (Valid is false). A series too short to finish warming
// up is empty throughout; that is not an error.
//
// # No-trade bars
//
// A bar whose Open, High, Low, and Close are all zero records a period with no
// trades (see domain.Bar). Every study skips such bars: it computes over the
// traded bars alone, as if the no-trade bars were not there, and leaves the
// element at each no-trade bar empty. So a no-trade bar never counts toward a
// window or a warm-up, ATR's previous close is the close of the last traded
// bar, and the element at every traded bar equals what the study would return
// for the same bars with the no-trade bars removed. Carrying the previous
// close forward instead would invent flat days that drag averages toward
// stale prices and pull RSI toward 50 on illiquid DFM names.
//
// # Parameters
//
// A period below 1, or another impossible parameter, returns an error that
// wraps domain.ErrInvalid.
package ta
