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

	"github.com/antiartificial/contextdb/pkg/client"
)

// adminHandler serves the admin dashboard.
type adminHandler struct {
	db  *client.DB
	mux *http.ServeMux
	tmpl *template.Template
}

// New creates an http.Handler that serves the admin UI at /admin/.
func New(db *client.DB) http.Handler {
	h := &adminHandler{
		db:  db,
		mux: http.NewServeMux(),
	}
	h.tmpl = template.Must(template.New("index").Parse(indexHTML))

	h.mux.HandleFunc("GET /admin/", h.handleIndex)
	h.mux.HandleFunc("GET /admin/api/stats", h.handleStats)

	return h
}

func (h *adminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
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

const indexHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>contextdb admin</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: system-ui, -apple-system, sans-serif; background: #0f1117; color: #e1e4e8; line-height: 1.6; }
  .container { max-width: 960px; margin: 0 auto; padding: 2rem; }
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
  .footer { margin-top: 2rem; font-size: 0.8rem; color: #484f58; }
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
</body>
</html>`
