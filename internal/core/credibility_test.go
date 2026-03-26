package core

import (
	"math"
	"testing"
	"time"

	"github.com/matryer/is"
)

func TestWeightedBayesianUpdate_Validated(t *testing.T) {
	is := is.New(t)
	src := DefaultSource("test-ns", "user:alice")

	// Standard weight = 1.0 (same as regular BayesianUpdate)
	src.WeightedBayesianUpdate(true, 1.0)
	is.Equal(src.Alpha, 2.0)
	is.Equal(src.Beta, 1.0)

	// Higher weight = larger shift
	src.WeightedBayesianUpdate(true, 3.0)
	is.Equal(src.Alpha, 5.0) // 2 + 3
	is.Equal(src.ClaimsValidated, int64(2))
}

func TestWeightedBayesianUpdate_Refuted(t *testing.T) {
	is := is.New(t)
	src := DefaultSource("test-ns", "user:alice")

	// Weighted refutation
	src.WeightedBayesianUpdate(false, 2.0)
	is.Equal(src.Alpha, 1.0)
	is.Equal(src.Beta, 3.0) // 1 + 2
	is.Equal(src.ClaimsRefuted, int64(1))
}

func TestWeightedBayesianUpdate_Clamped(t *testing.T) {
	is := is.New(t)
	src := DefaultSource("test-ns", "user:alice")

	// Weight below 0 should be clamped to 1.0
	src.WeightedBayesianUpdate(true, -5.0)
	is.Equal(src.Alpha, 2.0) // 1 + 1 (clamped)

	// Weight above 10 should be clamped to 10.0
	src2 := DefaultSource("test-ns", "user:bob")
	src2.WeightedBayesianUpdate(true, 100.0)
	is.Equal(src2.Alpha, 11.0) // 1 + 10 (clamped)
}

func TestDecayConfig_Defaults(t *testing.T) {
	is := is.New(t)
	cfg := DefaultDecayConfig()
	is.True(cfg.Lambda > 0)
	is.True(cfg.MinAlpha == 1.0)
	is.True(cfg.MinBeta == 1.0)
}

func TestDecayedCredibility_Fresh(t *testing.T) {
	is := is.New(t)
	src := DefaultSource("test-ns", "user:alice")

	// Immediately after creation, decayed credibility should equal regular
	regular := src.EffectiveCredibility()
	decayed := src.DecayedCredibility(DefaultDecayConfig())
	is.True(math.Abs(regular-decayed) < 0.001)
}

func TestDecayedCredibility_DecayOverTime(t *testing.T) {
	is := is.New(t)

	// Create source with high credibility
	src := DefaultSource("test-ns", "user:alice")
	for i := 0; i < 20; i++ {
		src.BayesianUpdate(true)
	}

	// High credibility initially
	initialCred := src.EffectiveCredibility()
	is.True(initialCred > 0.9)

	// Simulate time passing by manually setting UpdatedAt
	src.UpdatedAt = time.Now().Add(-30 * 24 * time.Hour) // 30 days ago

	// With decay, credibility should decrease
	cfg := DefaultDecayConfig()
	decayedCred := src.DecayedCredibility(cfg)
	is.True(decayedCred < initialCred)

	// But should not decay below the prior mean (0.5)
	is.True(decayedCred >= 0.5)
}

func TestDecayedCredibility_ConvergesToPrior(t *testing.T) {
	is := is.New(t)

	// Source with extreme credibility
	src := DefaultSource("test-ns", "user:alice")
	src.Alpha = 100
	src.Beta = 2

	// Very old update
	src.UpdatedAt = time.Now().Add(-365 * 24 * time.Hour) // 1 year ago

	cfg := DefaultDecayConfig()
	decayed := src.DecayedCredibility(cfg)

	// Should converge toward the prior mean (0.5) but not go below
	is.True(decayed > 0.5)
	is.True(decayed < 0.9) // Was close to 1.0, now decayed
}

func TestDecayedParameters(t *testing.T) {
	is := is.New(t)

	src := DefaultSource("test-ns", "user:alice")
	src.Alpha = 10
	src.Beta = 5
	src.UpdatedAt = time.Now().Add(-7 * 24 * time.Hour) // 1 week ago

	cfg := DefaultDecayConfig()
	alpha, beta := src.DecayedParameters(cfg)

	// Decayed parameters should be less than original
	is.True(alpha < src.Alpha)
	is.True(beta < src.Beta)

	// But not less than minimum
	is.True(alpha >= cfg.MinAlpha)
	is.True(beta >= cfg.MinBeta)
}

