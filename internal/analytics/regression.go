package analytics

import (
	"fmt"

	"gonum.org/v1/gonum/mat"
	"gonum.org/v1/gonum/stat"
)

// Regression is the ordinary least squares fit y = Alpha + Beta*x over N
// observations.
type Regression struct {
	Alpha    float64
	Beta     float64
	RSquared float64
	N        int
}

// Regress fits y = alpha + beta*x by ordinary least squares. Over log
// returns, Beta is the sensitivity of y to x. y and x must have equal lengths
// of at least three, and neither may be constant.
func Regress(y, x []float64) (Regression, error) {
	n := len(y)
	if len(x) != n {
		return Regression{}, fmt.Errorf("%w: y has %d values, x has %d", ErrLengthMismatch, n, len(x))
	}
	if n < 3 {
		return Regression{}, fmt.Errorf("%w: regression needs 3 observations, got %d", ErrInsufficientData, n)
	}
	if stat.Variance(x, nil) == 0 {
		return Regression{}, fmt.Errorf("%w: regressor x", ErrZeroVariance)
	}
	if stat.Variance(y, nil) == 0 {
		return Regression{}, fmt.Errorf("%w: regressand y", ErrZeroVariance)
	}
	design := mat.NewDense(n, 2, nil)
	for i, v := range x {
		design.Set(i, 0, 1)
		design.Set(i, 1, v)
	}
	var coef mat.VecDense
	if err := coef.SolveVec(design, mat.NewVecDense(n, append([]float64(nil), y...))); err != nil {
		return Regression{}, fmt.Errorf("analytics: least squares: %w", err)
	}
	alpha, beta := coef.AtVec(0), coef.AtVec(1)
	mean := stat.Mean(y, nil)
	var ssRes, ssTot float64
	for i, v := range y {
		r := v - (alpha + beta*x[i])
		ssRes += r * r
		ssTot += (v - mean) * (v - mean)
	}
	return Regression{Alpha: alpha, Beta: beta, RSquared: 1 - ssRes/ssTot, N: n}, nil
}

// HedgeRatio returns the units of x to sell against one unit of y: the OLS
// slope of y's prices on x's prices, fitted with an intercept.
func HedgeRatio(y, x []float64) (float64, error) {
	r, err := Regress(y, x)
	if err != nil {
		return 0, err
	}
	return r.Beta, nil
}
