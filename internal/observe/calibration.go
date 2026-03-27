package observe

import "math"

// PredictionOutcome pairs a predicted confidence with the actual truth outcome.
type PredictionOutcome struct {
	Predicted float64 // confidence at prediction time [0, 1]
	Actual    float64 // 1.0 if true, 0.0 if false
}

// BrierScore computes the Brier score for a set of predictions.
// Lower is better. Range [0, 1]. Perfect = 0.
func BrierScore(predictions []PredictionOutcome) float64 {
	if len(predictions) == 0 {
		return 0
	}
	var sum float64
	for _, p := range predictions {
		diff := p.Predicted - p.Actual
		sum += diff * diff
	}
	return sum / float64(len(predictions))
}

// ExpectedCalibrationError computes the ECE by binning predictions.
// Lower is better. Range [0, 1].
func ExpectedCalibrationError(predictions []PredictionOutcome, bins int) float64 {
	if len(predictions) == 0 || bins <= 0 {
		return 0
	}

	type bin struct {
		sumPredicted float64
		sumActual    float64
		count        int
	}

	buckets := make([]bin, bins)
	for _, p := range predictions {
		idx := int(p.Predicted * float64(bins))
		if idx >= bins {
			idx = bins - 1
		}
		buckets[idx].sumPredicted += p.Predicted
		buckets[idx].sumActual += p.Actual
		buckets[idx].count++
	}

	n := float64(len(predictions))
	var ece float64
	for _, b := range buckets {
		if b.count == 0 {
			continue
		}
		avgPred := b.sumPredicted / float64(b.count)
		avgActual := b.sumActual / float64(b.count)
		ece += (float64(b.count) / n) * math.Abs(avgPred-avgActual)
	}
	return ece
}

// MaxCalibrationError returns the worst-case calibration error across bins.
func MaxCalibrationError(predictions []PredictionOutcome, bins int) float64 {
	if len(predictions) == 0 || bins <= 0 {
		return 0
	}

	type bin struct {
		sumPredicted float64
		sumActual    float64
		count        int
	}

	buckets := make([]bin, bins)
	for _, p := range predictions {
		idx := int(p.Predicted * float64(bins))
		if idx >= bins {
			idx = bins - 1
		}
		buckets[idx].sumPredicted += p.Predicted
		buckets[idx].sumActual += p.Actual
		buckets[idx].count++
	}

	var mce float64
	for _, b := range buckets {
		if b.count == 0 {
			continue
		}
		avgPred := b.sumPredicted / float64(b.count)
		avgActual := b.sumActual / float64(b.count)
		gap := math.Abs(avgPred - avgActual)
		if gap > mce {
			mce = gap
		}
	}
	return mce
}
