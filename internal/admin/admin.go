// Package admin provides a minimal HTML dashboard for contextdb.
//
// Mount the handler at /admin/ to serve the dashboard.
//
//	mux.Handle("/admin/", admin.New(db))
package admin

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/buildinfo"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/observe"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/pkg/client"
	"github.com/antiartificial/contextdb/testdata"
)

// adminHandler serves the admin dashboard.
type adminHandler struct {
	db    *client.DB
	graph store.GraphStore
	mux   *http.ServeMux
}

//go:embed dist/index.html dist/assets/*
var adminDist embed.FS

// New creates an http.Handler that serves the admin UI at /admin/.
func New(db *client.DB) http.Handler {
	graph, _, _, _ := db.Stores()
	h := &adminHandler{
		db:    db,
		graph: graph,
		mux:   http.NewServeMux(),
	}
	staticFS, err := fs.Sub(adminDist, "dist")
	if err == nil {
		h.mux.Handle("GET /admin/assets/", http.StripPrefix("/admin/", http.FileServer(http.FS(staticFS))))
	}

	h.mux.HandleFunc("GET /admin/", h.handleIndex)
	h.mux.HandleFunc("GET /admin/debugger", h.handleIndex)
	h.mux.HandleFunc("GET /admin/api/stats", h.handleStats)
	h.mux.HandleFunc("GET /admin/api/metrics", h.handleMetrics)
	h.mux.HandleFunc("GET /admin/api/ranking-eval", h.handleRankingEval)
	h.mux.HandleFunc("GET /admin/api/belief", h.handleBeliefAudit)
	h.mux.HandleFunc("POST /admin/api/explain-rank", h.handleExplainRank)
	h.mux.HandleFunc("GET /admin/api/search", h.handleSearch)
	h.mux.HandleFunc("GET /admin/api/timetravel", h.handleTimeTravel)
	h.mux.HandleFunc("GET /admin/api/diff", h.handleDiff)

	return h
}

type adminRankingEvalReport struct {
	SchemaVersion    int                        `json:"schema_version"`
	GeneratedAt      string                     `json:"generated_at"`
	ContextDBVersion string                     `json:"contextdb_version"`
	Corpus           string                     `json:"corpus"`
	TopK             int                        `json:"top_k"`
	TotalQueries     int                        `json:"total_queries"`
	PassedQueries    int                        `json:"passed_queries"`
	FailedQueries    int                        `json:"failed_queries"`
	MeanReciprocal   float64                    `json:"mean_reciprocal_rank"`
	Categories       []adminRankingEvalCategory `json:"categories"`
	Queries          []adminRankingEvalQuery    `json:"queries"`
}

type adminRankingEvalCategory struct {
	Category       string  `json:"category"`
	TotalQueries   int     `json:"total_queries"`
	PassedQueries  int     `json:"passed_queries"`
	FailedQueries  int     `json:"failed_queries"`
	PassRate       float64 `json:"pass_rate"`
	MeanReciprocal float64 `json:"mean_reciprocal_rank"`
}

type adminRankingEvalQuery struct {
	ID                 string                   `json:"id"`
	Description        string                   `json:"description"`
	Namespace          string                   `json:"namespace"`
	Category           string                   `json:"category"`
	ExpectedRankCutoff int                      `json:"expected_rank_cutoff"`
	CorrectRank        int                      `json:"correct_rank,omitempty"`
	ReciprocalRank     float64                  `json:"reciprocal_rank"`
	Passed             bool                     `json:"passed"`
	TopResults         []adminRankingEvalResult `json:"top_results"`
}

type adminRankingEvalResult struct {
	Rank            int                 `json:"rank"`
	NodeID          string              `json:"node_id"`
	Text            string              `json:"text,omitempty"`
	Expected        bool                `json:"expected"`
	Score           float64             `json:"score"`
	SimilarityScore float64             `json:"similarity_score"`
	ConfidenceScore float64             `json:"confidence_score"`
	RecencyScore    float64             `json:"recency_score"`
	UtilityScore    float64             `json:"utility_score"`
	ScoreBreakdown  core.ScoreBreakdown `json:"score_breakdown"`
	RetrievalSource string              `json:"retrieval_source,omitempty"`
}

