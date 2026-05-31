// Package server provides gRPC and REST API servers for contextdb.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/buildinfo"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/ingest"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/store/postgres"
	"github.com/antiartificial/contextdb/pkg/client"
)

// RESTServer provides HTTP REST endpoints wrapping the client API.
type RESTServer struct {
	db *client.DB
}

// NewRESTServer returns a REST server backed by the given DB.
func NewRESTServer(db *client.DB) *RESTServer {
	return &RESTServer{db: db}
}

// Handler returns an http.Handler with all REST routes.
func (s *RESTServer) Handler() http.Handler {
	mux := http.NewServeMux()

	// POST /v1/namespaces/{ns}/write
	mux.HandleFunc("POST /v1/namespaces/{ns}/write", s.handleWrite)

	// POST /v1/namespaces/{ns}/retrieve
	mux.HandleFunc("POST /v1/namespaces/{ns}/retrieve", s.handleRetrieve)
	mux.HandleFunc("POST /v1/namespaces/{ns}/rank/explain", s.handleExplainRank)

	// POST /v1/namespaces/{ns}/ingest
	mux.HandleFunc("POST /v1/namespaces/{ns}/ingest", s.handleIngest)

	// GET /v1/namespaces/{ns}/nodes/{id}
	mux.HandleFunc("GET /v1/namespaces/{ns}/nodes/{id}", s.handleGetNode)

	// POST /v1/namespaces/{ns}/sources/label
	mux.HandleFunc("POST /v1/namespaces/{ns}/sources/label", s.handleLabelSource)
	mux.HandleFunc("GET /v1/namespaces/{ns}/sources/{sourceID}/trust", s.handleSourceTrustTimeline)
	mux.HandleFunc("GET /v1/namespaces/{ns}/review/queue", s.handleReviewQueue)
	mux.HandleFunc("GET /v1/namespaces/{ns}/review/escalations", s.handleReviewEscalations)
	mux.HandleFunc("GET /v1/namespaces/{ns}/review/decisions", s.handleReviewDecisions)
	mux.HandleFunc("POST /v1/namespaces/{ns}/review/decisions", s.handleRecordReviewDecision)

	// POST /v1/namespaces/{ns}/consensus/{claimID}
	mux.HandleFunc("POST /v1/namespaces/{ns}/consensus/{claimID}", s.handleConsensus)

	mux.HandleFunc("POST /v1/namespaces/{ns}/nodes/{id}/validate", s.handleValidateClaim)
	mux.HandleFunc("POST /v1/namespaces/{ns}/nodes/{id}/refute", s.handleRefuteClaim)
	mux.HandleFunc("POST /v1/namespaces/{ns}/nodes/{id}/useful", s.handleMarkUseful)
	mux.HandleFunc("POST /v1/namespaces/{ns}/nodes/{id}/stale", s.handleMarkStale)
	mux.HandleFunc("GET /v1/namespaces/{ns}/feedback/events", s.handleFeedbackEvents)

	mux.HandleFunc("GET /v1/namespaces/{ns}/nodes/{id}/narrative", s.handleNarrative)
	mux.HandleFunc("POST /v1/namespaces/{ns}/gaps", s.handleKnowledgeGaps)
	mux.HandleFunc("POST /v1/namespaces/{ns}/acquisition/plan", s.handleAcquisitionPlan)

	// GET /v1/stats
	mux.HandleFunc("GET /v1/stats", s.handleStats)

	// GET /v1/ping
	mux.HandleFunc("GET /v1/ping", s.handlePing)

	mux.HandleFunc("GET /v1/version", s.handleVersion)
	mux.HandleFunc("GET /v1/features", s.handleFeatures)
	mux.HandleFunc("GET /v1/migrations", s.handleMigrations)

	if gql, err := NewGraphQLServer(s.db); err == nil {
		mux.Handle("GET /graphql", gql)
		mux.Handle("POST /graphql", gql)
	}

	return mux
}

// ─── Request/Response types ──────────────────────────────────────────────────

