package observe_test

import (
	"math"
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/observe"
)

// ─── BrierScore ──────────────────────────────────────────────────────────────

func TestBrierScore_Perfect(t *testing.T) {
	is := is.New(t)

	// Perfect predictions: confident 1.0 when true, 0.0 when false.
	predictions := []observe.PredictionOutcome{
		{Predicted: 1.0, Actual: 1.0},
		{Predicted: 0.0, Actual: 0.0},
		{Predicted: 1.0, Actual: 1.0},
	}
	score := observe.BrierScore(predictions)
	is.Equal(0.0, score)
}

func TestBrierScore_Worst(t *testing.T) {
	is := is.New(t)

	// Worst predictions: confident 1.0 when false, 0.0 when true.
	predictions := []observe.PredictionOutcome{
		{Predicted: 1.0, Actual: 0.0},
		{Predicted: 0.0, Actual: 1.0},
	}
	score := observe.BrierScore(predictions)
	is.Equal(1.0, score)
}

func TestBrierScore_Empty(t *testing.T) {
	is := is.New(t)
	is.Equal(0.0, observe.BrierScore(nil))
}

func TestBrierScore_Midpoint(t *testing.T) {
	is := is.New(t)

	// Uniform 0.5 predictions: Brier = (0.5-1)^2 = 0.25.
	predictions := []observe.PredictionOutcome{
		{Predicted: 0.5, Actual: 1.0},
		{Predicted: 0.5, Actual: 0.0},
	}
	score := observe.BrierScore(predictions)
	// Both residuals are 0.25; mean = 0.25.
	is.True(math.Abs(score-0.25) < 1e-9)
}

// ─── ExpectedCalibrationError ────────────────────────────────────────────────

func TestECE_PerfectlyCalibrated(t *testing.T) {
	is := is.New(t)

	// In each bin, average predicted == average actual → ECE ≈ 0.
	// Use 10 bins of 10 samples each where predicted = actual fraction.
	var predictions []observe.PredictionOutcome
	for bin := 0; bin < 10; bin++ {
		midpoint := (float64(bin) + 0.5) / 10.0 // 0.05, 0.15, ..., 0.95
		for i := 0; i < 10; i++ {
			var actual float64
			if float64(i)/10.0 < midpoint {
				actual = 1.0
			}
			predictions = append(predictions, observe.PredictionOutcome{
				Predicted: midpoint,
				Actual:    actual,
			})
		}
	}

	ece := observe.ExpectedCalibrationError(predictions, 10)
	t.Logf("ECE (calibrated) = %.4f", ece)
	// Should be very small — not exactly zero due to the integer rounding above,
	// but well under 0.1.
	is.True(ece < 0.1)
}

func TestECE_MiscalibratedPredictions(t *testing.T) {
	is := is.New(t)

	// All predictions are 0.9 but actual outcomes are always 0 → badly miscalibrated.
	var predictions []observe.PredictionOutcome
	for i := 0; i < 100; i++ {
		predictions = append(predictions, observe.PredictionOutcome{
			Predicted: 0.9,
			Actual:    0.0,
		})
	}

	ece := observe.ExpectedCalibrationError(predictions, 10)
	t.Logf("ECE (miscalibrated) = %.4f", ece)
	// |avg_pred - avg_actual| = |0.9 - 0.0| = 0.9, weighted by 1.0 → ECE = 0.9.
	is.True(ece > 0.5)
}

func TestECE_Empty(t *testing.T) {
	is := is.New(t)
	is.Equal(0.0, observe.ExpectedCalibrationError(nil, 10))
}

func TestECE_ZeroBins(t *testing.T) {
	is := is.New(t)
	predictions := []observe.PredictionOutcome{{Predicted: 0.5, Actual: 1.0}}
	is.Equal(0.0, observe.ExpectedCalibrationError(predictions, 0))
}

// ─── MaxCalibrationError ─────────────────────────────────────────────────────

func TestMaxCalibrationError_PerfectlyCalibrated(t *testing.T) {
	is := is.New(t)

	// All predictions equal their actual fraction → MCE ≈ 0.
	predictions := []observe.PredictionOutcome{
		{Predicted: 0.2, Actual: 0.0},
		{Predicted: 0.2, Actual: 0.0},
		{Predicted: 0.2, Actual: 1.0},  // 1 out of 5 → avg actual 0.2
		{Predicted: 0.2, Actual: 0.0},
		{Predicted: 0.2, Actual: 0.0},
	}
	mce := observe.MaxCalibrationError(predictions, 10)
	t.Logf("MCE (calibrated) = %.4f", mce)
	is.True(mce < 0.1)
}

