package client

// This file re-exports internal types that callers outside the module need
// when constructing WriteRequest / calling Namespace(). Using type aliases
// ensures the re-exported types are identical to the originals — no
// conversion is required at call sites.

import (
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
)

// ── Namespace modes ───────────────────────────────────────────────────────────

// NamespaceMode selects scoring defaults and compaction strategy for a namespace.
type NamespaceMode = namespace.Mode

const (
	NSModeGeneral      NamespaceMode = namespace.ModeGeneral
	NSModeBeliefSystem NamespaceMode = namespace.ModeBeliefSystem
	NSModeAgentMemory  NamespaceMode = namespace.ModeAgentMemory
	NSModeProcedural   NamespaceMode = namespace.ModeProcedural
)

// ── Memory types ──────────────────────────────────────────────────────────────

// MemoryType controls the decay rate for agent-memory namespaces.
type MemoryType = core.MemoryType

const (
	MemEpisodic   MemoryType = core.MemoryEpisodic   // events; decays in ~hours/days
	MemSemantic   MemoryType = core.MemorySemantic   // facts; decays in weeks
	MemProcedural MemoryType = core.MemoryProcedural // skills; decays in months
	MemWorking    MemoryType = core.MemoryWorking    // task context; near-instant decay
	MemGeneral    MemoryType = core.MemoryGeneral    // untyped
)

// ── Node access ───────────────────────────────────────────────────────────────

// Node is the graph node type, re-exported so callers can reference its
// fields (ID, Labels, Properties, Confidence, TxTime, etc.) without
// importing the internal/core package.
type Node = core.Node
