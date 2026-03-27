package dsl

import "time"

// Query is the shared AST produced by both the pipe and CQL parsers.
// It maps 1:1 to a RetrieveRequest after conversion.
type Query struct {
	SearchText string
	Namespace  string
	Mode       string // "general", "agent_memory", "belief_system", "procedural"
	ValidAt    *time.Time
	KnownAt    *time.Time
	Predicates []Predicate
	Weights    ScoreWeights
	Graph      *GraphOpts
	Limit      int
	Rerank     bool
	RerankHint string   // optional model hint (CQL only)
	Return           []string // projection field names; nil = all
	ExcludeSourceIDs []string // source IDs to exclude from results
}

// Predicate is a single filter condition in a WHERE / where clause.
type Predicate struct {
	Field      string
	Op         CompareOp
	Value      Value
	UpperBound Value // only for OpBetween
}

// CompareOp enumerates comparison operators.
type CompareOp int

const (
	OpEq CompareOp = iota
	OpNeq
	OpGt
	OpGte
	OpLt
	OpLte
	OpLike
	OpBetween
	OpIn
	OpIsNull
	OpIsNotNull
)

func (op CompareOp) String() string {
	switch op {
	case OpEq:
		return "="
	case OpNeq:
		return "!="
	case OpGt:
		return ">"
	case OpGte:
		return ">="
	case OpLt:
		return "<"
	case OpLte:
		return "<="
	case OpLike:
		return "LIKE"
	case OpBetween:
		return "BETWEEN"
	case OpIn:
		return "IN"
	case OpIsNull:
		return "IS NULL"
	case OpIsNotNull:
		return "IS NOT NULL"
	default:
		return "?"
	}
}

// Value holds a typed literal from a query.
type Value struct {
	Type    ValueType
	Str     string
	Num     float64
	Time    time.Time
	Strings []string // for IN lists
}

// ValueType tags which field in Value is populated.
type ValueType int

const (
	ValString ValueType = iota
	ValNumber
	ValTime
	ValNow
	ValStringList
)

// ScoreWeights stores the four tuning dimensions.
// A negative value means "not set — use default".
type ScoreWeights struct {
	Similarity float64
	Confidence float64
	Recency    float64
	Utility    float64
}

// NoWeight is the sentinel for "not specified".
const NoWeight = -1.0

// DefaultWeights returns weights with all dimensions unset.
func DefaultWeights() ScoreWeights {
	return ScoreWeights{
		Similarity: NoWeight,
		Confidence: NoWeight,
		Recency:    NoWeight,
		Utility:    NoWeight,
	}
}

// GraphOpts controls graph traversal from the parsed query.
type GraphOpts struct {
	Edges []EdgeSpec
}

// EdgeSpec is a single FOLLOW / expand directive.
type EdgeSpec struct {
	Type     string // "contradicts", "supports", "derives_from", "cites"
	MaxDepth int    // 0 = default
}

// ResolvePreset converts a named preset to a float64 weight.
func ResolvePreset(name string) (float64, bool) {
	switch name {
	case "high":
		return 0.8, true
	case "medium":
		return 0.5, true
	case "low":
		return 0.2, true
	case "off":
		return 0.0, true
	default:
		return 0, false
	}
}