type adminBeliefAuditResponse struct {
	Node              core.Node                 `json:"Node"`
	Source            *core.Source              `json:"Source"`
	Supporters        []observe.EvidenceNode    `json:"Supporters"`
	Contradictors     []observe.EvidenceNode    `json:"Contradictors"`
	ProvenanceChain   []observe.EvidenceNode    `json:"ProvenanceChain"`
	ConfidenceHistory []observe.ConfidencePoint `json:"ConfidenceHistory"`
	AuditedAt         time.Time                 `json:"AuditedAt"`
	Epistemics        adminEpistemicsSummary    `json:"epistemics"`
}

type adminEpistemicsSummary struct {
	NodeID              string                         `json:"node_id"`
	Namespace           string                         `json:"namespace"`
	Text                string                         `json:"text,omitempty"`
	Labels              []string                       `json:"labels,omitempty"`
	Source              adminSourceContext             `json:"source"`
	ConfidenceTimeline  []adminConfidenceTimelinePoint `json:"confidence_timeline"`
	SourceTrustTimeline []adminSourceTrustPoint        `json:"source_trust_timeline"`
	ContradictionPaths  []adminContradictionPath       `json:"contradiction_paths"`
	GraphContext        []adminGraphContextNode        `json:"graph_context"`
	Counts              adminEpistemicsCounts          `json:"counts"`
}

type adminSourceContext struct {
	ID                   string   `json:"id,omitempty"`
	ExternalID           string   `json:"external_id,omitempty"`
	Labels               []string `json:"labels,omitempty"`
	EffectiveCredibility float64  `json:"effective_credibility"`
	CredibilityVariance  float64  `json:"credibility_variance"`
	Alpha                float64  `json:"alpha,omitempty"`
	Beta                 float64  `json:"beta,omitempty"`
	ClaimsAsserted       int64    `json:"claims_asserted,omitempty"`
	ClaimsValidated      int64    `json:"claims_validated,omitempty"`
	ClaimsRefuted        int64    `json:"claims_refuted,omitempty"`
	UpdatedAt            string   `json:"updated_at,omitempty"`
}

type adminConfidenceTimelinePoint struct {
	Time       string  `json:"time"`
	Confidence float64 `json:"confidence"`
	Version    uint64  `json:"version"`
}

type adminSourceTrustPoint struct {
	Time              string  `json:"time"`
	SourceID          string  `json:"source_id"`
	NodeID            string  `json:"node_id"`
	Action            string  `json:"action"`
	SourceCredibility float64 `json:"source_credibility"`
	Reason            string  `json:"reason,omitempty"`
}

type adminContradictionPath struct {
	Direction     string  `json:"direction"`
	Relation      string  `json:"relation"`
	ClaimID       string  `json:"claim_id"`
	ClaimText     string  `json:"claim_text,omitempty"`
	OtherNodeID   string  `json:"other_node_id"`
	OtherText     string  `json:"other_text,omitempty"`
	OtherSourceID string  `json:"other_source_id,omitempty"`
	EdgeID        string  `json:"edge_id"`
	EdgeWeight    float64 `json:"edge_weight"`
	Severity      string  `json:"severity"`
	ConfidenceGap float64 `json:"confidence_gap"`
}

