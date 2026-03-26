package core

import (
	"math"
	"time"
)

// WeightedBayesianUpdate performs a Bayesian update with configurable weight.
// Higher weights cause larger shifts in credibility (for high-importance claims).
// Weight should be in range (0, 10] where 1.0 is standard.
func (s *Source) WeightedBayesianUpdate(validated bool, weight float64) {
	if weight <= 0 {
		weight = 1.0
	}
	if weight > 10 {
		weight = 10.0
	}

	if validated {
		s.Alpha += weight
		s.ClaimsValidated++
	} else {
		s.Beta += weight
		s.ClaimsRefuted++
	}
	s.ClaimsAsserted++
	s.UpdatedAt = time.Now()
}

// DecayConfig holds parameters for time-decayed credibility.
type DecayConfig struct {
	// Lambda is the decay rate (higher = faster decay). Default: 0.001 per hour (~2% per day)
	Lambda float64
	// MinAlpha, MinBeta ensure the prior never decays below Beta(1,1)
	MinAlpha float64
	MinBeta  float64
}

// DefaultDecayConfig returns sensible defaults for time decay.
func DefaultDecayConfig() DecayConfig {
	return DecayConfig{
		Lambda:   0.001, // ~2% decay per day
		MinAlpha: 1.0,   // Don't decay below uniform prior
		MinBeta:  1.0,
	}
}

// DecayedCredibility returns the effective credibility with time decay applied.
// Recent observations have more weight than older ones.
// The formula applies exponential decay: Alpha_eff = MinAlpha + (Alpha - MinAlpha) * exp(-λ * age_hours)
func (s *Source) DecayedCredibility(cfg DecayConfig) float64 {
	if cfg.Lambda <= 0 {
		cfg = DefaultDecayConfig()
	}

	age := time.Since(s.UpdatedAt).Hours()
	if age < 0 {
		age = 0
	}

	// Apply exponential decay to the observation component (excluding the prior)
	decayFactor := math.Exp(-cfg.Lambda * age)

	// Effective Alpha = MinAlpha + (current - MinAlpha) * decay
	effectiveAlpha := cfg.MinAlpha + (s.Alpha-cfg.MinAlpha)*decayFactor
	effectiveBeta := cfg.MinBeta + (s.Beta-cfg.MinBeta)*decayFactor

	sum := effectiveAlpha + effectiveBeta
	if sum == 0 {
		return 0.5
	}
	return effectiveAlpha / sum
}

// DecayedParameters returns the time-decayed Alpha and Beta values.
// Useful for computing other statistics (variance, CDF, etc.) with decay.
func (s *Source) DecayedParameters(cfg DecayConfig) (alpha, beta float64) {
	if cfg.Lambda <= 0 {
		cfg = DefaultDecayConfig()
	}

	age := time.Since(s.UpdatedAt).Hours()
	if age < 0 {
		age = 0
	}

	decayFactor := math.Exp(-cfg.Lambda * age)

	effectiveAlpha := cfg.MinAlpha + (s.Alpha-cfg.MinAlpha)*decayFactor
	effectiveBeta := cfg.MinBeta + (s.Beta-cfg.MinBeta)*decayFactor

	return effectiveAlpha, effectiveBeta
}

// CredibleInterval returns the Bayesian credible interval for the credibility estimate.
// Uses normal approximation to Beta distribution for large samples.
// For small samples, returns conservative bounds.
// confidence should be 0.90, 0.95, or 0.99.
func (s *Source) CredibleInterval(confidence float64) (lower, upper float64) {
	// Z-scores for common confidence levels
	var z float64
	switch confidence {
	case 0.90:
		z = 1.645
	case 0.95:
		z = 1.96
	case 0.99:
		z = 2.576
	default:
		z = 1.96 // default to 95%
	}

	mean := s.EffectiveCredibility()
	std := math.Sqrt(s.CredibilityVariance())

	// For small samples, use conservative bounds (wider interval)
	if s.ClaimsAsserted < 10 {
		z *= 1.5
	}

	lower = mean - z*std
	upper = mean + z*std

	// Clamp to valid probability range
	if lower < 0 {
		lower = 0
	}
	if upper > 1 {
		upper = 1
	}

	return lower, upper
}