type writeRequest struct {
	Mode       string            `json:"mode"`
	Content    string            `json:"content"`
	SourceID   string            `json:"source_id"`
	Labels     []string          `json:"labels"`
	Properties map[string]string `json:"properties"`
	Vector     []float32         `json:"vector"`
	ModelID    string            `json:"model_id"`
	Confidence float64           `json:"confidence"`
	ValidFrom  *time.Time        `json:"valid_from,omitempty"`
	MemType    string            `json:"mem_type,omitempty"`
	Dedup      bool              `json:"dedup,omitempty"`
	SkipDedup  bool              `json:"skip_dedup,omitempty"`
}

type writeResponse struct {
	NodeID      string   `json:"node_id"`
	Admitted    bool     `json:"admitted"`
	Reason      string   `json:"reason,omitempty"`
	ConflictIDs []string `json:"conflict_ids,omitempty"`
}

type retrieveRequest struct {
	Vector      []float32    `json:"vector"`
	Vectors     [][]float32  `json:"vectors"`
	Text        string       `json:"text"`
	SeedIDs     []string     `json:"seed_ids"`
	TopK        int          `json:"top_k"`
	Labels      []string     `json:"labels"`
	ScoreParams *scoreParams `json:"score_params,omitempty"`
	AsOf        *time.Time   `json:"as_of,omitempty"`
}

type scoreParams struct {
	SimilarityWeight float64 `json:"similarity_weight"`
	ConfidenceWeight float64 `json:"confidence_weight"`
	RecencyWeight    float64 `json:"recency_weight"`
	UtilityWeight    float64 `json:"utility_weight"`
	DecayAlpha       float64 `json:"decay_alpha"`
}

type explainRankRequest struct {
	Mode        string       `json:"mode"`
	NodeID      string       `json:"node_id"`
	OtherNodeID string       `json:"other_node_id"`
	Text        string       `json:"text"`
	Vector      []float32    `json:"vector"`
	ScoreParams *scoreParams `json:"score_params,omitempty"`
	AsOf        *time.Time   `json:"as_of,omitempty"`
	MaxDepth    int          `json:"max_depth,omitempty"`
}

type retrieveResponse struct {
	Results []scoredNodeResponse `json:"results"`
}

type scoredNodeResponse struct {
	ID              string         `json:"id"`
	Namespace       string         `json:"namespace"`
	Labels          []string       `json:"labels"`
	Properties      map[string]any `json:"properties"`
	Score           float64        `json:"score"`
	SimilarityScore float64        `json:"similarity_score"`
	ConfidenceScore float64        `json:"confidence_score"`
	RecencyScore    float64        `json:"recency_score"`
	UtilityScore    float64        `json:"utility_score"`
	ScoreBreakdown  scoreBreakdown `json:"score_breakdown"`
	RetrievalSource string         `json:"retrieval_source"`
}

type scoreBreakdown struct {
	Similarity float64 `json:"similarity"`
	Confidence float64 `json:"confidence"`
	Recency    float64 `json:"recency"`
	Utility    float64 `json:"utility"`
}

type ingestRequest struct {
	Mode     string `json:"mode"`
	Text     string `json:"text"`
	SourceID string `json:"source_id"`
}

type ingestResponse struct {
	NodesWritten int `json:"nodes_written"`
	EdgesWritten int `json:"edges_written"`
	Rejected     int `json:"rejected"`
}

type consensusResponse struct {
	ClaimID     string  `json:"claim_id"`
	Probability float64 `json:"probability"`
	Confidence  float64 `json:"confidence"`
	SourceCount int     `json:"source_count"`
	Method      string  `json:"method"`
}

type labelSourceRequest struct {
	Mode       string   `json:"mode"`
	ExternalID string   `json:"external_id"`
	Labels     []string `json:"labels"`
}

type feedbackRequest struct {
	Mode    string `json:"mode"`
	Reason  string `json:"reason,omitempty"`
	Quality int    `json:"quality,omitempty"`
}

