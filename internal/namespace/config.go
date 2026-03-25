package namespace

import (
	"github.com/ataraxy-labs/contextdb/internal/core"
	"github.com/ataraxy-labs/contextdb/internal/store"
)

// Mode declares the operational pattern for a namespace.
// It selects default scoring params, compaction strategy, and
// admission thresholds. All defaults can be overridden per-query.
type Mode string

const (
	// ModeBeliefSystem weights credibility heavily. Designed for
	// multi-source fact tracking with poisoning resistance.
	ModeBeliefSystem Mode = "belief_system"

	// ModeAgentMemory weights utility (task outcome feedback) and
	// recency. Designed for agentic memory with episodic abstraction.
	ModeAgentMemory Mode = "agent_memory"

	// ModeGeneral provides balanced defaults for most use cases.
	ModeGeneral Mode = "general"

	// ModeProcedural emphasises confidence and slow decay, suitable
	// for skill/workflow storage where learned procedures should persist.
	ModeProcedural Mode = "procedural"
)

// Config holds the namespace-level configuration.
type Config struct {
	ID   string
	Mode Mode

	// Scoring defaults — overridable per query.
	ScoreParams core.ScoreParams

	// Traversal defaults.
	Traversal store.TraversalStrategy
	MaxDepth  int

	// Admission threshold: candidates scoring below this are rejected
	// at ingest time. Range [0, 1].
	AdmitThreshold float64

	// Compaction settings.
	CompactionEnabled bool
	CompactionWorker  string // "raptor" | "none"
}

// Defaults returns a Config populated with sensible defaults for the
// given mode. Callers customise from here.
func Defaults(id string, m Mode) Config {
	switch m {
	case ModeBeliefSystem:
		return Config{
			ID:                id,
			Mode:              ModeBeliefSystem,
			ScoreParams:       core.BeliefSystemParams(),
			Traversal:         store.StrategyWaterCircle,
			MaxDepth:          4,
			AdmitThreshold:    0.15, // low bar — credibility gates retrieval
			CompactionEnabled: false,
		}

	case ModeAgentMemory:
		return Config{
			ID:                id,
			Mode:              ModeAgentMemory,
			ScoreParams:       core.AgentMemoryParams(),
			Traversal:         store.StrategyBeam,
			MaxDepth:          2,
			AdmitThreshold:    0.35, // stricter — avoid storing low-value episodes
			CompactionEnabled: true,
			CompactionWorker:  "raptor",
		}

	case ModeProcedural:
		return Config{
			ID:                id,
			Mode:              ModeProcedural,
			ScoreParams:       core.ProceduralParams(),
			Traversal:         store.StrategyBFS,
			MaxDepth:          3,
			AdmitThreshold:    0.40,
			CompactionEnabled: false,
		}

	default: // ModeGeneral
		return Config{
			ID:                id,
			Mode:              ModeGeneral,
			ScoreParams:       core.GeneralParams(),
			Traversal:         store.StrategyWaterCircle,
			MaxDepth:          3,
			AdmitThreshold:    0.25,
			CompactionEnabled: false,
		}
	}
}