// PredictiveDistribution returns the probability of success on the next observation
// using the posterior predictive (Beta-Binomial marginal likelihood).
// For a Beta(α, β) posterior, P(next=success) = α / (α + β) which equals the mean.
// This is the basis for Thompson sampling and other Bayesian decision strategies.
func (s *Source) PredictiveDistribution() float64 {
	return s.EffectiveCredibility()
}

// ThompsonSample returns a single sample from the posterior Beta distribution.
// Useful for exploration-exploitation tradeoffs in multi-armed bandit algorithms.
// Uses the relationship: Beta(α, β) ~ Gamma(α, 1) / (Gamma(α, 1) + Gamma(β, 1))
// For efficiency, uses normal approximation when α + β > 30.
func (s *Source) ThompsonSample() float64 {
	sum := s.Alpha + s.Beta

	// Normal approximation for large parameters (Central Limit Theorem)
	if sum > 30 {
		mean := s.Alpha / sum
		variance := (s.Alpha * s.Beta) / (sum * sum * (sum + 1))
		std := math.Sqrt(variance)

		// Box-Muller transform for normal sample
		u1 := randFloat()
		u2 := randFloat()
		z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)

		sample := mean + z*std
		if sample < 0 {
			return 0
		}
		if sample > 1 {
			return 1
		}
		return sample
	}

	// For small parameters, use the gamma distribution relationship
	// Beta(α, β) = Gamma(α) / (Gamma(α) + Gamma(β))
	x := randGamma(s.Alpha, 1)
	y := randGamma(s.Beta, 1)
	return x / (x + y)
}

// randFloat returns a pseudo-random float in [0, 1).
// Uses a simple xorshift* for reproducibility (not crypto-secure).
var rngState uint64 = uint64(time.Now().UnixNano())

func randFloat() float64 {
	rngState ^= rngState >> 12
	rngState ^= rngState << 25
	rngState ^= rngState >> 27
	return float64(rngState>>11) / (1 << 53)
}

// randGamma returns a gamma-distributed sample using Marsaglia-Tsang method.
func randGamma(shape, scale float64) float64 {
	if shape >= 1 {
		d := shape - 1.0/3.0
		c := 1.0 / math.Sqrt(9.0*d)
		for {
			x := randNormal()
			v := 1.0 + c*x
			if v <= 0 {
				continue
			}
			v = v * v * v
			u := randFloat()
			if u < 1.0-0.0331*(x*x)*(x*x) ||
				math.Log(u) < 0.5*x*x+d*(1.0-v+math.Log(v)) {
				return scale * d * v
			}
		}
	}
	// For shape < 1, use the fact that Gamma(α) = Gamma(α+1) * U^(1/α)
	return randGamma(shape+1, scale) * math.Pow(randFloat(), 1.0/shape)
}

// randNormal returns a standard normal sample.
func randNormal() float64 {
	u1 := randFloat()
	u2 := randFloat()
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}

// ObservationWeight computes an automatic weight for a Bayesian update
// based on claim features: confidence, source agreement, and claim type.
// Higher confidence claims from diverse agreeing sources get higher weights.
func ObservationWeight(claimConfidence float64, agreeingSources int, isFactual bool) float64 {
	weight := 1.0

	// Scale by claim confidence (0.5 to 1.5x)
	weight *= 0.5 + claimConfidence

	// Boost for multiple agreeing sources (up to 2x)
	if agreeingSources > 1 {
		boost := 1.0 + 0.2*math.Min(float64(agreeingSources-1), 5)
		weight *= boost
	}

	// Factual claims get higher weight than opinions
	if isFactual {
		weight *= 1.3
	}

	// Clamp to valid range
	if weight > 10 {
		weight = 10
	}
	if weight < 0.1 {
		weight = 0.1
	}

	return weight
}
