package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/pkg/client"
)

// registerReviewWorkerRoutes keeps worker execution server-owned. Request
// bodies select only a named evaluator; webhook endpoints remain deployment
// configuration and are never accepted from callers.
func (s *RESTServer) registerReviewWorkerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/namespaces/{ns}/review/worker/cycle", s.handleReviewWorkerCycle)
	mux.HandleFunc("GET /v1/namespaces/{ns}/review/worker/runs", s.handleReviewWorkerRuns)
}

func (s *RESTServer) handleReviewWorkerCycle(w http.ResponseWriter, r *http.Request) {
	var req client.ReviewWorkerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Evaluator == "webhook" && strings.TrimSpace(os.Getenv("CONTEXTDB_REVIEW_WEBHOOK_URL")) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("webhook evaluator is not configured on this server"))
		return
	}
	if req.Evaluator == "webhook" {
		req.WebhookURL = os.Getenv("CONTEXTDB_REVIEW_WEBHOOK_URL")
		req.WebhookToken = os.Getenv("CONTEXTDB_REVIEW_WEBHOOK_TOKEN")
		req.Timeout = 10 * time.Second
	}
	ns := r.PathValue("ns")
	if tenant := TenantFromContext(r.Context()); tenant != "" {
		ns = tenant + "/" + ns
	}
	run, err := s.db.Namespace(ns, namespace.ModeGeneral).RunReviewWorker(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}
func (s *RESTServer) handleReviewWorkerRuns(w http.ResponseWriter, r *http.Request) {
	var after time.Time
	if raw := r.URL.Query().Get("after"); raw != "" {
		var err error
		after, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	ns := r.PathValue("ns")
	if tenant := TenantFromContext(r.Context()); tenant != "" {
		ns = tenant + "/" + ns
	}
	runs, err := s.db.Namespace(ns, namespace.ModeGeneral).ReviewWorkerRuns(r.Context(), after)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}
