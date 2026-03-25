// Package observe — instrumented wrappers around retrieval and ingest.
package observe

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/retrieval"
)

// Metrics holds all contextdb metric instruments. Initialise once with
// [NewMetrics] and keep a single instance for the process lifetime.
type Metrics struct {
	// Retrieval
	RetrievalTotal    *Counter   // total retrieve calls
	RetrievalErrors   *Counter   // retrieve calls that returned an error
	RetrievalLatency  *Histogram // retrieve duration (µs)
	RetrievalTopScore *Gauge     // last top-1 score (sanity check)
	RetrievalResults  *Gauge     // last result count

	// Per-source counters — how many results came from each path
	VectorHits *Counter
	GraphHits  *Counter
	FusedHits  *Counter

	// Ingest
	IngestTotal    *Counter
	IngestAdmitted *Counter
	IngestRejected *Counter
	IngestConflict *Counter
	IngestLatency  *Histogram

	// Admission
	AdmissionTrollRejected    *Counter
	AdmissionDuplicateSkipped *Counter
	AdmissionThresholdFailed  *Counter

	// Store operations
	GraphUpsertTotal   *Counter
	GraphUpsertLatency *Histogram
	GraphWalkTotal     *Counter
	GraphWalkLatency   *Histogram
	VectorIndexTotal   *Counter
	VectorIndexLatency *Histogram
	VectorSearchTotal  *Counter
	VectorSearchLatency *Histogram

	// Namespace
	ActiveNamespaces *Gauge
	NodeCount        *Gauge // approximate, updated on writes

	// Scoring
	ScoreZeroCount *Counter // nodes that scored 0 (filtered out)
}

// NewMetrics registers all metric instruments against the given registry
// and returns the populated Metrics struct.
func NewMetrics(r *Registry) *Metrics {
	if r == nil {
		r = Default
	}
	m := &Metrics{
		// Retrieval
		RetrievalTotal:   r.NewCounter("contextdb_retrieval_total", "Total hybrid retrieve calls"),
		RetrievalErrors:  r.NewCounter("contextdb_retrieval_errors_total", "Retrieve calls that returned an error"),
		RetrievalLatency: r.NewHistogram("contextdb_retrieval_duration_us", "Retrieve call duration (microseconds)", DefaultLatencyBuckets),
		RetrievalTopScore: r.NewGauge("contextdb_retrieval_top_score", "Top-1 score of the most recent retrieve call"),
		RetrievalResults:  r.NewGauge("contextdb_retrieval_result_count", "Result count of the most recent retrieve call"),

		VectorHits: r.NewCounter("contextdb_retrieval_vector_hits_total", "Results sourced from vector ANN path"),
		GraphHits:  r.NewCounter("contextdb_retrieval_graph_hits_total", "Results sourced from graph walk path"),
		FusedHits:  r.NewCounter("contextdb_retrieval_fused_hits_total", "Results sourced from both paths (fused)"),

		// Ingest
		IngestTotal:    r.NewCounter("contextdb_ingest_total", "Total ingest calls"),
		IngestAdmitted: r.NewCounter("contextdb_ingest_admitted_total", "Ingest calls admitted to the graph"),
		IngestRejected: r.NewCounter("contextdb_ingest_rejected_total", "Ingest calls rejected by admission gate"),
		IngestConflict: r.NewCounter("contextdb_ingest_conflict_total", "Ingest calls that detected a contradiction"),
		IngestLatency:  r.NewHistogram("contextdb_ingest_duration_us", "Ingest call duration (microseconds)", DefaultLatencyBuckets),

		// Admission
		AdmissionTrollRejected:    r.NewCounter("contextdb_admission_troll_rejected_total", "Nodes rejected due to credibility floor"),
		AdmissionDuplicateSkipped: r.NewCounter("contextdb_admission_duplicate_skipped_total", "Nodes skipped as near-duplicates"),
		AdmissionThresholdFailed:  r.NewCounter("contextdb_admission_threshold_failed_total", "Nodes below novelty×credibility threshold"),

		// Store
		GraphUpsertTotal:    r.NewCounter("contextdb_graph_upsert_total", "Graph node/edge upsert calls"),
		GraphUpsertLatency:  r.NewHistogram("contextdb_graph_upsert_duration_us", "Graph upsert duration (microseconds)", DefaultLatencyBuckets),
		GraphWalkTotal:      r.NewCounter("contextdb_graph_walk_total", "Graph walk calls"),
		GraphWalkLatency:    r.NewHistogram("contextdb_graph_walk_duration_us", "Graph walk duration (microseconds)", DefaultLatencyBuckets),
		VectorIndexTotal:    r.NewCounter("contextdb_vector_index_total", "Vector index calls"),
		VectorIndexLatency:  r.NewHistogram("contextdb_vector_index_duration_us", "Vector index duration (microseconds)", DefaultLatencyBuckets),
		VectorSearchTotal:   r.NewCounter("contextdb_vector_search_total", "Vector ANN search calls"),
		VectorSearchLatency: r.NewHistogram("contextdb_vector_search_duration_us", "Vector ANN search duration (microseconds)", DefaultLatencyBuckets),

		// Namespace
		ActiveNamespaces: r.NewGauge("contextdb_active_namespaces", "Number of active namespaces"),
		NodeCount:        r.NewGauge("contextdb_node_count", "Approximate total node count across all namespaces"),

		// Scoring
		ScoreZeroCount: r.NewCounter("contextdb_score_zero_total", "Nodes filtered out with score=0 during retrieval"),
	}
	return m
}

