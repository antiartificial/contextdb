package observe

import (
	"math"
	"sort"
)

// Calibrator transforms a raw confidence score into a calibrated probability.
type Calibrator interface {
	Calibrate(rawConfidence float64) float64
}

// PlattScaler performs Platt scaling (logistic regression) on confidence scores.
// After fitting, Calibrate(x) = 1 / (1 + exp(-(A*x + B))).
type PlattScaler struct {
	A      float64 // logistic slope
	B      float64 // logistic intercept
	Fitted bool
}

// Fit trains the Platt scaler from prediction outcomes.
// Uses gradient descent to find A and B that minimize log-loss.
// Requires at least minSamples predictions (default 50).
func (p *PlattScaler) Fit(predictions []PredictionOutcome, minSamples int) {
	if minSamples == 0 {
		minSamples = 50
	}
	if len(predictions) < minSamples {
		return // not enough data
	}

	// Simple gradient descent for logistic regression.
	// Loss = -sum(y*log(sigma) + (1-y)*log(1-sigma)) where sigma = 1/(1+exp(-(A*x+B)))
	p.A = 0.0
	p.B = 0.0
	lr := 0.01

	for iter := 0; iter < 1000; iter++ {
		var gradA, gradB float64
		for _, pred := range predictions {
			z := p.A*pred.Predicted + p.B
			sigma := 1.0 / (1.0 + math.Exp(-z))
			err := sigma - pred.Actual
			gradA += err * pred.Predicted
			gradB += err
		}
		n := float64(len(predictions))
		p.A -= lr * gradA / n
		p.B -= lr * gradB / n
	}
	p.Fitted = true
}

// Calibrate applies the learned logistic transformation.
// Returns rawConfidence unchanged if the scaler has not been fitted.
func (p *PlattScaler) Calibrate(rawConfidence float64) float64 {
	if !p.Fitted {
		return rawConfidence // identity if not fitted
	}
	z := p.A*rawConfidence + p.B
	return 1.0 / (1.0 + math.Exp(-z))
}

// IsotonicRegressor performs isotonic (monotone) regression for calibration.
// After fitting, Calibrate interpolates between the learned step function.
type IsotonicRegressor struct {
	// xs and ys are the fitted step function (sorted by xs).
	xs     []float64
	ys     []float64
	Fitted bool
}

// Fit trains the isotonic regressor using the Pool Adjacent Violators Algorithm (PAVA).
func (r *IsotonicRegressor) Fit(predictions []PredictionOutcome) {
	if len(predictions) < 2 {
		return
	}

	// Sort by predicted value.
	sorted := make([]PredictionOutcome, len(predictions))
	copy(sorted, predictions)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Predicted < sorted[j].Predicted
	})

	// Pool Adjacent Violators Algorithm.
	type block struct {
		sumY  float64
		count int
	}
	blocks := make([]block, len(sorted))
	for i, p := range sorted {
		blocks[i] = block{sumY: p.Actual, count: 1}
	}

	// Merge adjacent violating blocks.
	merged := true
	for merged {
		merged = false
		for i := 0; i < len(blocks)-1; i++ {
			meanI := blocks[i].sumY / float64(blocks[i].count)
			meanJ := blocks[i+1].sumY / float64(blocks[i+1].count)
			if meanI > meanJ {
				// Merge blocks i and i+1.
				blocks[i].sumY += blocks[i+1].sumY
				blocks[i].count += blocks[i+1].count
				blocks = append(blocks[:i+1], blocks[i+2:]...)
				merged = true
			}
		}
	}

	// Build the step function using the midpoint of each block's sorted range.
	r.xs = make([]float64, 0, len(blocks))
	r.ys = make([]float64, 0, len(blocks))
	idx := 0
	for _, b := range blocks {
		midIdx := idx + b.count/2
		if midIdx >= len(sorted) {
			midIdx = len(sorted) - 1
		}
		r.xs = append(r.xs, sorted[midIdx].Predicted)
		r.ys = append(r.ys, b.sumY/float64(b.count))
		idx += b.count
	}
	r.Fitted = true
}

// Calibrate applies isotonic interpolation.
// Returns rawConfidence unchanged if the regressor has not been fitted.
func (r *IsotonicRegressor) Calibrate(rawConfidence float64) float64 {
	if !r.Fitted || len(r.xs) == 0 {
		return rawConfidence
	}

	// Binary search for position in the step function.
	i := sort.SearchFloat64s(r.xs, rawConfidence)
	if i == 0 {
		return r.ys[0]
	}
	if i >= len(r.xs) {
		return r.ys[len(r.ys)-1]
	}

	// Linear interpolation between neighbouring knots.
	x0, x1 := r.xs[i-1], r.xs[i]
	y0, y1 := r.ys[i-1], r.ys[i]
	if x1 == x0 {
		return y0
	}
	t := (rawConfidence - x0) / (x1 - x0)
	return y0 + t*(y1-y0)
}