type reviewDecisionRequest struct {
	Mode      string     `json:"mode"`
	ReviewID  string     `json:"review_id"`
	Status    string     `json:"status"`
	Owner     string     `json:"owner,omitempty"`
	Decision  string     `json:"decision,omitempty"`
	Note      string     `json:"note,omitempty"`
	RecheckAt *time.Time `json:"recheck_at,omitempty"`
}

type gapRequest struct {
	Mode       string  `json:"mode"`
	TopK       int     `json:"top_k,omitempty"`
	MinGapSize float64 `json:"min_gap_size,omitempty"`
	MaxGaps    int     `json:"max_gaps,omitempty"`
}

type acquisitionPlanRequest struct {
	Mode       string  `json:"mode"`
	TopK       int     `json:"top_k,omitempty"`
	MinGapSize float64 `json:"min_gap_size,omitempty"`
	MaxGaps    int     `json:"max_gaps,omitempty"`
	Budget     int     `json:"budget,omitempty"`
}

type reviewQueueResponse struct {
	Items []client.ReviewItem `json:"items"`
}

type reviewEscalationDigestResponse struct {
	Digest client.ReviewEscalationDigest `json:"digest"`
}

type reviewDecisionsResponse struct {
	Decisions []client.ReviewDecision `json:"decisions"`
}

type feedbackResponse struct {
	NodeID            string  `json:"node_id"`
	Action            string  `json:"action"`
	Confidence        float64 `json:"confidence"`
	Utility           float64 `json:"utility"`
	SourceID          string  `json:"source_id,omitempty"`
	SourceCredibility float64 `json:"source_credibility,omitempty"`
	Reason            string  `json:"reason,omitempty"`
}

type narrativeReportResponse struct {
	NodeID                string                      `json:"node_id"`
	Namespace             string                      `json:"namespace"`
	GeneratedAt           time.Time                   `json:"generated_at"`
	Summary               string                      `json:"summary"`
	Claim                 citedClaimResponse          `json:"claim"`
	Evidence              []citedClaimResponse        `json:"evidence"`
	Contradictions        []citedClaimResponse        `json:"contradictions"`
	Provenance            []citedClaimResponse        `json:"provenance"`
	ConfidenceExplanation string                      `json:"confidence_explanation"`
	Grounding             []retrieval.GroundingResult `json:"grounding,omitempty"`
}

type citedClaimResponse struct {
	NodeID          string     `json:"node_id"`
	SourceID        string     `json:"source_id,omitempty"`
	Text            string     `json:"text"`
	Confidence      float64    `json:"confidence"`
	EpistemicType   string     `json:"epistemic_type,omitempty"`
	ValidFrom       time.Time  `json:"valid_from"`
	ValidUntil      *time.Time `json:"valid_until,omitempty"`
	ProvenanceDepth int        `json:"provenance_depth,omitempty"`
	Relation        string     `json:"relation,omitempty"`
}

type gapReportResponse struct {
	Namespace     string                 `json:"namespace"`
	Gaps          []knowledgeGapResponse `json:"gaps"`
	CoverageScore float64                `json:"coverage_score"`
	TotalNodes    int                    `json:"total_nodes"`
	GapsDetected  int                    `json:"gaps_detected"`
}