// ─── Instrumented retrieval engine ───────────────────────────────────────────

// InstrumentedEngine wraps retrieval.Engine with metric recording and
// structured logging on every operation.
type InstrumentedEngine struct {
	engine  *retrieval.Engine
	metrics *Metrics
	log     *slog.Logger
}

// NewInstrumentedEngine wraps an existing Engine with observability.
func NewInstrumentedEngine(e *retrieval.Engine, m *Metrics, log *slog.Logger) *InstrumentedEngine {
	if log == nil {
		log = slog.Default()
	}
	return &InstrumentedEngine{engine: e, metrics: m, log: log}
}

// Retrieve calls the underlying engine and records metrics.
func (ie *InstrumentedEngine) Retrieve(ctx context.Context, q retrieval.Query) ([]core.ScoredNode, error) {
	start := time.Now()
	ie.metrics.RetrievalTotal.Inc()

	results, err := ie.engine.Retrieve(ctx, q)

	elapsed := time.Since(start)
	ie.metrics.RetrievalLatency.ObserveDuration(elapsed)

	if err != nil {
		ie.metrics.RetrievalErrors.Inc()
		ie.log.Error("retrieve failed",
			"namespace", q.Namespace,
			"error", err,
			"elapsed_us", elapsed.Microseconds())
		return nil, err
	}

	// Record result stats
	ie.metrics.RetrievalResults.Set(float64(len(results)))
	if len(results) > 0 {
		ie.metrics.RetrievalTopScore.Set(results[0].Score)
	}

	// Count results by source
	for _, r := range results {
		switch {
		case r.RetrievalSource == "vector":
			ie.metrics.VectorHits.Inc()
		case r.RetrievalSource == "graph":
			ie.metrics.GraphHits.Inc()
		default:
			ie.metrics.FusedHits.Inc()
		}
	}

	// Log at debug level with score breakdown
	if ie.log.Enabled(ctx, slog.LevelDebug) {
		fields := RetrievalLogFields(
			q.Namespace,
			len(results),
			topScore(results),
			elapsed,
			topSource(results),
		)
		ie.log.Debug("retrieve", fields...)
	}

	return results, nil
}

// ─── Score distribution reporter ─────────────────────────────────────────────

// ScoreReport holds aggregate score statistics across a set of results.
// Useful for debugging weight tuning — log this periodically.
type ScoreReport struct {
	Count   int
	Min     float64
	Max     float64
	Mean    float64
	P50     float64
	P95     float64
	ZeroCount int // results that scored exactly 0 (filtered before TopK)
}

// ReportScores computes a ScoreReport from a result slice.
// Also accepts a pre-TopK candidate slice to count zeros.
func ReportScores(results []core.ScoredNode, zeroCount int) ScoreReport {
	if len(results) == 0 {
		return ScoreReport{ZeroCount: zeroCount}
	}

	scores := make([]float64, len(results))
	sum := 0.0
	min := results[0].Score
	max := results[0].Score

	for i, r := range results {
		scores[i] = r.Score
		sum += r.Score
		if r.Score < min {
			min = r.Score
		}
		if r.Score > max {
			max = r.Score
		}
	}

	// Simple percentile from sorted slice
	sorted := make([]float64, len(scores))
	copy(sorted, scores)
	sortFloat64s(sorted)

	return ScoreReport{
		Count:     len(results),
		Min:       min,
		Max:       max,
		Mean:      sum / float64(len(results)),
		P50:       percentile(sorted, 50),
		P95:       percentile(sorted, 95),
		ZeroCount: zeroCount,
	}
}

// LogScoreReport logs a ScoreReport at debug level.
func LogScoreReport(log *slog.Logger, ns string, report ScoreReport) {
	log.Debug("score distribution",
		"namespace", ns,
		"count", report.Count,
		"min", f4(report.Min),
		"max", f4(report.Max),
		"mean", f4(report.Mean),
		"p50", f4(report.P50),
		"p95", f4(report.P95),
		"zero_count", report.ZeroCount,
	)
}

func f4(v float64) string {
	return fmt.Sprintf("%.4f", v)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func topScore(results []core.ScoredNode) float64 {
	if len(results) == 0 {
		return 0
	}
	return results[0].Score
}

func topSource(results []core.ScoredNode) string {
	if len(results) == 0 {
		return "none"
	}
	return results[0].RetrievalSource
}

func percentile(sorted []float64, pct float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(pct / 100 * float64(len(sorted)-1))
	return sorted[idx]
}

func sortFloat64s(s []float64) {
	// insertion sort — fine for small slices (TopK is typically ≤ 20)
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}
