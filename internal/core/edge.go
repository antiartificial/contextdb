package core

import (
	"time"

	"github.com/google/uuid"
)

// Conventional edge type constants. Types are free-form strings;
// these constants exist for consistency and discoverability.
const (
	EdgeRelatesTo    = "relates_to"    // general semantic relationship
	EdgeContradicts  = "contradicts"   // source claim conflicts with destination
	EdgeSupersedes   = "supersedes"    // source is a newer version of destination
	EdgeEndorsedBy   = "endorsed_by"   // source claim is endorsed by destination source
	EdgeDerivedFrom  = "derived_from"  // source was abstracted/summarised from destination
	EdgeSupports     = "supports"      // source provides evidence for destination
	EdgeCites        = "cites"         // source references destination
	EdgeRefines      = "refines"       // source narrows or clarifies destination
	EdgeGeneralizes  = "generalizes"   // source broadens destination
	EdgeIsExampleOf  = "is_example_of" // source is a concrete instance of destination
)

// Edge is a directed, typed, weighted relationship between two nodes.
// Edge types are caller-defined strings; see the Edge* constants for
// conventional types.
type Edge struct {
	ID         uuid.UUID
	Namespace  string
	Src        uuid.UUID
	Dst        uuid.UUID
	Type       string
	Weight     float64
	Properties map[string]any

	ValidFrom  time.Time
	ValidUntil *time.Time
	TxTime     time.Time

	// non-destructive logical deletion
	InvalidatedAt *time.Time
}

// IsActiveAt reports whether the edge is logically present at time t.
func (e Edge) IsActiveAt(t time.Time) bool {
	if e.InvalidatedAt != nil && !t.Before(*e.InvalidatedAt) {
		return false
	}
	if t.Before(e.ValidFrom) {
		return false
	}
	if e.ValidUntil != nil && t.After(*e.ValidUntil) {
		return false
	}
	return true
}
