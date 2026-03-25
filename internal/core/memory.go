package core

// MemoryType determines decay rate and retrieval priority.
// Used by agentic memory namespaces; ignored by belief-system namespaces.
type MemoryType string

const (
	MemoryEpisodic   MemoryType = "episodic"   // specific events; decays in days
	MemorySemantic   MemoryType = "semantic"   // generalized facts; decays in weeks
	MemoryProcedural MemoryType = "procedural" // learned workflows; decays in months
	MemoryWorking    MemoryType = "working"    // current task context; near-instant decay
	MemoryGeneral    MemoryType = ""           // untyped; uses general decay
)

// DecayAlpha returns the exponential decay constant for a memory type.
//
// Score multiplier = exp(-alpha * age_in_hours)
//
// Interpretation:
//
//	episodic   0.08  → half-life ~8.7 hours
//	semantic   0.02  → half-life ~34.7 hours (~1.4 days)
//	procedural 0.001 → half-life ~693 hours (~29 days)
//	working    999   → effectively zero after first hour
//	general    0.05  → half-life ~13.9 hours
func DecayAlpha(t MemoryType) float64 {
	switch t {
	case MemoryEpisodic:
		return 0.08
	case MemorySemantic:
		return 0.02
	case MemoryProcedural:
		return 0.001
	case MemoryWorking:
		return 999.0
	default:
		return 0.05
	}
}
