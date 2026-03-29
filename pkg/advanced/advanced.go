// Package advanced re-exports internal contextdb types and constructors
// for use by companion applications that embed contextdb as a library.
//
// This is a thin facade -- it adds no logic, just makes internal types
// accessible outside the module boundary.
package advanced

import (
	"context"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/compact"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/dsl"
	"github.com/antiartificial/contextdb/internal/ingest"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/observe"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/pkg/client"
)

// -- Store interfaces ---------------------------------------------------------

type GraphStore = store.GraphStore
type VectorIndex = store.VectorIndex
type KVStore = store.KVStore
type EventLog = store.EventLog

// -- Core types ---------------------------------------------------------------

type Node = core.Node
type Edge = core.Edge
type Source = core.Source
type ScoredNode = core.ScoredNode
type ScoreParams = core.ScoreParams
type VectorEntry = core.VectorEntry
type MemoryType = core.MemoryType
type DecayConfig = core.DecayConfig

const (
	MemoryEpisodic   = core.MemoryEpisodic
	MemorySemantic   = core.MemorySemantic
	MemoryProcedural = core.MemoryProcedural
	MemoryWorking    = core.MemoryWorking
	MemoryGeneral    = core.MemoryGeneral
)

const (
	EpistemicAssertion   = core.EpistemicAssertion
	EpistemicObservation = core.EpistemicObservation
	EpistemicInference   = core.EpistemicInference
)

const (
	EdgeRelatesTo   = core.EdgeRelatesTo
	EdgeContradicts = core.EdgeContradicts
	EdgeSupersedes  = core.EdgeSupersedes
	EdgeEndorsedBy  = core.EdgeEndorsedBy
	EdgeDerivedFrom = core.EdgeDerivedFrom
	EdgeSupports    = core.EdgeSupports
	EdgeCites       = core.EdgeCites
	EdgeRefines     = core.EdgeRefines
	EdgeGeneralizes = core.EdgeGeneralizes
	EdgeIsExampleOf = core.EdgeIsExampleOf
	EdgeRetracted   = core.EdgeRetracted
)

var (
	NodeText           = core.NodeText
	CosineSimilarity   = core.CosineSimilarity
	DecayAlpha         = core.DecayAlpha
	DefaultDecayConfig = core.DefaultDecayConfig
	DefaultSource      = core.DefaultSource
	BeliefSystemParams = core.BeliefSystemParams
	AgentMemoryParams  = core.AgentMemoryParams
	GeneralParams      = core.GeneralParams
	ProceduralParams   = core.ProceduralParams
)

// -- Namespace modes ----------------------------------------------------------

type NamespaceMode = namespace.Mode

const (
	ModeBeliefSystem = namespace.ModeBeliefSystem
	ModeAgentMemory  = namespace.ModeAgentMemory
	ModeGeneral      = namespace.ModeGeneral
	ModeProcedural   = namespace.ModeProcedural
)

// -- Belief diff --------------------------------------------------------------

type BeliefDiff = retrieval.BeliefDiff
type BeliefConflict = retrieval.BeliefConflict
type ConflictSide = retrieval.ConflictSide

func ComputeBeliefDiff(ctx context.Context, graph GraphStore, ns string, nodeIDs []uuid.UUID) (*BeliefDiff, error) {
	return retrieval.ComputeBeliefDiff(ctx, graph, ns, nodeIDs)
}

// -- Narrative -----------------------------------------------------------------

type NarrativeFormatter = retrieval.NarrativeFormatter
type NarrativeReport = retrieval.NarrativeReport
type CitedClaim = retrieval.CitedClaim
type GroundingResult = retrieval.GroundingResult

func NewNarrativeFormatter(graph GraphStore, vecs VectorIndex) *NarrativeFormatter {
	return retrieval.NewNarrativeFormatter(graph, vecs)
}

// -- Knowledge gaps -----------------------------------------------------------

type GapDetector = retrieval.GapDetector
type KnowledgeGap = retrieval.KnowledgeGap
type GapQuery = retrieval.GapQuery
type GapReport = retrieval.GapReport

