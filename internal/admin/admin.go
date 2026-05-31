// Package admin provides a minimal HTML dashboard for contextdb.
//
// Mount the handler at /admin/ to serve the dashboard.
//
//	mux.Handle("/admin/", admin.New(db))
package admin

import (
	"encoding/json"
	"html/template"
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
	tmpl  *template.Template
}

// New creates an http.Handler that serves the admin UI at /admin/.
func New(db *client.DB) http.Handler {
	graph, _, _, _ := db.Stores()
	h := &adminHandler{
		db:    db,
		graph: graph,
		mux:   http.NewServeMux(),
	}
	h.tmpl = template.Must(template.New("index").Parse(indexHTML))

	h.mux.HandleFunc("GET /admin/", h.handleIndex)
	h.mux.HandleFunc("GET /admin/debugger", h.handleIndex)
	h.mux.HandleFunc("GET /admin/api/stats", h.handleStats)
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
	stats := h.db.Stats()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	h.tmpl.Execute(w, stats)
}

// handleStats returns JSON stats for the dashboard.
func (h *adminHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := h.db.Stats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
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

const indexHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>contextdb admin</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: system-ui, -apple-system, sans-serif; background: #0f1117; color: #e1e4e8; line-height: 1.6; }
  .container { max-width: 1120px; margin: 0 auto; padding: 2rem; }
  h1 { font-size: 1.5rem; color: #58a6ff; margin-bottom: 1.5rem; }
  h2 { font-size: 1.1rem; color: #8b949e; margin-bottom: 1rem; border-bottom: 1px solid #21262d; padding-bottom: 0.5rem; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1rem; margin-bottom: 2rem; }
  .card { background: #161b22; border: 1px solid #30363d; border-radius: 6px; padding: 1rem; }
  .card .label { font-size: 0.8rem; color: #8b949e; text-transform: uppercase; letter-spacing: 0.05em; }
  .card .value { font-size: 1.8rem; font-weight: 600; color: #58a6ff; margin-top: 0.25rem; }
  .links { list-style: none; }
  .links li { padding: 0.5rem 0; border-bottom: 1px solid #21262d; }
  .links a { color: #58a6ff; text-decoration: none; }
  .links a:hover { text-decoration: underline; }
  .tool { background: #161b22; border: 1px solid #30363d; border-radius: 6px; padding: 1rem; margin-bottom: 2rem; }
  .row { display: grid; grid-template-columns: 1fr 1fr auto; gap: 0.75rem; align-items: end; }
  .search-row { display: grid; grid-template-columns: 1fr 1fr 7rem auto; gap: 0.75rem; align-items: end; margin-bottom: 0.75rem; }
  label { display: block; font-size: 0.8rem; color: #8b949e; margin-bottom: 0.25rem; }
  input { width: 100%; background: #0f1117; color: #e1e4e8; border: 1px solid #30363d; border-radius: 4px; padding: 0.55rem; }
  button { background: #238636; color: white; border: 0; border-radius: 4px; padding: 0.62rem 0.9rem; cursor: pointer; }
  button:hover { background: #2ea043; }
  .results { border: 1px solid #30363d; border-radius: 6px; overflow: hidden; margin-bottom: 0.75rem; }
  .result { display: grid; grid-template-columns: 1fr auto; gap: 1rem; padding: 0.75rem; border-bottom: 1px solid #30363d; background: #0f1117; }
  .result:last-child { border-bottom: 0; }
  .result strong { display: block; color: #e1e4e8; overflow-wrap: anywhere; }
  .meta { color: #8b949e; font-size: 0.78rem; overflow-wrap: anywhere; }
  .ghost { background: #21262d; }
  .ghost:hover { background: #30363d; }
  pre { white-space: pre-wrap; overflow-wrap: anywhere; background: #0f1117; border: 1px solid #30363d; border-radius: 6px; padding: 1rem; max-height: 28rem; overflow: auto; }
  .footer { margin-top: 2rem; font-size: 0.8rem; color: #484f58; }
  @media (max-width: 760px) { .row, .search-row, .result { grid-template-columns: 1fr; } }
</style>
</head>
<body>
<div class="container">
  <h1>contextdb admin</h1>

  <h2>Overview</h2>
  <div class="grid">
    <div class="card">
      <div class="label">Mode</div>
      <div class="value" style="font-size:1.2rem">{{.Mode}}</div>
    </div>
    <div class="card">
      <div class="label">Ingest Total</div>
      <div class="value">{{.IngestTotal}}</div>
    </div>
    <div class="card">
      <div class="label">Admitted</div>
      <div class="value">{{.IngestAdmitted}}</div>
    </div>
    <div class="card">
      <div class="label">Rejected</div>
      <div class="value">{{.IngestRejected}}</div>
    </div>
    <div class="card">
      <div class="label">Retrieval Total</div>
      <div class="value">{{.RetrievalTotal}}</div>
    </div>
    <div class="card">
      <div class="label">Retrieval Errors</div>
      <div class="value">{{.RetrievalErrors}}</div>
    </div>
  </div>

  <h2>Belief Debugger</h2>
  <div class="tool">
    <div class="search-row">
      <div>
        <label for="search-ns">Namespace</label>
        <input id="search-ns" value="default" autocomplete="off">
      </div>
      <div>
        <label for="search-q">Search</label>
        <input id="search-q" placeholder="text, source, label, or UUID" autocomplete="off">
      </div>
      <div>
        <label for="search-limit">Limit</label>
        <input id="search-limit" value="10" inputmode="numeric" autocomplete="off">
      </div>
      <button id="search-run" type="button">Search</button>
    </div>
    <div id="search-results" class="results" hidden></div>
    <div class="row">
      <div>
        <label for="debug-ns">Namespace</label>
        <input id="debug-ns" value="default" autocomplete="off">
      </div>
      <div>
        <label for="debug-id">Node ID</label>
        <input id="debug-id" placeholder="00000000-0000-0000-0000-000000000000" autocomplete="off">
      </div>
      <button id="debug-run" type="button">Inspect</button>
    </div>
    <pre id="debug-output">Enter a namespace and node ID to inspect source, support, contradictions, provenance, and confidence history.</pre>
  </div>

  <h2>Quick Links</h2>
  <ul class="links">
    <li><a href="/v1/ping">Health Check (/v1/ping)</a></li>
    <li><a href="/v1/stats">API Stats (/v1/stats)</a></li>
    <li><a href="/metrics">Prometheus Metrics (/metrics)</a></li>
    <li><a href="/debug/pprof/">Profiling (/debug/pprof/)</a></li>
    <li><a href="/debug/vars">expvar (/debug/vars)</a></li>
  </ul>

  <div class="footer">contextdb admin dashboard</div>
</div>
<script>
const output = document.getElementById('debug-output');
const searchResults = document.getElementById('search-results');
function setDebuggerTarget(ns, id) {
  document.getElementById('debug-ns').value = ns;
  document.getElementById('debug-id').value = id;
}
document.getElementById('search-run').addEventListener('click', async () => {
  const ns = document.getElementById('search-ns').value.trim();
  const q = document.getElementById('search-q').value.trim();
  const limit = document.getElementById('search-limit').value.trim();
  searchResults.hidden = false;
  searchResults.textContent = 'Searching...';
  try {
    const res = await fetch('/admin/api/search?ns=' + encodeURIComponent(ns) + '&q=' + encodeURIComponent(q) + '&limit=' + encodeURIComponent(limit));
    const text = await res.text();
    if (!res.ok) throw new Error(text || res.statusText);
    const data = JSON.parse(text);
    searchResults.innerHTML = '';
    if (!data.results || data.results.length === 0) {
      searchResults.textContent = 'No matching nodes.';
      return;
    }
    for (const result of data.results) {
      const item = document.createElement('div');
      item.className = 'result';
      const detail = document.createElement('div');
      const title = document.createElement('strong');
      title.textContent = result.text || result.id;
      const meta = document.createElement('div');
      meta.className = 'meta';
      meta.textContent = result.id + ' | ' + (result.source_id || 'unknown source') + ' | confidence ' + (result.confidence || 0).toFixed(2) + ' | ' + result.match_reason;
      detail.appendChild(title);
      detail.appendChild(meta);
      const button = document.createElement('button');
      button.className = 'ghost';
      button.type = 'button';
      button.textContent = 'Inspect';
      button.addEventListener('click', () => setDebuggerTarget(ns, result.id));
      item.appendChild(detail);
      item.appendChild(button);
      searchResults.appendChild(item);
    }
  } catch (err) {
    searchResults.textContent = String(err.message || err);
  }
});
document.getElementById('debug-run').addEventListener('click', async () => {
  const ns = document.getElementById('debug-ns').value.trim();
  const id = document.getElementById('debug-id').value.trim();
  output.textContent = 'Loading...';
  try {
    const res = await fetch('/admin/api/belief?ns=' + encodeURIComponent(ns) + '&id=' + encodeURIComponent(id));
    const text = await res.text();
    if (!res.ok) throw new Error(text || res.statusText);
    output.textContent = JSON.stringify(JSON.parse(text), null, 2);
  } catch (err) {
    output.textContent = String(err.message || err);
  }
});
</script>
</body>
</html>`
