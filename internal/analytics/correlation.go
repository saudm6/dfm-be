package analytics

import (
	"fmt"

	"gonum.org/v1/gonum/mat"
	"gonum.org/v1/gonum/stat"
)

// CorrelationMatrix returns the Pearson correlation of every pair of series.
// Element [i][j] is the correlation of series i with series j. The series
// must have equal lengths of at least two, and none may be constant.
func CorrelationMatrix(series ...[]float64) ([][]float64, error) {
	if len(series) == 0 {
		return [][]float64{}, nil
	}
	n := len(series[0])
	for k, s := range series {
		if len(s) != n {
			return nil, fmt.Errorf("%w: series 0 has %d values, series %d has %d", ErrLengthMismatch, n, k, len(s))
		}
	}
	if n < 2 {
		return nil, fmt.Errorf("%w: correlation needs 2 observations, got %d", ErrInsufficientData, n)
	}
	for k, s := range series {
		if stat.Variance(s, nil) == 0 {
			return nil, fmt.Errorf("%w: series %d", ErrZeroVariance, k)
		}
	}
	data := mat.NewDense(n, len(series), nil)
	for k, s := range series {
		data.SetCol(k, s)
	}
	var corr mat.SymDense
	stat.CorrelationMatrix(&corr, data, nil)
	out := make([][]float64, len(series))
	for i := range out {
		out[i] = make([]float64, len(series))
		for j := range out[i] {
			out[i][j] = corr.At(i, j)
		}
	}
	return out, nil
}