type knowledgeGapResponse struct {
	ID                 string    `json:"id"`
	NearestTopics      []string  `json:"nearest_topics"`
	CentroidVector     []float32 `json:"centroid_vector"`
	DensityScore       float64   `json:"density_score"`
	ConfidenceGap      float64   `json:"confidence_gap"`
	TemporalGapSeconds float64   `json:"temporal_gap_seconds"`
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func (s *RESTServer) handleWrite(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	var req writeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	mode := resolveMode(req.Mode)
	h := s.db.Namespace(ns, mode)

	props := make(map[string]any)
	for k, v := range req.Properties {
		props[k] = v
	}

	var validFrom time.Time
	if req.ValidFrom != nil {
		validFrom = *req.ValidFrom
	}

	result, err := h.Write(r.Context(), client.WriteRequest{
		Content:    req.Content,
		SourceID:   req.SourceID,
		Labels:     req.Labels,
		Properties: props,
		Vector:     req.Vector,
		ModelID:    req.ModelID,
		Confidence: req.Confidence,
		ValidFrom:  validFrom,
		MemType:    core.MemoryType(req.MemType),
		Dedup:      req.Dedup,
		SkipDedup:  req.SkipDedup,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	conflictIDs := make([]string, len(result.ConflictIDs))
	for i, id := range result.ConflictIDs {
		conflictIDs[i] = id.String()
	}

	writeJSON(w, http.StatusOK, writeResponse{
		NodeID:      result.NodeID.String(),
		Admitted:    result.Admitted,
		Reason:      result.Reason,
		ConflictIDs: conflictIDs,
	})
}

func (s *RESTServer) handleRetrieve(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	var req retrieveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	h := s.db.Namespace(ns, namespace.ModeGeneral)

	var seedIDs []uuid.UUID
	for _, s := range req.SeedIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			continue
		}
		seedIDs = append(seedIDs, id)
	}

	var sp core.ScoreParams
	if req.ScoreParams != nil {
		sp = core.ScoreParams{
			SimilarityWeight: req.ScoreParams.SimilarityWeight,
			ConfidenceWeight: req.ScoreParams.ConfidenceWeight,
			RecencyWeight:    req.ScoreParams.RecencyWeight,
			UtilityWeight:    req.ScoreParams.UtilityWeight,
			DecayAlpha:       req.ScoreParams.DecayAlpha,
		}
	}

	var asOf time.Time
	if req.AsOf != nil {
		asOf = *req.AsOf
	}

	results, err := h.Retrieve(r.Context(), client.RetrieveRequest{
		Vector:      req.Vector,
		Vectors:     req.Vectors,
		Text:        req.Text,
		SeedIDs:     seedIDs,
		TopK:        req.TopK,
		Labels:      req.Labels,
		ScoreParams: sp,
		AsOf:        asOf,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	resp := retrieveResponse{Results: make([]scoredNodeResponse, len(results))}
	for i, r := range results {
		resp.Results[i] = newScoredNodeResponse(r)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *RESTServer) handleExplainRank(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	var req explainRankRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	nodeID, err := uuid.Parse(req.NodeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid node_id: %w", err))
		return
	}
	otherNodeID, err := uuid.Parse(req.OtherNodeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid other_node_id: %w", err))
		return
	}

	var sp core.ScoreParams
	if req.ScoreParams != nil {
		sp = core.ScoreParams{
			SimilarityWeight: req.ScoreParams.SimilarityWeight,
			ConfidenceWeight: req.ScoreParams.ConfidenceWeight,
			RecencyWeight:    req.ScoreParams.RecencyWeight,
			UtilityWeight:    req.ScoreParams.UtilityWeight,
			DecayAlpha:       req.ScoreParams.DecayAlpha,
		}
	}

	var asOf time.Time
	if req.AsOf != nil {
		asOf = *req.AsOf
	}

	h := s.db.Namespace(ns, resolveMode(req.Mode))
	explanation, err := h.ExplainRank(r.Context(), client.ExplainRankRequest{
		NodeID:      nodeID,
		OtherNodeID: otherNodeID,
		Text:        req.Text,
		Vector:      req.Vector,
		ScoreParams: sp,
		AsOf:        asOf,
		MaxDepth:    req.MaxDepth,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, explanation)
}

func (s *RESTServer) handleIngest(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	var req ingestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	mode := resolveMode(req.Mode)
	h := s.db.Namespace(ns, mode)

	result, err := h.IngestText(r.Context(), req.Text, req.SourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, ingestResponse{
		NodesWritten: result.NodesWritten,
		EdgesWritten: result.EdgesWritten,
		Rejected:     result.Rejected,
	})
}

func (s *RESTServer) handleGetNode(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid node id: %w", err))
		return
	}

	// Direct graph store access via the DB's namespace handle
	h := s.db.Namespace(ns, namespace.ModeGeneral)
	_ = h // namespace handle ensures the ns exists

	// Use retrieve with empty vector and the node ID as seed
	results, err := h.Retrieve(r.Context(), client.RetrieveRequest{
		SeedIDs: []uuid.UUID{id},
		TopK:    1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(results) == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("node not found"))
		return
	}

	writeJSON(w, http.StatusOK, newScoredNodeResponse(results[0]))
}

func (s *RESTServer) handleLabelSource(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	var req labelSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	mode := resolveMode(req.Mode)
	h := s.db.Namespace(ns, mode)

	if err := h.LabelSource(r.Context(), req.ExternalID, req.Labels); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *RESTServer) handleConsensus(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	claimIDStr := r.PathValue("claimID")
	claimID, err := uuid.Parse(claimIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid claim id: %w", err))
		return
	}

	graph, _, _, _ := s.db.Stores()
	resolver := ingest.NewConsensusResolver(graph, nil)

	estimate, err := resolver.ResolveTruth(r.Context(), claimID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, consensusResponse{
		ClaimID:     estimate.ClaimID.String(),
		Probability: estimate.Probability,
		Confidence:  estimate.Confidence,
		SourceCount: estimate.SourceCount,
		Method:      estimate.Method,
	})
}

func (s *RESTServer) handleValidateClaim(w http.ResponseWriter, r *http.Request) {
	s.handleFeedback(w, r, "validate")
}

func (s *RESTServer) handleRefuteClaim(w http.ResponseWriter, r *http.Request) {
	s.handleFeedback(w, r, "refute")
}

func (s *RESTServer) handleMarkUseful(w http.ResponseWriter, r *http.Request) {
	s.handleFeedback(w, r, "useful")
}

func (s *RESTServer) handleMarkStale(w http.ResponseWriter, r *http.Request) {
	s.handleFeedback(w, r, "stale")
}

func (s *RESTServer) handleFeedback(w http.ResponseWriter, r *http.Request, action string) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}
	nodeID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid node id: %w", err))
		return
	}

	var req feedbackRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	h := s.db.Namespace(ns, resolveMode(req.Mode))
	var result client.FeedbackResult
	switch action {
	case "validate":
		result, err = h.ValidateClaim(r.Context(), nodeID)
	case "refute":
		result, err = h.RefuteClaim(r.Context(), nodeID, req.Reason)
	case "useful":
		result, err = h.MarkUseful(r.Context(), nodeID, req.Quality)
	case "stale":
		result, err = h.MarkStale(r.Context(), nodeID, req.Reason)
	default:
		err = fmt.Errorf("unknown feedback action %q", action)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, newFeedbackResponse(result))
}

func (s *RESTServer) handleFeedbackEvents(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	var after time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid after timestamp: %w", err))
			return
		}
		after = t
	}

	h := s.db.Namespace(ns, resolveMode(r.URL.Query().Get("mode")))
	events, err := h.FeedbackEvents(r.Context(), after)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *RESTServer) handleSourceTrustTimeline(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}
	sourceID := r.PathValue("sourceID")
	if sourceID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing source id"))
		return
	}

	var after time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid after timestamp: %w", err))
			return
		}
		after = t
	}

	h := s.db.Namespace(ns, resolveMode(r.URL.Query().Get("mode")))
	points, err := h.SourceTrustTimeline(r.Context(), sourceID, after)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source_id": sourceID,
		"points":    points,
	})
}