type adminGraphContextNode struct {
	NodeID     string   `json:"node_id"`
	Text       string   `json:"text,omitempty"`
	SourceID   string   `json:"source_id,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	Relation   string   `json:"relation"`
	Direction  string   `json:"direction"`
	EdgeType   string   `json:"edge_type"`
	EdgeWeight float64  `json:"edge_weight"`
	Confidence float64  `json:"confidence"`
	ValidFrom  string   `json:"valid_from,omitempty"`
}

type adminEpistemicsCounts struct {
	Supporters         int `json:"supporters"`
	Contradictors      int `json:"contradictors"`
	Provenance         int `json:"provenance"`
	GraphNeighbors     int `json:"graph_neighbors"`
	SourceTrustPoints  int `json:"source_trust_points"`
	ConfidenceVersions int `json:"confidence_versions"`
}

func (h *adminHandler) handleRankingEval(w http.ResponseWriter, r *http.Request) {
	topK, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("top_k")))
	if err != nil || topK <= 0 {
		topK = 5
	}
	if topK > 25 {
		http.Error(w, "top_k must be 25 or less", http.StatusBadRequest)
		return
	}
	report, err := buildAdminRankingEvalReport(r.Context(), topK, time.Now().UTC())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}

func buildAdminRankingEvalReport(ctx context.Context, topK int, generatedAt time.Time) (adminRankingEvalReport, error) {
	if topK <= 0 {
		topK = 5
	}
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	corpus := testdata.Build()
	engine := retrieval.Engine{
		Graph:   corpus.Graph,
		Vectors: corpus.Vecs,
		KV:      corpus.KV,
	}
	report := adminRankingEvalReport{
		SchemaVersion:    1,
		GeneratedAt:      generatedAt.UTC().Format(time.RFC3339),
		ContextDBVersion: buildinfo.Version,
		Corpus:           "representative",
		TopK:             topK,
		TotalQueries:     len(corpus.QuerySet),
	}
	reciprocalSum := 0.0
	categoryStats := map[string]*adminRankingEvalCategory{}
	for _, query := range corpus.QuerySet {
		cfg := namespace.Defaults(query.Namespace, adminRankingEvalCorpusMode(query.Namespace))
		results, err := engine.Retrieve(ctx, retrieval.Query{
			Namespace:   query.Namespace,
			Vector:      query.Vector,
			TopK:        topK,
			Strategy:    retrieval.HybridStrategy{VectorWeight: 1, Traversal: cfg.Traversal, MaxDepth: cfg.MaxDepth},
			ScoreParams: cfg.ScoreParams,
		})
		if err != nil {
			return report, fmt.Errorf("ranking eval %s: %w", query.ID, err)
		}
		queryReport := adminRankingEvalQuery{
			ID:                 query.ID,
			Description:        query.Description,
			Namespace:          query.Namespace,
			Category:           query.Category,
			ExpectedRankCutoff: adminRankingEvalExpectedRankCutoff(query.Category),
		}
		if queryReport.ExpectedRankCutoff > len(results) {
			queryReport.ExpectedRankCutoff = len(results)
		}
		for i, result := range results {
			rank := i + 1
			expected := adminRankingEvalContainsNode(query.CorrectNodeIDs, result.Node.ID)
			if expected && queryReport.CorrectRank == 0 {
				queryReport.CorrectRank = rank
				queryReport.ReciprocalRank = 1 / float64(rank)
				reciprocalSum += queryReport.ReciprocalRank
			}
			text, _ := result.Node.Properties["text"].(string)
			queryReport.TopResults = append(queryReport.TopResults, adminRankingEvalResult{
				Rank:            rank,
				NodeID:          result.Node.ID.String(),
				Text:            text,
				Expected:        expected,
				Score:           result.Score,
				SimilarityScore: result.SimilarityScore,
				ConfidenceScore: result.ConfidenceScore,
				RecencyScore:    result.RecencyScore,
				UtilityScore:    result.UtilityScore,
				ScoreBreakdown:  result.Breakdown,
				RetrievalSource: result.RetrievalSource,
			})
		}
		queryReport.Passed = queryReport.CorrectRank > 0 && queryReport.CorrectRank <= queryReport.ExpectedRankCutoff
		if queryReport.Passed {
			report.PassedQueries++
		}
		report.Queries = append(report.Queries, queryReport)
		stats := categoryStats[queryReport.Category]
		if stats == nil {
			stats = &adminRankingEvalCategory{Category: queryReport.Category}
			categoryStats[queryReport.Category] = stats
		}
		stats.TotalQueries++
		if queryReport.Passed {
			stats.PassedQueries++
		}
		stats.MeanReciprocal += queryReport.ReciprocalRank
	}
	report.FailedQueries = report.TotalQueries - report.PassedQueries
	if report.TotalQueries > 0 {
		report.MeanReciprocal = reciprocalSum / float64(report.TotalQueries)
	}
	for _, stats := range categoryStats {
		stats.FailedQueries = stats.TotalQueries - stats.PassedQueries
		stats.PassRate = ratio(float64(stats.PassedQueries), float64(stats.TotalQueries))
		stats.MeanReciprocal = ratio(stats.MeanReciprocal, float64(stats.TotalQueries))
		report.Categories = append(report.Categories, *stats)
	}
	sort.SliceStable(report.Categories, func(i, j int) bool {
		return report.Categories[i].Category < report.Categories[j].Category
	})
	return report, nil
}

func adminRankingEvalExpectedRankCutoff(category string) int {
	switch category {
	case "poisoning", "temporal", "procedural":
		return 1
	default:
		return 3
	}
}

func adminRankingEvalCorpusMode(ns string) namespace.Mode {
	switch ns {
	case testdata.NSChannel:
		return namespace.ModeBeliefSystem
	case testdata.NSAgent:
		return namespace.ModeAgentMemory
	case testdata.NSProcedural:
		return namespace.ModeProcedural
	default:
		return namespace.ModeGeneral
	}
}

func adminRankingEvalContainsNode(ids []uuid.UUID, id uuid.UUID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func (h *adminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// parseTime parses an RFC3339 or YYYY-MM-DD formatted time string.
func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02", s)
	}
	return t, err
}

// handleIndex serves the main dashboard HTML page.
func (h *adminHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	index, err := adminDist.ReadFile("dist/index.html")
	if err != nil {
		http.Error(w, "admin UI is not built; run npm run admin:build", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(index)
}

// handleStats returns JSON stats for the dashboard.
func (h *adminHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := h.db.Stats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

type adminMetricsSnapshot struct {
	Mode        string                `json:"mode"`
	GeneratedAt string                `json:"generated_at"`
	Health      adminMetricsHealth    `json:"health"`
	Ingest      adminIngestMetrics    `json:"ingest"`
	Retrieval   adminRetrievalMetrics `json:"retrieval"`
	Latency     adminLatencyMetrics   `json:"latency"`
}

type adminMetricsHealth struct {
	Status  string   `json:"status"`
	Signals []string `json:"signals"`
}

type adminIngestMetrics struct {
	Total         int64   `json:"total"`
	Admitted      int64   `json:"admitted"`
	Rejected      int64   `json:"rejected"`
	AdmissionRate float64 `json:"admission_rate"`
	RejectionRate float64 `json:"rejection_rate"`
}

type adminRetrievalMetrics struct {
	Total     int64   `json:"total"`
	Errors    int64   `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
}