func TestCredibleInterval(t *testing.T) {
	is := is.New(t)

	src := DefaultSource("test-ns", "user:alice")

	// With few observations, interval should be wide
	lower, upper := src.CredibleInterval(0.95)
	is.True(lower < 0.5)
	is.True(upper > 0.5)
	is.True(upper-lower > 0.3) // Wide interval

	// Add many observations
	for i := 0; i < 100; i++ {
		src.BayesianUpdate(true)
	}

	// Interval should be narrower
	lower2, upper2 := src.CredibleInterval(0.95)
	is.True(upper2-lower2 < 0.2) // Narrower interval
	is.True(lower2 > 0.8)        // High credibility
}

func TestCredibleInterval_Bounds(t *testing.T) {
	is := is.New(t)

	// Extreme credibility
	src := DefaultSource("test-ns", "user:alice")
	src.Alpha = 1000
	src.Beta = 1

	lower, upper := src.CredibleInterval(0.95)
	is.True(lower >= 0)
	is.True(upper <= 1)
	is.True(lower < upper)
}

func TestPredictiveDistribution(t *testing.T) {
	is := is.New(t)

	src := DefaultSource("test-ns", "user:alice")

	// Predictive should equal mean
	pred := src.PredictiveDistribution()
	mean := src.EffectiveCredibility()
	is.Equal(pred, mean)

	// After updates, should track mean
	src.BayesianUpdate(true)
	pred = src.PredictiveDistribution()
	mean = src.EffectiveCredibility()
	is.Equal(pred, mean)
}

func TestThompsonSample_Range(t *testing.T) {
	is := is.New(t)

	src := DefaultSource("test-ns", "user:alice")

	// Generate multiple samples
	for i := 0; i < 100; i++ {
		sample := src.ThompsonSample()
		is.True(sample >= 0)
		is.True(sample <= 1)
	}
}

func TestThompsonSample_MeanConvergence(t *testing.T) {
	is := is.New(t)

	// High credibility source
	src := DefaultSource("test-ns", "user:alice")
	src.Alpha = 50
	src.Beta = 10
	mean := src.EffectiveCredibility()

	// Sample mean should be close to actual mean
	var sum float64
	n := 1000
	for i := 0; i < n; i++ {
		sum += src.ThompsonSample()
	}
	sampleMean := sum / float64(n)

	// Within 5% of true mean
	is.True(math.Abs(sampleMean-mean) < 0.05)
}

func TestObservationWeight(t *testing.T) {
	is := is.New(t)

	// Default: weight = 1.0
	w1 := ObservationWeight(0.5, 1, false)
	is.True(math.Abs(w1-1.0) < 0.1)

	// High confidence boost
	w2 := ObservationWeight(1.0, 1, false)
	is.True(w2 > w1)

	// Multiple agreeing sources boost
	w3 := ObservationWeight(0.5, 5, false)
	is.True(w3 > w1)

	// Factual claim boost
	w4 := ObservationWeight(0.5, 1, true)
	is.True(w4 > w1)

	// Combined boost (capped at 10)
	w5 := ObservationWeight(1.0, 10, true)
	is.True(w5 <= 10.0)
	is.True(w5 > 2.0)
}

func TestObservationWeight_Clamped(t *testing.T) {
	is := is.New(t)

	// Very low confidence (0.1) should be clamped
	w := ObservationWeight(0.1, 1, false)
	is.True(w >= 0.1)

	// Extreme values should be clamped to [0.1, 10]
	w2 := ObservationWeight(1.0, 100, true)
	is.True(w2 <= 10.0)
}

func TestCredibilityIntegration(t *testing.T) {
	is := is.New(t)

	// Simulate a real-world credibility learning scenario
	src := DefaultSource("test-ns", "source:reporter")

	// Initial uncertainty
	lower1, upper1 := src.CredibleInterval(0.95)
	uncertainty1 := upper1 - lower1
	is.True(uncertainty1 > 0.3) // High uncertainty

	// Weighted update based on observation weight
	weight := ObservationWeight(0.9, 3, true) // High confidence, multiple sources, factual
	src.WeightedBayesianUpdate(true, weight)

	// Multiple regular updates
	for i := 0; i < 10; i++ {
		src.BayesianUpdate(true)
	}

	// Credibility should be high
	is.True(src.EffectiveCredibility() > 0.8)

	// Uncertainty should be lower
	lower2, upper2 := src.CredibleInterval(0.95)
	uncertainty2 := upper2 - lower2
	is.True(uncertainty2 < uncertainty1)

	// Time passes...
	src.UpdatedAt = time.Now().Add(-30 * 24 * time.Hour)

	// Decayed credibility should be lower than current
	decayed := src.DecayedCredibility(DefaultDecayConfig())
	is.True(decayed < src.EffectiveCredibility())
	is.True(decayed > 0.5) // But still above neutral
}