func (s *RESTServer) handleReviewQueue(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	req, err := parseReviewQueueRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	h := s.db.Namespace(ns, resolveMode(r.URL.Query().Get("mode")))
	items, err := h.ReviewQueue(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, reviewQueueResponse{Items: items})
}

func (s *RESTServer) handleReviewEscalations(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}
	req, err := parseReviewQueueRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	h := s.db.Namespace(ns, resolveMode(r.URL.Query().Get("mode")))
	digest, err := h.ReviewEscalationDigest(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, reviewEscalationDigestResponse{Digest: digest})
}

func parseReviewQueueRequest(r *http.Request) (client.ReviewQueueRequest, error) {
	var req client.ReviewQueueRequest
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return req, fmt.Errorf("invalid after timestamp: %w", err)
		}
		req.After = t
	}
	var err error
	if req.LowConfidenceThreshold, err = parseOptionalFloatQuery(r, "low_confidence_threshold"); err != nil {
		return req, err
	}
	if req.SourceTrustThreshold, err = parseOptionalFloatQuery(r, "source_trust_threshold"); err != nil {
		return req, err
	}
	if req.SourceTrustDropThreshold, err = parseOptionalFloatQuery(r, "source_trust_drop_threshold"); err != nil {
		return req, err
	}
	if req.SourceAnomalyEscalationPriority, err = parseOptionalFloatQuery(r, "source_anomaly_escalation_priority"); err != nil {
		return req, err
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("source_refutation_threshold")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return req, fmt.Errorf("invalid source_refutation_threshold: %w", err)
		}
		req.SourceRefutationThreshold = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("escalation_after_hours")); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return req, fmt.Errorf("invalid escalation_after_hours: %w", err)
		}
		req.EscalationAfter = time.Duration(parsed * float64(time.Hour))
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return req, fmt.Errorf("invalid limit: %w", err)
		}
		req.Limit = parsed
	}
	req.Types = parseCommaList(r.URL.Query().Get("type"))
	req.SourceID = strings.TrimSpace(r.URL.Query().Get("source_id"))
	req.Status = strings.TrimSpace(r.URL.Query().Get("status"))
	req.Owner = strings.TrimSpace(r.URL.Query().Get("owner"))
	return req, nil
}

