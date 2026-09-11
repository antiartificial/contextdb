package server

import (
	"encoding/json"
	"net/http"
)

func (s *RESTServer) registerRecoveryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/namespaces/{ns}/recovery/pending", func(w http.ResponseWriter, r *http.Request) {
		pending, err := s.acquisitionReviewHandle(r, r.URL.Query().Get("mode")).PendingWrites(r.Context())
		if err != nil {
			writeError(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{"pending": pending})
	})
	mux.HandleFunc("POST /v1/namespaces/{ns}/recovery/reconcile", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Execute bool   `json:"execute"`
			Mode    string `json:"mode"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, 400, err)
			return
		}
		h := s.acquisitionReviewHandle(r, req.Mode)
		if req.Execute {
			if err := h.RecoverPendingWrites(r.Context()); err != nil {
				writeError(w, 409, err)
				return
			}
		}
		pending, err := h.PendingWrites(r.Context())
		if err != nil {
			writeError(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{"dry_run": !req.Execute, "pending": pending})
	})
}