type adminLatencyMetrics struct {
	P50Us  float64 `json:"p50_us"`
	P95Us  float64 `json:"p95_us"`
	MeanUs float64 `json:"mean_us"`
	P50Ms  float64 `json:"p50_ms"`
	P95Ms  float64 `json:"p95_ms"`
	MeanMs float64 `json:"mean_ms"`
}

func (h *adminHandler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := buildAdminMetricsSnapshot(h.db.Stats(), time.Now().UTC())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

func buildAdminMetricsSnapshot(stats client.DBStats, generatedAt time.Time) adminMetricsSnapshot {
	ingestTotal := float64(stats.IngestTotal)
	retrievalTotal := float64(stats.RetrievalTotal)
	admissionRate := ratio(float64(stats.IngestAdmitted), ingestTotal)
	rejectionRate := ratio(float64(stats.IngestRejected), ingestTotal)
	errorRate := ratio(float64(stats.RetrievalErrors), retrievalTotal)
	status := "healthy"
	signals := []string{"admin metrics online"}
	if stats.RetrievalErrors > 0 {
		status = "degraded"
		signals = append(signals, "retrieval errors observed")
	}
	if stats.IngestRejected > 0 {
		if status == "healthy" {
			status = "watch"
		}
		signals = append(signals, "ingest rejections observed")
	}
	if stats.IngestTotal == 0 && stats.RetrievalTotal == 0 {
		signals = append(signals, "no traffic recorded yet")
	}
	return adminMetricsSnapshot{
		Mode:        string(stats.Mode),
		GeneratedAt: generatedAt.Format(time.RFC3339),
		Health: adminMetricsHealth{
			Status:  status,
			Signals: signals,
		},
		Ingest: adminIngestMetrics{
			Total:         stats.IngestTotal,
			Admitted:      stats.IngestAdmitted,
			Rejected:      stats.IngestRejected,
			AdmissionRate: admissionRate,
			RejectionRate: rejectionRate,
		},
		Retrieval: adminRetrievalMetrics{
			Total:     stats.RetrievalTotal,
			Errors:    stats.RetrievalErrors,
			ErrorRate: errorRate,
		},
		Latency: adminLatencyMetrics{
			P50Us:  stats.LatencyP50Us,
			P95Us:  stats.LatencyP95Us,
			MeanUs: stats.LatencyMeanUs,
			P50Ms:  stats.LatencyP50Us / 1000,
			P95Ms:  stats.LatencyP95Us / 1000,
			MeanMs: stats.LatencyMeanUs / 1000,
		},
	}
}

func ratio(numerator, denominator float64) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / denominator
}

