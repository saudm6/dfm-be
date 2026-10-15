// Package analytics computes the statistics behind correlation, hedging, and
// pairs trading: log returns, a Pearson correlation matrix, OLS beta with R
// squared, the hedge ratio, difference and ratio spreads, rolling mean and
// standard deviation, the z-score, and the mean-reversion half-life.
//
// The package is pure: it reads []domain.Bar and plain float64 slices and
// does no I/O. Prices are converted from decimal to float64 once, in Closes;
// every statistic is computed in float64, which carries far more precision
// than any correlation or regression estimate needs.
//
// A typical pair pipeline is Closes for each instrument, Align to the dates
// on which both traded, then LogReturns, Regress, HedgeRatio, the spreads,
// ZScore, and HalfLife over the aligned prices.
//
// No-trade days: a bar whose open, high, low, and close are all zero records
// a period with no trades. Closes drops such bars, so no return is ever
// computed against a zero price; the next traded day's return is measured
// from the last traded close.
package analytics
