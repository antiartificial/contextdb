package core

import (
	"time"

	"github.com/google/uuid"
)

// Source represents the origin of a claim or memory. Credibility is modeled
// using a Beta-Binomial conjugate prior: Beta(Alpha, Beta) where
// Alpha = 1 + ClaimsValidated and Beta = 1 + ClaimsRefuted (Laplace smoothing).
// The mean credibility E[Beta] = Alpha / (Alpha + Beta).
type Source struct {
	ID              uuid.UUID
	Namespace       string
	ExternalID      string         // Discord user ID, agent ID, doc URL, etc.
	Labels          []string       // "moderator", "bot", "verified", "flagged", "troll"
	Alpha           float64        // Beta distribution α parameter (validated + 1)
	Beta            float64        // Beta distribution β parameter (refuted + 1)
	ClaimsAsserted  int64
	ClaimsValidated int64
	ClaimsRefuted   int64
	Properties      map[string]any // extensible metadata; "domain_cred" key stores per-domain BetaParams
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// BetaParams stores Alpha/Beta for a Beta distribution.
type BetaParams struct {
	Alpha float64 `json:"alpha"`
	Beta  float64 `json:"beta"`
}

// DomainCredibility returns the effective credibility for a specific domain.
// Falls back to the global credibility if no domain-specific data exists.
func (s Source) DomainCredibility(domain string) float64 {
	if domain == "" {
		return s.EffectiveCredibility()
	}
	domCreds := s.getDomainCreds()
	if dc, ok := domCreds[domain]; ok {
		return dc.Alpha / (dc.Alpha + dc.Beta)
	}
	return s.EffectiveCredibility()
}

// DomainBayesianUpdate performs a Bayesian update scoped to a domain.
// It also updates the global credibility so aggregate statistics stay current.
func (s *Source) DomainBayesianUpdate(domain string, validated bool) {
	// Always update global credibility too
	s.BayesianUpdate(validated)

	if domain == "" {
		return
	}
	domCreds := s.getDomainCreds()
	dc, ok := domCreds[domain]
	if !ok {
		dc = BetaParams{Alpha: 1.0, Beta: 1.0} // uniform prior
	}
	if validated {
		dc.Alpha++
	} else {
		dc.Beta++
	}
	domCreds[domain] = dc
	s.setDomainCreds(domCreds)
}

// getDomainCreds reads the per-domain Beta parameters from Properties.
// Handles both native map[string]BetaParams and JSON-deserialized map[string]any.
func (s Source) getDomainCreds() map[string]BetaParams {
	if s.Properties == nil {
		return make(map[string]BetaParams)
	}
	raw, ok := s.Properties["domain_cred"]
	if !ok {
		return make(map[string]BetaParams)
	}
	// In-process storage: already the right type
	if m, ok := raw.(map[string]BetaParams); ok {
		return m
	}
	// After JSON round-trip: map[string]any with nested map[string]any values
	if m, ok := raw.(map[string]any); ok {
		result := make(map[string]BetaParams, len(m))
		for k, v := range m {
			if bp, ok := v.(map[string]any); ok {
				alpha, _ := bp["alpha"].(float64)
				beta, _ := bp["beta"].(float64)
				if alpha == 0 {
					alpha = 1
				}
				if beta == 0 {
					beta = 1
				}
				result[k] = BetaParams{Alpha: alpha, Beta: beta}
			}
		}
		return result
	}
	return make(map[string]BetaParams)
}

// setDomainCreds writes the per-domain Beta parameters into Properties.
func (s *Source) setDomainCreds(m map[string]BetaParams) {
	if s.Properties == nil {
		s.Properties = make(map[string]any)
	}
	s.Properties["domain_cred"] = m
}

// EffectiveCredibility returns the mean of the Beta distribution (Alpha/(Alpha+Beta))
// after applying label overrides. Moderator/admin labels always return 1.0;
// flagged/troll labels always return 0.05.
//
// The Beta distribution variance is BetaVar = (Alpha * Beta) / ((Alpha + Beta)^2 * (Alpha + Beta + 1))
// which provides uncertainty quantification (lower variance = more certainty).
func (s Source) EffectiveCredibility() float64 {
	for _, l := range s.Labels {
		switch l {
		case "moderator", "admin":
			return 1.0
		case "flagged", "troll":
			return 0.05
		}
	}
	// Compute mean of Beta distribution
	sum := s.Alpha + s.Beta
	if sum == 0 {
		return 0.5 // neutral if not initialized
	}
	return s.Alpha / sum
}

// CredibilityVariance returns the variance of the Beta distribution,
// which quantifies uncertainty in the credibility estimate.
// Lower variance indicates more certainty (more observations).
func (s Source) CredibilityVariance() float64 {
	sum := s.Alpha + s.Beta
	if sum == 0 {
		return 0.25 // maximum variance for uniform prior
	}
	return (s.Alpha * s.Beta) / (sum * sum * (sum + 1))
}

// BayesianUpdate increments Alpha on validation or Beta on refutation.
// This is the conjugate prior update for a binomial likelihood.
func (s *Source) BayesianUpdate(validated bool) {
	if validated {
		s.Alpha += 1
		s.ClaimsValidated++
	} else {
		s.Beta += 1
		s.ClaimsRefuted++
	}
	s.ClaimsAsserted++
	s.UpdatedAt = time.Now()
}

// DefaultSource returns a new Source with neutral Beta(1,1) prior
// (uniform distribution, mean credibility = 0.5).
func DefaultSource(ns, externalID string) Source {
	now := time.Now()
	return Source{
		ID:         uuid.New(),
		Namespace:  ns,
		ExternalID: externalID,
		Alpha:      1, // Beta(1,1) = uniform prior
		Beta:       1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}
