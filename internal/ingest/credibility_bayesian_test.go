package ingest

import (
	"math"
	"testing"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/matryer/is"
)

func TestDefaultSource_BetaDistribution(t *testing.T) {
	is := is.New(t)

	src := core.DefaultSource("test-ns", "user:alice")

	// Default should be Beta(1, 1) = uniform prior
	is.Equal(src.Alpha, 1.0)
	is.Equal(src.Beta, 1.0)

	// Mean credibility should be 0.5
	is.Equal(src.EffectiveCredibility(), 0.5)

	// Variance of Beta(1,1) = 1/4 * 1/3 = 1/12 ≈ 0.083
	expectedVar := 1.0 / 12.0
	is.True(math.Abs(src.CredibilityVariance()-expectedVar) < 0.001)
}

func TestBayesianUpdate_Validated(t *testing.T) {
	is := is.New(t)

	src := core.DefaultSource("test-ns", "user:alice")

	// Update with validated claim
	src.BayesianUpdate(true)

	// Alpha should increase, Beta stays same
	is.Equal(src.Alpha, 2.0)
	is.Equal(src.Beta, 1.0)
	is.Equal(src.ClaimsValidated, int64(1))
	is.Equal(src.ClaimsAsserted, int64(1))

	// Mean credibility should increase
	is.Equal(src.EffectiveCredibility(), 2.0/3.0) // 2/(2+1) = 0.667
}

func TestBayesianUpdate_Refuted(t *testing.T) {
	is := is.New(t)

	src := core.DefaultSource("test-ns", "user:alice")

	// Update with refuted claim
	src.BayesianUpdate(false)

	// Beta should increase, Alpha stays same
	is.Equal(src.Alpha, 1.0)
	is.Equal(src.Beta, 2.0)
	is.Equal(src.ClaimsRefuted, int64(1))
	is.Equal(src.ClaimsAsserted, int64(1))

	// Mean credibility should decrease
	is.Equal(src.EffectiveCredibility(), 1.0/3.0) // 1/(1+2) = 0.333
}

func TestBayesianUpdate_Multiple(t *testing.T) {
	is := is.New(t)

	src := core.DefaultSource("test-ns", "user:alice")

	// Simulate 10 validated, 3 refuted
	for i := 0; i < 10; i++ {
		src.BayesianUpdate(true)
	}
	for i := 0; i < 3; i++ {
		src.BayesianUpdate(false)
	}

	// Alpha = 1 + 10 = 11, Beta = 1 + 3 = 4
	is.Equal(src.Alpha, 11.0)
	is.Equal(src.Beta, 4.0)
	is.Equal(src.ClaimsValidated, int64(10))
	is.Equal(src.ClaimsRefuted, int64(3))
	is.Equal(src.ClaimsAsserted, int64(13))

	// Mean credibility = 11/15 = 0.733
	is.True(math.Abs(src.EffectiveCredibility()-0.733) < 0.01)
}

func TestCredibilityVariance_DecreasesWithObservations(t *testing.T) {
	is := is.New(t)

	src := core.DefaultSource("test-ns", "user:alice")

	initialVariance := src.CredibilityVariance()

	// Add many observations
	for i := 0; i < 50; i++ {
		src.BayesianUpdate(true)
	}

	// Variance should decrease as we get more certain
	is.True(src.CredibilityVariance() < initialVariance)
	is.True(src.CredibilityVariance() < 0.01) // Very certain after 50 observations
}

func TestMeanCredibility_Helper(t *testing.T) {
	is := is.New(t)

	// Beta(1, 1) = uniform
	is.Equal(MeanCredibility(1, 1), 0.5)

	// Beta(10, 1) = high credibility
	is.Equal(MeanCredibility(10, 1), 10.0/11.0)

	// Beta(1, 10) = low credibility
	is.Equal(MeanCredibility(1, 10), 1.0/11.0)

	// Zero sum returns default 0.5
	is.Equal(MeanCredibility(0, 0), 0.5)
}

func TestCredibilityVariance_Helper(t *testing.T) {
	is := is.New(t)

	// Beta(1, 1) variance
	var11 := CredibilityVariance(1, 1)
	expected := 1.0 / 12.0
	is.True(math.Abs(var11-expected) < 0.0001)

	// Beta(10, 10) should have lower variance
	var1010 := CredibilityVariance(10, 10)
	is.True(var1010 < var11)

	// Beta(100, 100) should have even lower variance
	var100100 := CredibilityVariance(100, 100)
	is.True(var100100 < var1010)

	// Zero sum returns max variance (uniform prior)
	is.Equal(CredibilityVariance(0, 0), 0.25)
}

func TestUpdateSourceBayesian(t *testing.T) {
	is := is.New(t)

	src := core.DefaultSource("test-ns", "user:alice")

	UpdateSourceBayesian(&src, true)
	is.Equal(src.Alpha, 2.0)
	is.Equal(src.ClaimsValidated, int64(1))

	UpdateSourceBayesian(&src, false)
	is.Equal(src.Beta, 2.0)
	is.Equal(src.ClaimsRefuted, int64(1))
}

func TestEffectiveCredibility_LabelOverrides(t *testing.T) {
	is := is.New(t)

	// Moderator label should return 1.0
	src := core.DefaultSource("test-ns", "moderator:alice")
	src.Labels = []string{"moderator"}
	is.Equal(src.EffectiveCredibility(), 1.0)

	// Admin label should also return 1.0
	src.Labels = []string{"admin"}
	is.Equal(src.EffectiveCredibility(), 1.0)

	// Flagged label should return 0.05
	src.Labels = []string{"flagged"}
	is.Equal(src.EffectiveCredibility(), 0.05)

	// Troll label should return 0.05
	src.Labels = []string{"troll"}
	is.Equal(src.EffectiveCredibility(), 0.05)

	// Mixed: moderator + troll - first match wins (moderator)
	src.Labels = []string{"moderator", "troll"}
	is.Equal(src.EffectiveCredibility(), 1.0)
}

func TestBayesianConsistencyWithLegacyCounters(t *testing.T) {
	is := is.New(t)

	src := core.DefaultSource("test-ns", "user:alice")

	// Simulate updates
	src.BayesianUpdate(true)
	src.BayesianUpdate(true)
	src.BayesianUpdate(false)

	// Counters should be consistent with Beta parameters
	// Alpha = 1 + validated = 3
	// Beta = 1 + refuted = 2
	is.Equal(src.Alpha, 1.0+float64(src.ClaimsValidated))
	is.Equal(src.Beta, 1.0+float64(src.ClaimsRefuted))
}

func TestBayesianConvergence(t *testing.T) {
	is := is.New(t)

	src := core.DefaultSource("test-ns", "user:alice")

	// Simulate a source with 80% accuracy
	// After many observations, credibility should converge to ~0.8
	for i := 0; i < 80; i++ {
		src.BayesianUpdate(true)
	}
	for i := 0; i < 20; i++ {
		src.BayesianUpdate(false)
	}

	// After 100 observations with 80% accuracy, mean should be close to 0.8
	// Alpha = 81, Beta = 21, mean = 81/102 ≈ 0.794
	mean := src.EffectiveCredibility()
	is.True(mean > 0.78 && mean < 0.82)

	// Variance should be small
	is.True(src.CredibilityVariance() < 0.002)
}
