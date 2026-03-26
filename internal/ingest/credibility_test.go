package ingest

import (
	"math"
	"testing"

	"github.com/matryer/is"
)

func TestComputeDelta(t *testing.T) {
	is := is.New(t)

	// Validated > refuted → positive delta
	delta := ComputeDelta(10, 2, 0.5, 0.1)
	is.True(delta > 0)

	// Refuted > validated → negative delta
	delta = ComputeDelta(2, 10, 0.5, 0.1)
	is.True(delta < 0)

	// Equal validated/refuted → near zero delta
	delta = ComputeDelta(5, 5, 0.5, 0.1)
	is.True(math.Abs(delta) < 0.02) // small adjustment

	// Clamp: don't exceed 1.0
	delta = ComputeDelta(100, 0, 0.99, 0.5)
	is.True(0.99+delta <= 1.0)

	// Clamp: don't go below 0.05
	delta = ComputeDelta(0, 100, 0.06, 0.5)
	is.True(0.06+delta >= 0.05)

	// Zero claims → slight negative (biased toward 0.5 prior)
	delta = ComputeDelta(0, 0, 0.5, 0.1)
	is.True(math.Abs(delta) <= 0.1) // small
}

func TestCredibilityEvolution(t *testing.T) {
	is := is.New(t)

	// Simulate credibility evolving over time
	cred := 0.5 // starting credibility
	lr := 0.1

	// Source publishes validated claims
	for i := 0; i < 20; i++ {
		delta := ComputeDelta(int64(i+1), 0, cred, lr)
		cred += delta
	}
	is.True(cred > 0.6) // credibility should rise

	// Source then publishes many refuted claims
	for i := 0; i < 30; i++ {
		delta := ComputeDelta(20, int64(i+21), cred, lr)
		cred += delta
	}
	is.True(cred < 0.5) // credibility should drop
}