func NewGapDetector(graph GraphStore, vecs VectorIndex) *GapDetector {
	return retrieval.NewGapDetector(graph, vecs)
}

func BuildGapReport(ns string, gaps []KnowledgeGap, totalNodes int) *GapReport {
	return retrieval.BuildGapReport(ns, gaps, totalNodes)
}

// -- Active learning ----------------------------------------------------------

type ActiveLearner = retrieval.ActiveLearner
type AcquisitionSuggestion = retrieval.AcquisitionSuggestion
type AcquisitionType = retrieval.AcquisitionType

const (
	AcquireVerifyClaim   = retrieval.AcquireVerifyClaim
	AcquireRefreshStale  = retrieval.AcquireRefreshStale
	AcquireLowConfidence = retrieval.AcquireLowConfidence
	AcquireHighUtility   = retrieval.AcquireHighUtility
)

func NewActiveLearner(graph GraphStore) *ActiveLearner {
	return retrieval.NewActiveLearner(graph)
}

// -- Conflict clusters --------------------------------------------------------

type ConflictCluster = retrieval.ConflictCluster

func FindConflictClusters(ctx context.Context, graph GraphStore, ns string, nodeIDs []uuid.UUID) ([]ConflictCluster, error) {
	return retrieval.FindConflictClusters(ctx, graph, ns, nodeIDs)
}

// -- Inference chain ----------------------------------------------------------

type InferenceChain = retrieval.InferenceChain
type InferenceLink = retrieval.InferenceLink

func TraceInferenceChain(ctx context.Context, graph GraphStore, ns string, nodeID uuid.UUID, maxDepth int) (*InferenceChain, error) {
	return retrieval.TraceInferenceChain(ctx, graph, ns, nodeID, maxDepth)
}

// -- DSL ----------------------------------------------------------------------

type Query = dsl.Query
type Predicate = dsl.Predicate
type ScoreWeights = dsl.ScoreWeights

func ParsePipe(input string) (*Query, error)           { return dsl.ParsePipe(input) }
func ParseCQL(input string) (*Query, error)             { return dsl.ParseCQL(input) }
func ToRetrieveRequest(q *Query) client.RetrieveRequest { return dsl.ToRetrieveRequest(q) }

// -- Retraction ---------------------------------------------------------------

type BulkRetractor = compact.BulkRetractor
type RetractResult = compact.RetractResult

func NewBulkRetractor(graph GraphStore) *BulkRetractor {
	return compact.NewBulkRetractor(graph)
}

// -- GDPR ---------------------------------------------------------------------

type GDPRProcessor = compact.GDPRProcessor
type ErasureRequest = compact.ErasureRequest
type ErasureReport = compact.ErasureReport

func NewGDPRProcessor(graph GraphStore, vecs VectorIndex, kv KVStore, log EventLog) *GDPRProcessor {
	return compact.NewGDPRProcessor(graph, vecs, kv, log)
}

// -- Conflict detection -------------------------------------------------------

type ConflictDetector = ingest.ConflictDetector
type DetectResult = ingest.DetectResult

// NewConflictDetector creates a heuristic-only conflict detector.
func NewConflictDetector(graph GraphStore) *ConflictDetector {
	return ingest.NewConflictDetector(graph, nil)
}

// -- Calibration --------------------------------------------------------------

type PredictionOutcome = observe.PredictionOutcome
type PlattScaler = observe.PlattScaler
type IsotonicRegressor = observe.IsotonicRegressor

func BrierScore(predictions []PredictionOutcome) float64 {
	return observe.BrierScore(predictions)
}

func ExpectedCalibrationError(predictions []PredictionOutcome, bins int) float64 {
	return observe.ExpectedCalibrationError(predictions, bins)
}

func MaxCalibrationError(predictions []PredictionOutcome, bins int) float64 {
	return observe.MaxCalibrationError(predictions, bins)
}

// -- Credibility helpers ------------------------------------------------------

func MeanCredibility(alpha, beta float64) float64 {
	return ingest.MeanCredibility(alpha, beta)
}

func CredibilityVariance(alpha, beta float64) float64 {
	return ingest.CredibilityVariance(alpha, beta)
}
