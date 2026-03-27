package core

import (
	"testing"

	"github.com/matryer/is"
)

func TestDomainCredibility_FallsBackToGlobal(t *testing.T) {
	is := is.New(t)

	src := DefaultSource("test-ns", "user:alice")
	// Apply a few global updates so global != 0.5
	src.BayesianUpdate(true)
	src.BayesianUpdate(true)
	src.BayesianUpdate(false)
	globalCred := src.EffectiveCredibility()

	// No domain data has been stored yet — should return global credibility
	is.Equal(src.DomainCredibility("science"), globalCred)
	is.Equal(src.DomainCredibility(""), globalCred)
}

func TestDomainBayesianUpdate_CreatesDomainScopedCreds(t *testing.T) {
	is := is.New(t)

	src := DefaultSource("test-ns", "user:bob")

	// Perform domain-scoped updates in "finance"
	src.DomainBayesianUpdate("finance", true)
	src.DomainBayesianUpdate("finance", true)
	src.DomainBayesianUpdate("finance", false)

	domCreds := src.getDomainCreds()
	dc, ok := domCreds["finance"]
	is.True(ok) // domain entry must exist after update

	// Started at Beta(1,1), applied +2 validated, +1 refuted → Beta(3,2)
	is.Equal(dc.Alpha, 3.0)
	is.Equal(dc.Beta, 2.0)

	// Global credibility must also have been updated (3 calls to BayesianUpdate)
	is.Equal(src.ClaimsAsserted, int64(3))
}

func TestDomainCredibility_ReturnsDomainSpecificValue(t *testing.T) {
	is := is.New(t)

	src := DefaultSource("test-ns", "user:carol")

	// Push global credibility low by adding many refutations
	for i := 0; i < 9; i++ {
		src.BayesianUpdate(false)
	}
	// Global: Beta(1, 10) → mean = 1/11 ≈ 0.09

	// Boost domain "medicine" with many validations — without touching global further
	// We call DomainBayesianUpdate which also touches global, so let's set domain creds directly
	domCreds := map[string]BetaParams{
		"medicine": {Alpha: 9.0, Beta: 1.0}, // mean = 9/10 = 0.9
	}
	src.setDomainCreds(domCreds)

	globalCred := src.EffectiveCredibility()
	medicineCred := src.DomainCredibility("medicine")
	scienceCred := src.DomainCredibility("science") // no domain data → fallback to global

	// Domain-specific credibility should be much higher than global
	is.True(medicineCred > globalCred)
	// Expected: medicine ≈ 0.9, global ≈ 0.09
	is.Equal(medicineCred, 0.9)
	// Unknown domain falls back to global
	is.Equal(scienceCred, globalCred)
}

func TestDomainBayesianUpdate_EmptyDomainOnlyUpdatesGlobal(t *testing.T) {
	is := is.New(t)

	src := DefaultSource("test-ns", "user:dave")
	src.DomainBayesianUpdate("", true)

	// Global must be updated
	is.Equal(src.ClaimsAsserted, int64(1))
	is.Equal(src.Alpha, 2.0) // 1 (prior) + 1 (validated)

	// No domain entries should exist
	domCreds := src.getDomainCreds()
	is.Equal(len(domCreds), 0)
}

func TestGetDomainCreds_HandlesJSONDeserialized(t *testing.T) {
	is := is.New(t)

	src := DefaultSource("test-ns", "user:eve")

	// Simulate what happens after a JSON round-trip: map[string]any with nested map[string]any
	src.Properties = map[string]any{
		"domain_cred": map[string]any{
			"history": map[string]any{
				"alpha": float64(5),
				"beta":  float64(2),
			},
		},
	}

	domCreds := src.getDomainCreds()
	dc, ok := domCreds["history"]
	is.True(ok)
	is.Equal(dc.Alpha, 5.0)
	is.Equal(dc.Beta, 2.0)

	// DomainCredibility should also work via the deserialized path
	cred := src.DomainCredibility("history")
	is.Equal(cred, 5.0/7.0)
}

func TestDomainCredibility_EmptyDomainReturnsGlobal(t *testing.T) {
	is := is.New(t)

	src := DefaultSource("test-ns", "user:frank")
	src.DomainBayesianUpdate("tech", true)

	// Empty string domain always returns global, never domain-specific
	is.Equal(src.DomainCredibility(""), src.EffectiveCredibility())
}
