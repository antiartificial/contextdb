package core

import (
	"time"

	"github.com/google/uuid"
)

// Source represents the origin of a claim or memory. Credibility starts at
// 0.5 for unknown sources and is updated based on claim outcomes.
type Source struct {
	ID               uuid.UUID
	Namespace        string
	ExternalID       string   // Discord user ID, agent ID, doc URL, etc.
	Labels           []string // "moderator", "bot", "verified", "flagged", "troll"
	CredibilityScore float64  // 0.0–1.0
	ClaimsAsserted   int64
	ClaimsValidated  int64
	ClaimsRefuted    int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// EffectiveCredibility returns the credibility score after applying label
// overrides. Moderator/admin labels always return 1.0; flagged/troll labels
// always return 0.05 regardless of the stored score.
func (s Source) EffectiveCredibility() float64 {
	for _, l := range s.Labels {
		switch l {
		case "moderator", "admin":
			return 1.0
		case "flagged", "troll":
			return 0.05
		}
	}
	return s.CredibilityScore
}

// DefaultSource returns a new Source with neutral credibility.
func DefaultSource(ns, externalID string) Source {
	now := time.Now()
	return Source{
		ID:               uuid.New(),
		Namespace:        ns,
		ExternalID:       externalID,
		CredibilityScore: 0.5,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}