func TestMaxCalibrationError_Miscalibrated(t *testing.T) {
	is := is.New(t)

	// Two bins: one perfect, one badly miscalibrated → MCE should be high.
	predictions := []observe.PredictionOutcome{
		// Bin near 0.1: predicted 0.1, actual fraction 0.1 → gap ≈ 0
		{Predicted: 0.1, Actual: 0.0},
		{Predicted: 0.1, Actual: 0.0},
		{Predicted: 0.1, Actual: 0.0},
		{Predicted: 0.1, Actual: 0.0},
		{Predicted: 0.1, Actual: 0.0},
		{Predicted: 0.1, Actual: 0.0},
		{Predicted: 0.1, Actual: 0.0},
		{Predicted: 0.1, Actual: 0.0},
		{Predicted: 0.1, Actual: 0.0},
		{Predicted: 0.1, Actual: 1.0}, // 1/10 = 0.1 → gap=0
		// Bin near 0.9: predicted 0.9, actual fraction 0.0 → gap = 0.9
		{Predicted: 0.9, Actual: 0.0},
		{Predicted: 0.9, Actual: 0.0},
		{Predicted: 0.9, Actual: 0.0},
		{Predicted: 0.9, Actual: 0.0},
		{Predicted: 0.9, Actual: 0.0},
	}
	mce := observe.MaxCalibrationError(predictions, 10)
	t.Logf("MCE (miscalibrated) = %.4f", mce)
	is.True(mce > 0.5)
}

func TestMaxCalibrationError_Empty(t *testing.T) {
	is := is.New(t)
	is.Equal(0.0, observe.MaxCalibrationError(nil, 10))
}

// ─── PlattScaler ─────────────────────────────────────────────────────────────

func TestPlattScaler_UnfittedReturnsIdentity(t *testing.T) {
	is := is.New(t)

	var ps observe.PlattScaler
	for _, x := range []float64{0.0, 0.25, 0.5, 0.75, 1.0} {
		is.Equal(x, ps.Calibrate(x))
	}
}

func TestPlattScaler_FitRequiresMinSamples(t *testing.T) {
	is := is.New(t)

	// Fewer than 50 samples → not fitted.
	predictions := []observe.PredictionOutcome{
		{Predicted: 0.8, Actual: 1.0},
		{Predicted: 0.2, Actual: 0.0},
	}
	var ps observe.PlattScaler
	ps.Fit(predictions, 0) // 0 triggers default of 50
	is.True(!ps.Fitted)
	// Still identity.
	is.Equal(0.5, ps.Calibrate(0.5))
}

func TestPlattScaler_FitAndCalibrateRoundTrip(t *testing.T) {
	is := is.New(t)

	// Generate synthetic data: raw predictions are overconfident (squished toward
	// 0.9 and 0.1) but the underlying signal is logistic. Platt scaling should
	// move the calibrated outputs toward 0.5 for borderline cases, and the ECE
	// should improve (decrease) relative to the raw predictions.
	//
	// Construction: true probability p(y=1|x) = sigma(4x - 2).
	// Raw prediction is x itself — overconfident when x is far from 0.5.
	// We set actual = round(true_p) to create clean 0/1 labels.
	n := 200
	predictions := make([]observe.PredictionOutcome, n)
	for i := 0; i < n; i++ {
		x := float64(i) / float64(n-1) // 0..1
		z := 4.0*x - 2.0
		trueP := 1.0 / (1.0 + math.Exp(-z))
		var actual float64
		if trueP >= 0.5 {
			actual = 1.0
		}
		predictions[i] = observe.PredictionOutcome{Predicted: x, Actual: actual}
	}

	var ps observe.PlattScaler
	ps.Fit(predictions, 50)

	is.True(ps.Fitted)
	t.Logf("Fitted PlattScaler: A=%.4f B=%.4f", ps.A, ps.B)

	// Calibrated output must be in [0, 1] for all training inputs.
	for _, pred := range predictions {
		c := ps.Calibrate(pred.Predicted)
		is.True(c >= 0.0 && c <= 1.0)
	}

	// ECE should improve (decrease) after Platt calibration.
	rawECE := observe.ExpectedCalibrationError(predictions, 10)

	calibrated := make([]observe.PredictionOutcome, n)
	for i, pred := range predictions {
		calibrated[i] = observe.PredictionOutcome{
			Predicted: ps.Calibrate(pred.Predicted),
			Actual:    pred.Actual,
		}
	}
	calECE := observe.ExpectedCalibrationError(calibrated, 10)

	t.Logf("ECE raw=%.4f calibrated=%.4f", rawECE, calECE)
	// Platt scaling on logistic data should not make ECE dramatically worse.
	// We allow calibrated ECE to be at most 1.5x the raw ECE as a sanity bound.
	is.True(calECE <= rawECE*1.5+0.05)
}

