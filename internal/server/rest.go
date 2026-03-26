// Package server provides gRPC and REST API servers for contextdb.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
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

	// POST /v1/namespaces/{ns}/ingest
	mux.HandleFunc("POST /v1/namespaces/{ns}/ingest", s.handleIngest)

	// GET /v1/namespaces/{ns}/nodes/{id}
	mux.HandleFunc("GET /v1/namespaces/{ns}/nodes/{id}", s.handleGetNode)

	// POST /v1/namespaces/{ns}/sources/label
	mux.HandleFunc("POST /v1/namespaces/{ns}/sources/label", s.handleLabelSource)

	// GET /v1/stats
	mux.HandleFunc("GET /v1/stats", s.handleStats)

	// GET /v1/ping
	mux.HandleFunc("GET /v1/ping", s.handlePing)

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

type retrieveResponse struct {
	Results []scoredNodeResponse `json:"results"`
}

type scoredNodeResponse struct {
	ID              string            `json:"id"`
	Namespace       string            `json:"namespace"`
	Labels          []string          `json:"labels"`
	Properties      map[string]any    `json:"properties"`
	Score           float64           `json:"score"`
	SimilarityScore float64           `json:"similarity_score"`
	ConfidenceScore float64           `json:"confidence_score"`
	RecencyScore    float64           `json:"recency_score"`
	UtilityScore    float64           `json:"utility_score"`
	RetrievalSource string            `json:"retrieval_source"`
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

type labelSourceRequest struct {
	Mode       string   `json:"mode"`
	ExternalID string   `json:"external_id"`
	Labels     []string `json:"labels"`
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
		resp.Results[i] = scoredNodeResponse{
			ID:              r.Node.ID.String(),
			Namespace:       r.Node.Namespace,
			Labels:          r.Node.Labels,
			Properties:      r.Node.Properties,
			Score:           r.Score,
			SimilarityScore: r.SimilarityScore,
			ConfidenceScore: r.ConfidenceScore,
			RecencyScore:    r.RecencyScore,
			UtilityScore:    r.UtilityScore,
			RetrievalSource: r.RetrievalSource,
		}
	}

	writeJSON(w, http.StatusOK, resp)
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

	node := results[0]
	writeJSON(w, http.StatusOK, scoredNodeResponse{
		ID:              node.Node.ID.String(),
		Namespace:       node.Node.Namespace,
		Labels:          node.Node.Labels,
		Properties:      node.Node.Properties,
		Score:           node.Score,
		SimilarityScore: node.SimilarityScore,
		ConfidenceScore: node.ConfidenceScore,
		RecencyScore:    node.RecencyScore,
		UtilityScore:    node.UtilityScore,
		RetrievalSource: node.RetrievalSource,
	})
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