func parseOptionalFloatQuery(r *http.Request, name string) (float64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return parsed, nil
}

func parseCommaList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (s *RESTServer) handleReviewDecisions(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	var after time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid after timestamp: %w", err))
			return
		}
		after = t
	}

	h := s.db.Namespace(ns, resolveMode(r.URL.Query().Get("mode")))
	decisions, err := h.ReviewDecisions(r.Context(), after)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, reviewDecisionsResponse{Decisions: decisions})
}

func (s *RESTServer) handleRecordReviewDecision(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	var req reviewDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var recheckAt time.Time
	if req.RecheckAt != nil {
		recheckAt = *req.RecheckAt
	}

	h := s.db.Namespace(ns, resolveMode(req.Mode))
	decision, err := h.RecordReviewDecision(r.Context(), client.ReviewDecisionRequest{
		ReviewID:  req.ReviewID,
		Status:    req.Status,
		Owner:     req.Owner,
		Decision:  req.Decision,
		Note:      req.Note,
		RecheckAt: recheckAt,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func (s *RESTServer) handleNarrative(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}
	nodeID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid node id: %w", err))
		return
	}

	h := s.db.Namespace(ns, resolveMode(r.URL.Query().Get("mode")))
	report, err := h.Explain(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if report == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("node not found"))
		return
	}
	writeJSON(w, http.StatusOK, newNarrativeReportResponse(report))
}

func (s *RESTServer) handleKnowledgeGaps(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	var req gapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	h := s.db.Namespace(ns, resolveMode(req.Mode))
	report, err := h.KnowledgeGaps(r.Context(), client.GapRequest{
		TopK:       req.TopK,
		MinGapSize: req.MinGapSize,
		MaxGaps:    req.MaxGaps,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, newGapReportResponse(report))
}

func (s *RESTServer) handleAcquisitionPlan(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	tenant := TenantFromContext(r.Context())
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	var req acquisitionPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	h := s.db.Namespace(ns, resolveMode(req.Mode))
	plan, err := h.AcquisitionPlan(r.Context(), client.AcquisitionPlanRequest{
		TopK:       req.TopK,
		MinGapSize: req.MinGapSize,
		MaxGaps:    req.MaxGaps,
		Budget:     req.Budget,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *RESTServer) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.db.Stats()
	writeJSON(w, http.StatusOK, stats)
}

func (s *RESTServer) handlePing(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *RESTServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	info := buildinfo.Current(postgres.AvailableMigrations())
	writeJSON(w, http.StatusOK, info)
}

func (s *RESTServer) handleFeatures(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":  buildinfo.Version,
		"features": buildinfo.Features(),
	})
}

