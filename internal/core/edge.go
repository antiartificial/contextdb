package core

import (
	"time"

	"github.com/google/uuid"
)

// Edge is a directed, typed, weighted relationship between two nodes.
// Edge types are caller-defined strings.
//
// Conventional types (not enforced):
//
//	"relates_to"    general semantic relationship
//	"contradicts"   source claim conflicts with destination
//	"supercedes"    source is a newer version of destination
//	"endorsed_by"   source claim is endorsed by destination source
//	"derived_from"  source was abstracted/summarised from destination
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
