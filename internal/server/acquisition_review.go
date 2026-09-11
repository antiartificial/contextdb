package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/pkg/client"
)

type acquisitionReviewDecisionRequest struct {
	Mode  string `json:"mode"`
	Actor string `json:"actor,omitempty"`
	Note  string `json:"note,omitempty"`
}

type acquisitionReviewCandidatesResponse struct {
	Candidates []client.AcquisitionReviewCandidate `json:"candidates"`
}

// registerAcquisitionReviewRoutes keeps the optional approval workflow out of
// the core REST route table while exposing a cohesive acquisition review API.
func (s *RESTServer) registerAcquisitionReviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/namespaces/{ns}/acquisition/review/candidates", s.handleAcquisitionReviewCandidates)
	mux.HandleFunc("POST /v1/namespaces/{ns}/acquisition/review/candidates/{id}/approve", s.handleApproveAcquisitionReviewCandidate)
	mux.HandleFunc("POST /v1/namespaces/{ns}/acquisition/review/candidates/{id}/reject", s.handleRejectAcquisitionReviewCandidate)
}

func (s *RESTServer) acquisitionReviewHandle(r *http.Request, mode string) *client.NamespaceHandle {
	ns := r.PathValue("ns")
	if tenant := TenantFromContext(r.Context()); tenant != "" {
		ns = tenant + "/" + ns
	}
	return s.db.Namespace(ns, resolveMode(mode))
}

func (s *RESTServer) handleAcquisitionReviewCandidates(w http.ResponseWriter, r *http.Request) {
	var after time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid after timestamp: %w", err))
			return
		}
		after = parsed
	}
	candidates, err := s.acquisitionReviewHandle(r, r.URL.Query().Get("mode")).AcquisitionReviewCandidates(r.Context(), after)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, acquisitionReviewCandidatesResponse{Candidates: candidates})
}

func acquisitionReviewDecisionBody(r *http.Request) (acquisitionReviewDecisionRequest, error) {
	var body acquisitionReviewDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		return body, err
	}
	return body, nil
}

func acquisitionReviewCandidateID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid acquisition review candidate id: %w", err)
	}
	return id, nil
}

func (s *RESTServer) handleApproveAcquisitionReviewCandidate(w http.ResponseWriter, r *http.Request) {
	body, err := acquisitionReviewDecisionBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id, err := acquisitionReviewCandidateID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.acquisitionReviewHandle(r, body.Mode).ApproveAcquisitionReviewCandidate(r.Context(), id, client.AcquisitionReviewDecisionRequest{Actor: body.Actor, Note: body.Note})
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *RESTServer) handleRejectAcquisitionReviewCandidate(w http.ResponseWriter, r *http.Request) {
	body, err := acquisitionReviewDecisionBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id, err := acquisitionReviewCandidateID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.acquisitionReviewHandle(r, body.Mode).RejectAcquisitionReviewCandidate(r.Context(), id, client.AcquisitionReviewDecisionRequest{Actor: body.Actor, Note: body.Note})
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