func (s *RESTServer) handleMigrations(w http.ResponseWriter, r *http.Request) {
	migrations := postgres.AvailableMigrations()
	info := buildinfo.Current(migrations)
	writeJSON(w, http.StatusOK, map[string]any{
		"version":          buildinfo.Version,
		"latest_migration": info.LatestMigration,
		"migrations":       migrations,
	})
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func resolveMode(mode string) namespace.Mode {
	switch strings.ToLower(mode) {
	case "belief_system":
		return namespace.ModeBeliefSystem
	case "agent_memory":
		return namespace.ModeAgentMemory
	case "procedural":
		return namespace.ModeProcedural
	default:
		return namespace.ModeGeneral
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func newScoredNodeResponse(r client.Result) scoredNodeResponse {
	return scoredNodeResponse{
		ID:              r.Node.ID.String(),
		Namespace:       r.Node.Namespace,
		Labels:          r.Node.Labels,
		Properties:      r.Node.Properties,
		Score:           r.Score,
		SimilarityScore: r.SimilarityScore,
		ConfidenceScore: r.ConfidenceScore,
		RecencyScore:    r.RecencyScore,
		UtilityScore:    r.UtilityScore,
		ScoreBreakdown: scoreBreakdown{
			Similarity: r.Breakdown.Similarity,
			Confidence: r.Breakdown.Confidence,
			Recency:    r.Breakdown.Recency,
			Utility:    r.Breakdown.Utility,
		},
		RetrievalSource: r.RetrievalSource,
	}
}

func newFeedbackResponse(r client.FeedbackResult) feedbackResponse {
	return feedbackResponse{
		NodeID:            r.NodeID.String(),
		Action:            r.Action,
		Confidence:        r.Confidence,
		Utility:           r.Utility,
		SourceID:          r.SourceID,
		SourceCredibility: r.SourceCredibility,
		Reason:            r.Reason,
	}
}

func newNarrativeReportResponse(r *retrieval.NarrativeReport) narrativeReportResponse {
	return narrativeReportResponse{
		NodeID:                r.NodeID.String(),
		Namespace:             r.Namespace,
		GeneratedAt:           r.GeneratedAt,
		Summary:               r.Summary,
		Claim:                 newCitedClaimResponse(r.Claim),
		Evidence:              newCitedClaimResponses(r.Evidence),
		Contradictions:        newCitedClaimResponses(r.Contradictions),
		Provenance:            newCitedClaimResponses(r.Provenance),
		ConfidenceExplanation: r.ConfidenceExplanation,
		Grounding:             r.Grounding,
	}
}

func newCitedClaimResponses(claims []retrieval.CitedClaim) []citedClaimResponse {
	out := make([]citedClaimResponse, len(claims))
	for i, claim := range claims {
		out[i] = newCitedClaimResponse(claim)
	}
	return out
}

func newCitedClaimResponse(c retrieval.CitedClaim) citedClaimResponse {
	return citedClaimResponse{
		NodeID:          c.NodeID.String(),
		SourceID:        c.SourceID,
		Text:            c.Text,
		Confidence:      c.Confidence,
		EpistemicType:   c.EpistemicType,
		ValidFrom:       c.ValidFrom,
		ValidUntil:      c.ValidUntil,
		ProvenanceDepth: c.ProvenanceDepth,
		Relation:        c.Relation,
	}
}

func newGapReportResponse(r *retrieval.GapReport) gapReportResponse {
	out := gapReportResponse{
		Namespace:     r.Namespace,
		CoverageScore: r.CoverageScore,
		TotalNodes:    r.TotalNodes,
		GapsDetected:  r.GapsDetected,
		Gaps:          make([]knowledgeGapResponse, len(r.Gaps)),
	}
	for i, gap := range r.Gaps {
		out.Gaps[i] = knowledgeGapResponse{
			ID:                 gap.ID.String(),
			NearestTopics:      gap.NearestTopics,
			CentroidVector:     gap.CentroidVector,
			DensityScore:       gap.DensityScore,
			ConfidenceGap:      gap.ConfidenceGap,
			TemporalGapSeconds: gap.TemporalGap.Seconds(),
		}
	}
	return out
}