type adminExplainRankRequest struct {
	Namespace   string    `json:"namespace"`
	NodeID      string    `json:"node_id"`
	OtherNodeID string    `json:"other_node_id"`
	Text        string    `json:"text,omitempty"`
	Vector      []float32 `json:"vector,omitempty"`
	MaxDepth    int       `json:"max_depth,omitempty"`
}

func (h *adminHandler) handleExplainRank(w http.ResponseWriter, r *http.Request) {
	var req adminExplainRankRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ns := strings.TrimSpace(req.Namespace)
	if ns == "" {
		http.Error(w, "missing namespace", http.StatusBadRequest)
		return
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(req.NodeID))
	if err != nil {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}
	otherNodeID, err := uuid.Parse(strings.TrimSpace(req.OtherNodeID))
	if err != nil {
		http.Error(w, "invalid other_node_id", http.StatusBadRequest)
		return
	}
	explanation, err := h.db.Namespace(ns, "").ExplainRank(r.Context(), client.ExplainRankRequest{
		NodeID:      nodeID,
		OtherNodeID: otherNodeID,
		Text:        req.Text,
		Vector:      req.Vector,
		MaxDepth:    req.MaxDepth,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(explanation)
}

// handleBeliefAudit returns the evidence trail for one claim.
// Query params:
//   - ns: namespace (required)
//   - id: node UUID (required)
func (h *adminHandler) handleBeliefAudit(w http.ResponseWriter, r *http.Request) {
	ns := strings.TrimSpace(r.URL.Query().Get("ns"))
	if ns == "" {
		http.Error(w, "missing ns parameter", http.StatusBadRequest)
		return
	}
	rawID := strings.TrimSpace(r.URL.Query().Get("id"))
	if rawID == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}
	nodeID, err := uuid.Parse(rawID)
	if err != nil {
		http.Error(w, "invalid id parameter", http.StatusBadRequest)
		return
	}

	audit, err := observe.AuditBelief(r.Context(), h.graph, ns, nodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if audit == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	response, err := h.buildBeliefAuditResponse(r.Context(), ns, audit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *adminHandler) buildBeliefAuditResponse(ctx context.Context, ns string, audit *observe.BeliefAudit) (adminBeliefAuditResponse, error) {
	summary, err := h.buildEpistemicsSummary(ctx, ns, audit)
	if err != nil {
		return adminBeliefAuditResponse{}, err
	}
	return adminBeliefAuditResponse{
		Node:              audit.Node,
		Source:            audit.Source,
		Supporters:        audit.Supporters,
		Contradictors:     audit.Contradictors,
		ProvenanceChain:   audit.ProvenanceChain,
		ConfidenceHistory: audit.ConfidenceHistory,
		AuditedAt:         audit.AuditedAt,
		Epistemics:        summary,
	}, nil
}

func (h *adminHandler) buildEpistemicsSummary(ctx context.Context, ns string, audit *observe.BeliefAudit) (adminEpistemicsSummary, error) {
	summary := adminEpistemicsSummary{
		NodeID:    audit.Node.ID.String(),
		Namespace: audit.Node.Namespace,
		Text:      core.NodeText(audit.Node),
		Labels:    audit.Node.Labels,
		Counts: adminEpistemicsCounts{
			Supporters:         len(audit.Supporters),
			Contradictors:      len(audit.Contradictors),
			Provenance:         len(audit.ProvenanceChain),
			ConfidenceVersions: len(audit.ConfidenceHistory),
		},
	}
	if audit.Source != nil {
		summary.Source = adminSourceContext{
			ID:                   audit.Source.ID.String(),
			ExternalID:           audit.Source.ExternalID,
			Labels:               audit.Source.Labels,
			EffectiveCredibility: audit.Source.EffectiveCredibility(),
			CredibilityVariance:  audit.Source.CredibilityVariance(),
			Alpha:                audit.Source.Alpha,
			Beta:                 audit.Source.Beta,
			ClaimsAsserted:       audit.Source.ClaimsAsserted,
			ClaimsValidated:      audit.Source.ClaimsValidated,
			ClaimsRefuted:        audit.Source.ClaimsRefuted,
			UpdatedAt:            formatOptionalTime(audit.Source.UpdatedAt),
		}
		timeline, err := h.db.Namespace(ns, "").SourceTrustTimeline(ctx, audit.Source.ExternalID, time.Time{})
		if err != nil {
			return summary, err
		}
		for _, point := range timeline {
			summary.SourceTrustTimeline = append(summary.SourceTrustTimeline, adminSourceTrustPoint{
				Time:              point.TxTime.Format(time.RFC3339),
				SourceID:          point.SourceID,
				NodeID:            point.NodeID.String(),
				Action:            point.Action,
				SourceCredibility: point.SourceCredibility,
				Reason:            point.Reason,
			})
		}
		summary.Counts.SourceTrustPoints = len(summary.SourceTrustTimeline)
	}
	for _, point := range audit.ConfidenceHistory {
		summary.ConfidenceTimeline = append(summary.ConfidenceTimeline, adminConfidenceTimelinePoint{
			Time:       point.Time.Format(time.RFC3339),
			Confidence: point.Confidence,
			Version:    point.Version,
		})
	}
	for _, contradictor := range audit.Contradictors {
		summary.ContradictionPaths = append(summary.ContradictionPaths, buildAdminContradictionPath(audit.Node, contradictor))
	}
	graphContext, err := h.buildAdminGraphContext(ctx, ns, audit.Node.ID)
	if err != nil {
		return summary, err
	}
	summary.GraphContext = graphContext
	summary.Counts.GraphNeighbors = len(graphContext)
	return summary, nil
}

func (h *adminHandler) buildAdminGraphContext(ctx context.Context, ns string, nodeID uuid.UUID) ([]adminGraphContextNode, error) {
	outgoing, err := h.graph.EdgesFrom(ctx, ns, nodeID, nil)
	if err != nil {
		return nil, err
	}
	incoming, err := h.graph.EdgesTo(ctx, ns, nodeID, nil)
	if err != nil {
		return nil, err
	}
	context := make([]adminGraphContextNode, 0, len(outgoing)+len(incoming))
	for _, edge := range outgoing {
		node, err := h.graph.GetNode(ctx, ns, edge.Dst)
		if err != nil {
			return nil, err
		}
		if node == nil {
			continue
		}
		context = append(context, buildAdminGraphContextNode(*node, edge, "outgoing"))
	}
	for _, edge := range incoming {
		node, err := h.graph.GetNode(ctx, ns, edge.Src)
		if err != nil {
			return nil, err
		}
		if node == nil {
			continue
		}
		context = append(context, buildAdminGraphContextNode(*node, edge, "incoming"))
	}
	sort.SliceStable(context, func(i, j int) bool {
		if context[i].Relation == context[j].Relation {
			return context[i].NodeID < context[j].NodeID
		}
		return context[i].Relation < context[j].Relation
	})
	return context, nil
}

func buildAdminGraphContextNode(node core.Node, edge core.Edge, direction string) adminGraphContextNode {
	return adminGraphContextNode{
		NodeID:     node.ID.String(),
		Text:       core.NodeText(node),
		SourceID:   nodeSourceID(node),
		Labels:     node.Labels,
		Relation:   adminEdgeRelation(edge.Type, direction),
		Direction:  direction,
		EdgeType:   edge.Type,
		EdgeWeight: edge.Weight,
		Confidence: node.Confidence,
		ValidFrom:  formatOptionalTime(node.ValidFrom),
	}
}

func buildAdminContradictionPath(claim core.Node, evidence observe.EvidenceNode) adminContradictionPath {
	direction := "incoming"
	if evidence.Edge.Src == claim.ID {
		direction = "outgoing"
	}
	gap := claim.Confidence - evidence.Node.Confidence
	if gap < 0 {
		gap = -gap
	}
	return adminContradictionPath{
		Direction:     direction,
		Relation:      adminEdgeRelation(evidence.Edge.Type, direction),
		ClaimID:       claim.ID.String(),
		ClaimText:     core.NodeText(claim),
		OtherNodeID:   evidence.Node.ID.String(),
		OtherText:     core.NodeText(evidence.Node),
		OtherSourceID: nodeSourceID(evidence.Node),
		EdgeID:        evidence.Edge.ID.String(),
		EdgeWeight:    evidence.Edge.Weight,
		Severity:      adminContradictionSeverity(gap, evidence.Edge.Weight),
		ConfidenceGap: gap,
	}
}

func adminEdgeRelation(edgeType, direction string) string {
	switch edgeType {
	case core.EdgeSupports:
		if direction == "incoming" {
			return "supported by"
		}
		return "supports"
	case core.EdgeContradicts:
		if direction == "incoming" {
			return "contradicted by"
		}
		return "contradicts"
	case core.EdgeDerivedFrom:
		if direction == "incoming" {
			return "derived into"
		}
		return "derived from"
	default:
		if direction == "incoming" {
			return "linked from"
		}
		return "links to"
	}
}

func adminContradictionSeverity(confidenceGap, edgeWeight float64) string {
	strength := edgeWeight
	if strength == 0 {
		strength = 1
	}
	switch {
	case confidenceGap >= 0.5 || strength >= 0.9:
		return "high"
	case confidenceGap >= 0.2 || strength >= 0.5:
		return "medium"
	default:
		return "low"
	}
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

type searchResult struct {
	ID          uuid.UUID `json:"id"`
	Labels      []string  `json:"labels,omitempty"`
	Text        string    `json:"text,omitempty"`
	SourceID    string    `json:"source_id,omitempty"`
	Confidence  float64   `json:"confidence,omitempty"`
	Version     uint64    `json:"version,omitempty"`
	ValidFrom   string    `json:"valid_from,omitempty"`
	MatchReason string    `json:"match_reason"`
}

// handleSearch returns recent valid nodes matching text, labels, or source.
// Query params:
//   - ns: namespace (required)
//   - q: case-insensitive content/source query (optional)
//   - labels: comma-separated label filter (optional)
//   - limit: max results, default 10, max 50
func (h *adminHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	ns := strings.TrimSpace(r.URL.Query().Get("ns"))
	if ns == "" {
		http.Error(w, "missing ns parameter", http.StatusBadRequest)
		return
	}
	limit := 10
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 {
			http.Error(w, "invalid limit parameter", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	if limit > 50 {
		limit = 50
	}
	var labels []string
	if rawLabels := strings.TrimSpace(r.URL.Query().Get("labels")); rawLabels != "" {
		for _, label := range strings.Split(rawLabels, ",") {
			if label = strings.TrimSpace(label); label != "" {
				labels = append(labels, label)
			}
		}
	}
	nodes, err := h.graph.ValidAt(r.Context(), ns, time.Now(), labels)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].TxTime.Equal(nodes[j].TxTime) {
			return nodes[i].ID.String() < nodes[j].ID.String()
		}
		return nodes[i].TxTime.After(nodes[j].TxTime)
	})
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	results := make([]searchResult, 0, min(limit, len(nodes)))
	for _, node := range nodes {
		result, ok := buildSearchResult(node, query)
		if !ok {
			continue
		}
		results = append(results, result)
		if len(results) >= limit {
			break
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"namespace": ns,
		"query":     strings.TrimSpace(r.URL.Query().Get("q")),
		"count":     len(results),
		"results":   results,
	})
}

func buildSearchResult(node core.Node, query string) (searchResult, bool) {
	text := core.NodeText(node)
	sourceID := nodeSourceID(node)
	haystack := strings.ToLower(strings.Join([]string{
		text,
		sourceID,
		strings.Join(node.Labels, " "),
		node.ID.String(),
	}, " "))
	if query != "" && !strings.Contains(haystack, query) {
		return searchResult{}, false
	}
	reason := "recent"
	if query != "" {
		switch {
		case strings.Contains(strings.ToLower(text), query):
			reason = "text"
		case strings.Contains(strings.ToLower(sourceID), query):
			reason = "source"
		case strings.Contains(strings.ToLower(strings.Join(node.Labels, " ")), query):
			reason = "label"
		case strings.Contains(strings.ToLower(node.ID.String()), query):
			reason = "id"
		}
	}
	return searchResult{
		ID:          node.ID,
		Labels:      node.Labels,
		Text:        text,
		SourceID:    sourceID,
		Confidence:  node.Confidence,
		Version:     node.Version,
		ValidFrom:   node.ValidFrom.Format(time.RFC3339),
		MatchReason: reason,
	}, true
}

func nodeSourceID(node core.Node) string {
	if sourceID, ok := node.Properties["source_id"].(string); ok {
		return sourceID
	}
	return ""
}

// handleTimeTravel returns all nodes valid at a given point in time.
// Query params:
//   - ns: namespace (required)
//   - asof: ISO 8601 timestamp (required)
//   - labels: comma-separated label filter (optional)
func (h *adminHandler) handleTimeTravel(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("ns")
	if ns == "" {
		http.Error(w, "missing ns parameter", http.StatusBadRequest)
		return
	}

	asofStr := r.URL.Query().Get("asof")
	if asofStr == "" {
		http.Error(w, "missing asof parameter", http.StatusBadRequest)
		return
	}

	asof, err := parseTime(asofStr)
	if err != nil {
		http.Error(w, "invalid asof format (use RFC3339 or YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	var labels []string
	if l := r.URL.Query().Get("labels"); l != "" {
		labels = strings.Split(l, ",")
	}

	nodes, err := h.graph.ValidAt(r.Context(), ns, asof, labels)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"namespace": ns,
		"as_of":     asof.Format(time.RFC3339),
		"count":     len(nodes),
		"nodes":     nodes,
	})
}

// handleDiff returns what changed between two points in time.
// Query params:
//   - ns: namespace (required)
//   - from: RFC3339 or YYYY-MM-DD start time (required)
//   - to: RFC3339 or YYYY-MM-DD end time (required)
func (h *adminHandler) handleDiff(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("ns")
	if ns == "" {
		http.Error(w, "missing ns parameter", http.StatusBadRequest)
		return
	}

	fromStr := r.URL.Query().Get("from")
	if fromStr == "" {
		http.Error(w, "missing from parameter", http.StatusBadRequest)
		return
	}

	toStr := r.URL.Query().Get("to")
	if toStr == "" {
		http.Error(w, "missing to parameter", http.StatusBadRequest)
		return
	}

	from, err := parseTime(fromStr)
	if err != nil {
		http.Error(w, "invalid from format (use RFC3339 or YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	to, err := parseTime(toStr)
	if err != nil {
		http.Error(w, "invalid to format (use RFC3339 or YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	diffs, err := h.graph.Diff(r.Context(), ns, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"namespace": ns,
		"from":      from.Format(time.RFC3339),
		"to":        to.Format(time.RFC3339),
		"count":     len(diffs),
		"changes":   diffs,
	})
}