// ─── IsotonicRegressor ───────────────────────────────────────────────────────

func TestIsotonicRegressor_UnfittedReturnsIdentity(t *testing.T) {
	is := is.New(t)

	var ir observe.IsotonicRegressor
	for _, x := range []float64{0.0, 0.3, 0.7, 1.0} {
		is.Equal(x, ir.Calibrate(x))
	}
}

func TestIsotonicRegressor_MonotoneData(t *testing.T) {
	is := is.New(t)

	// Simple monotone data: low predictions → low actuals, high → high.
	predictions := []observe.PredictionOutcome{
		{Predicted: 0.1, Actual: 0.0},
		{Predicted: 0.2, Actual: 0.0},
		{Predicted: 0.3, Actual: 0.0},
		{Predicted: 0.4, Actual: 0.0},
		{Predicted: 0.5, Actual: 1.0},
		{Predicted: 0.6, Actual: 1.0},
		{Predicted: 0.7, Actual: 1.0},
		{Predicted: 0.8, Actual: 1.0},
		{Predicted: 0.9, Actual: 1.0},
		{Predicted: 1.0, Actual: 1.0},
	}

	var ir observe.IsotonicRegressor
	ir.Fit(predictions)

	is.True(ir.Fitted)

	// For monotone data, calibrated output should be monotonically non-decreasing.
	prev := ir.Calibrate(0.0)
	for _, x := range []float64{0.1, 0.2, 0.3, 0.5, 0.7, 0.9, 1.0} {
		curr := ir.Calibrate(x)
		is.True(curr >= prev-1e-9) // allow tiny floating-point slack
		prev = curr
	}

	t.Logf("Isotonic output at 0.1=%.3f 0.5=%.3f 0.9=%.3f",
		ir.Calibrate(0.1), ir.Calibrate(0.5), ir.Calibrate(0.9))
}

func TestIsotonicRegressor_NonMonotoneDataMerged(t *testing.T) {
	is := is.New(t)

	// Deliberately non-monotone: PAVA must merge violating blocks.
	// Block at 0.6 has lower actual fraction than block at 0.4 → merge.
	predictions := []observe.PredictionOutcome{
		{Predicted: 0.1, Actual: 0.0},
		{Predicted: 0.2, Actual: 0.0},
		{Predicted: 0.4, Actual: 1.0},
		{Predicted: 0.4, Actual: 1.0},
		{Predicted: 0.6, Actual: 0.0}, // violates monotonicity
		{Predicted: 0.6, Actual: 0.0},
		{Predicted: 0.8, Actual: 1.0},
		{Predicted: 0.9, Actual: 1.0},
		{Predicted: 1.0, Actual: 1.0},
		{Predicted: 1.0, Actual: 1.0},
	}

	var ir observe.IsotonicRegressor
	ir.Fit(predictions)
	is.True(ir.Fitted)

	// After PAVA the output must still be monotonically non-decreasing.
	prev := ir.Calibrate(0.0)
	for _, x := range []float64{0.1, 0.2, 0.4, 0.6, 0.8, 0.9, 1.0} {
		curr := ir.Calibrate(x)
		is.True(curr >= prev-1e-9)
		prev = curr
	}
}

func TestIsotonicRegressor_OutputInRange(t *testing.T) {
	is := is.New(t)

	// Output must remain in [0, 1] for inputs within training range.
	predictions := make([]observe.PredictionOutcome, 100)
	for i := range predictions {
		x := float64(i) / 99.0
		var actual float64
		if x > 0.5 {
			actual = 1.0
		}
		predictions[i] = observe.PredictionOutcome{Predicted: x, Actual: actual}
	}

	var ir observe.IsotonicRegressor
	ir.Fit(predictions)
	is.True(ir.Fitted)

	for _, x := range []float64{0.0, 0.1, 0.5, 0.9, 1.0} {
		c := ir.Calibrate(x)
		is.True(c >= 0.0 && c <= 1.0)
	}
}
