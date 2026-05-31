// Package admin provides a minimal HTML dashboard for contextdb.
//
// Mount the handler at /admin/ to serve the dashboard.
//
//	mux.Handle("/admin/", admin.New(db))
package admin

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/observe"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/pkg/client"
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
	h.mux.HandleFunc("GET /admin/api/belief", h.handleBeliefAudit)
	h.mux.HandleFunc("GET /admin/api/search", h.handleSearch)
	h.mux.HandleFunc("GET /admin/api/timetravel", h.handleTimeTravel)
	h.mux.HandleFunc("GET /admin/api/diff", h.handleDiff)

	return h
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(audit)
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
