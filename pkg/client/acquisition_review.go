package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/store"
)

// AcquisitionReviewCandidate is connector output held outside the graph until
// an operator explicitly approves it. CandidateID and NodeID are deterministic
// so approval retries address the same durable intent.
type AcquisitionReviewCandidate struct {
	CandidateID uuid.UUID      `json:"candidate_id"`
	Namespace   string         `json:"namespace"`
	TaskID      string         `json:"task_id"`
	ConnectorID string         `json:"connector_id"`
	RunKey      string         `json:"run_key"`
	NodeID      uuid.UUID      `json:"node_id"`
	Content     string         `json:"content"`
	SourceID    string         `json:"source_id"`
	Labels      []string       `json:"labels,omitempty"`
	Confidence  float64        `json:"confidence,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// AcquisitionReviewDecision is an append-only audit event for an acquisition
// candidate. Only admitted is terminal approval; approval_requested is safe to
// replay after an interrupted cross-store write.
type AcquisitionReviewDecision struct {
	EventID     uuid.UUID `json:"event_id"`
	Namespace   string    `json:"namespace"`
	CandidateID uuid.UUID `json:"candidate_id"`
	Status      string    `json:"status"`
	Actor       string    `json:"actor,omitempty"`
	Note        string    `json:"note,omitempty"`
	NodeID      uuid.UUID `json:"node_id,omitempty"`
	TxTime      time.Time `json:"tx_time"`
}

// AcquisitionReviewDecisionRequest supplies auditable operator context.
type AcquisitionReviewDecisionRequest struct {
	Actor string
	Note  string
}

// AcquisitionReviewResult reports the current terminal or attempted outcome.
type AcquisitionReviewResult struct {
	Candidate AcquisitionReviewCandidate `json:"candidate"`
	Decision  AcquisitionReviewDecision  `json:"decision"`
	Write     WriteResult                `json:"write,omitempty"`
}

func acquisitionCandidateID(ns, taskID, connectorID, runKey, content, sourceID string) uuid.UUID {
	identity := strings.Join([]string{ns, taskID, connectorID, runKey, content, sourceID}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return uuid.NewSHA1(uuid.NameSpaceOID, sum[:])
}

func acquisitionCandidateProperties(item AcquisitionPreviewItem) map[string]any {
	properties := make(map[string]any)
	if item.Title != "" {
		properties["title"] = item.Title
	}
	if item.URL != "" {
		properties["url"] = item.URL
	}
	if item.Snippet != "" {
		properties["snippet"] = item.Snippet
	}
	if len(item.Metadata) > 0 {
		properties["acquisition_metadata"] = item.Metadata
	}
	return properties
}

func (h *NamespaceHandle) recordAcquisitionReviewCandidate(ctx context.Context, task AcquisitionTask, connector AcquisitionConnector, run AcquisitionConnectorRun, item AcquisitionPreviewItem, content, sourceID string, labels []string, createdAt time.Time) (AcquisitionReviewCandidate, error) {
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	id := acquisitionCandidateID(h.cfg.ID, task.ID, connector.ID, run.IdempotencyKey, content, sourceID)
	candidate := AcquisitionReviewCandidate{
		CandidateID: id, Namespace: h.cfg.ID, TaskID: task.ID, ConnectorID: connector.ID,
		RunKey: run.IdempotencyKey, NodeID: uuid.NewSHA1(uuid.NameSpaceOID, append([]byte("acquisition-node\x00"), id[:]...)),
		Content: content, SourceID: sourceID, Labels: labels, Confidence: item.Confidence,
		Properties: acquisitionCandidateProperties(item), CreatedAt: createdAt,
	}
	h.acquisitionMu.Lock()
	defer h.acquisitionMu.Unlock()
	release, err := h.acquireCoordinationLease(ctx, "acquisition-candidate", id.String())
	if err != nil {
		return AcquisitionReviewCandidate{}, err
	}
	defer release()
	existing, err := h.AcquisitionReviewCandidates(ctx, time.Time{})
	if err != nil {
		return AcquisitionReviewCandidate{}, err
	}
	for _, previous := range existing {
		if previous.CandidateID != id {
			continue
		}
		canonical := candidate
		canonical.CreatedAt = previous.CreatedAt
		a, err := json.Marshal(canonical)
		if err != nil {
			return AcquisitionReviewCandidate{}, err
		}
		b, err := json.Marshal(previous)
		if err != nil {
			return AcquisitionReviewCandidate{}, err
		}
		if !bytes.Equal(a, b) {
			return AcquisitionReviewCandidate{}, fmt.Errorf("acquisition candidate %s conflicts with stored evidence", id)
		}
		return previous, nil
	}
	payload, err := json.Marshal(candidate)
	if err != nil {
		return AcquisitionReviewCandidate{}, fmt.Errorf("acquisition review candidate: marshal: %w", err)
	}
	// A deterministic event ID makes retries observable as the same intent.
	if err := h.db.log.Append(ctx, store.Event{ID: id, Namespace: h.cfg.ID, Type: store.EventAcquisitionReviewCandidate, Payload: payload, TxTime: createdAt}); err != nil {
		return AcquisitionReviewCandidate{}, fmt.Errorf("acquisition review candidate: append event: %w", err)
	}
	return candidate, nil
}

// AcquisitionReviewCandidates returns the latest durable candidate payload for
// each stable candidate ID. Candidate records do not participate in retrieval.
func (h *NamespaceHandle) AcquisitionReviewCandidates(ctx context.Context, after time.Time) ([]AcquisitionReviewCandidate, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, after)
	if err != nil {
		return nil, fmt.Errorf("acquisition review candidates: %w", err)
	}
	byID := map[uuid.UUID]AcquisitionReviewCandidate{}
	for _, event := range events {
		if event.Type != store.EventAcquisitionReviewCandidate {
			continue
		}
		var candidate AcquisitionReviewCandidate
		if err := json.Unmarshal(event.Payload, &candidate); err != nil {
			return nil, fmt.Errorf("acquisition review candidates: decode %s: %w", event.ID, err)
		}
		if candidate.CandidateID == uuid.Nil {
			candidate.CandidateID = event.ID
		}
		if candidate.Namespace == "" {
			candidate.Namespace = event.Namespace
		}
		if candidate.CreatedAt.IsZero() {
			candidate.CreatedAt = event.TxTime
		}
		byID[candidate.CandidateID] = candidate
	}
	out := make([]AcquisitionReviewCandidate, 0, len(byID))
	for _, candidate := range byID {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (h *NamespaceHandle) acquisitionReviewDecisions(ctx context.Context) (map[uuid.UUID]AcquisitionReviewDecision, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("acquisition review decisions: %w", err)
	}
	latest := map[uuid.UUID]AcquisitionReviewDecision{}
	for _, event := range events {
		if event.Type != store.EventAcquisitionReviewDecision {
			continue
		}
		var decision AcquisitionReviewDecision
		if err := json.Unmarshal(event.Payload, &decision); err != nil {
			return nil, fmt.Errorf("acquisition review decisions: decode %s: %w", event.ID, err)
		}
		if decision.CandidateID == uuid.Nil || (latest[decision.CandidateID].TxTime.After(decision.TxTime)) {
			continue
		}
		if decision.TxTime.IsZero() {
			decision.TxTime = event.TxTime
		}
		latest[decision.CandidateID] = decision
	}
	return latest, nil
}

func (h *NamespaceHandle) recordAcquisitionReviewDecision(ctx context.Context, candidate AcquisitionReviewCandidate, status string, req AcquisitionReviewDecisionRequest) (AcquisitionReviewDecision, error) {
	decision := AcquisitionReviewDecision{EventID: uuid.New(), Namespace: h.cfg.ID, CandidateID: candidate.CandidateID, Status: status, Actor: strings.TrimSpace(req.Actor), Note: strings.TrimSpace(req.Note), NodeID: candidate.NodeID, TxTime: time.Now().UTC()}
	payload, err := json.Marshal(decision)
	if err != nil {
		return AcquisitionReviewDecision{}, fmt.Errorf("acquisition review decision: marshal: %w", err)
	}
	if err := h.db.log.Append(ctx, store.Event{ID: decision.EventID, Namespace: h.cfg.ID, Type: store.EventAcquisitionReviewDecision, Payload: payload, TxTime: decision.TxTime}); err != nil {
		return AcquisitionReviewDecision{}, fmt.Errorf("acquisition review decision: append event: %w", err)
	}
	return decision, nil
}

func (h *NamespaceHandle) acquisitionReviewCandidate(ctx context.Context, id uuid.UUID) (AcquisitionReviewCandidate, error) {
	candidates, err := h.AcquisitionReviewCandidates(ctx, time.Time{})
	if err != nil {
		return AcquisitionReviewCandidate{}, err
	}
	for _, candidate := range candidates {
		if candidate.CandidateID == id {
			return candidate, nil
		}
	}
	return AcquisitionReviewCandidate{}, fmt.Errorf("acquisition review candidate %s not found", id)
}

func (h *NamespaceHandle) acquisitionReviewItems(ctx context.Context, after time.Time) ([]ReviewItem, error) {
	candidates, err := h.AcquisitionReviewCandidates(ctx, after)
	if err != nil {
		return nil, err
	}
	decisions, err := h.acquisitionReviewDecisions(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]ReviewItem, 0, len(candidates))
	for _, candidate := range candidates {
		decision := decisions[candidate.CandidateID]
		if decision.Status == "admitted" || decision.Status == "rejected" || decision.Status == "admission_rejected" {
			continue
		}
		status := decision.Status
		if status == "" {
			status = "pending"
		}
		items = append(items, ReviewItem{
			ID: "acquisition_candidate:" + candidate.CandidateID.String(), Type: "acquisition_candidate", Priority: 0.8,
			Reason: "acquired content awaits explicit review before admission", NodeID: candidate.NodeID,
			SourceID: candidate.SourceID, Text: candidate.Content, CreatedAt: candidate.CreatedAt,
			Suggested: "approve or reject the acquired candidate", Confidence: candidate.Confidence,
			Status: status, Owner: decision.Actor, Note: decision.Note, ReviewedAt: decision.TxTime,
		})
	}
	return items, nil
}

// ApproveAcquisitionReviewCandidate admits one explicitly approved candidate.
// A recovery-aware stable NodeID lets retries complete a partial write before
// the terminal admitted audit event is appended.
func (h *NamespaceHandle) ApproveAcquisitionReviewCandidate(ctx context.Context, id uuid.UUID, req AcquisitionReviewDecisionRequest) (AcquisitionReviewResult, error) {
	h.acquisitionMu.Lock()
	defer h.acquisitionMu.Unlock()
	release, err := h.acquireCoordinationLease(ctx, "acquisition-candidate", id.String())
	if err != nil {
		return AcquisitionReviewResult{}, fmt.Errorf("acquisition review candidate lease: %w", err)
	}
	defer release()
	candidate, err := h.acquisitionReviewCandidate(ctx, id)
	if err != nil {
		return AcquisitionReviewResult{}, err
	}
	decisions, err := h.acquisitionReviewDecisions(ctx)
	if err != nil {
		return AcquisitionReviewResult{}, err
	}
	if current, ok := decisions[id]; ok && (current.Status == "rejected" || current.Status == "admission_rejected") {
		return AcquisitionReviewResult{Candidate: candidate, Decision: current}, fmt.Errorf("acquisition review candidate %s was rejected", id)
	}
	if current, ok := decisions[id]; ok && current.Status == "admitted" {
		return AcquisitionReviewResult{Candidate: candidate, Decision: current, Write: WriteResult{NodeID: candidate.NodeID, Admitted: true, Reason: "already admitted"}}, nil
	}
	if current, ok := decisions[id]; !ok || current.Status != "approval_requested" {
		if _, err := h.recordAcquisitionReviewDecision(ctx, candidate, "approval_requested", req); err != nil {
			return AcquisitionReviewResult{}, err
		}
	}
	written, err := h.Write(ctx, WriteRequest{NodeID: candidate.NodeID, IdempotencyKey: "acquisition-review:" + candidate.CandidateID.String(), Content: candidate.Content, SourceID: candidate.SourceID, Labels: candidate.Labels, Properties: candidate.Properties, Confidence: candidate.Confidence})
	if err != nil {
		_, auditErr := h.recordAcquisitionReviewDecision(ctx, candidate, "admission_failed", req)
		if auditErr != nil {
			return AcquisitionReviewResult{}, auditErr
		}
		return AcquisitionReviewResult{Candidate: candidate, Write: written}, err
	}
	if !written.Admitted {
		decision, auditErr := h.recordAcquisitionReviewDecision(ctx, candidate, "admission_rejected", req)
		if auditErr != nil {
			return AcquisitionReviewResult{Candidate: candidate, Write: written}, auditErr
		}
		return AcquisitionReviewResult{Candidate: candidate, Decision: decision, Write: written}, fmt.Errorf("acquisition review candidate %s was not admitted: %s", id, written.Reason)
	}
	decision, err := h.recordAcquisitionReviewDecision(ctx, candidate, "admitted", req)
	if err != nil {
		return AcquisitionReviewResult{Candidate: candidate, Write: written}, err
	}
	return AcquisitionReviewResult{Candidate: candidate, Decision: decision, Write: written}, nil
}

// RejectAcquisitionReviewCandidate records a terminal, idempotent rejection.
func (h *NamespaceHandle) RejectAcquisitionReviewCandidate(ctx context.Context, id uuid.UUID, req AcquisitionReviewDecisionRequest) (AcquisitionReviewResult, error) {
	h.acquisitionMu.Lock()
	defer h.acquisitionMu.Unlock()
	release, err := h.acquireCoordinationLease(ctx, "acquisition-candidate", id.String())
	if err != nil {
		return AcquisitionReviewResult{}, fmt.Errorf("acquisition review candidate lease: %w", err)
	}
	defer release()
	candidate, err := h.acquisitionReviewCandidate(ctx, id)
	if err != nil {
		return AcquisitionReviewResult{}, err
	}
	decisions, err := h.acquisitionReviewDecisions(ctx)
	if err != nil {
		return AcquisitionReviewResult{}, err
	}
	if current, ok := decisions[id]; ok && current.Status == "rejected" {
		return AcquisitionReviewResult{Candidate: candidate, Decision: current}, nil
	}
	if current, ok := decisions[id]; ok && current.Status == "admitted" {
		return AcquisitionReviewResult{Candidate: candidate, Decision: current}, fmt.Errorf("acquisition review candidate %s was already admitted", id)
	}
	decision, err := h.recordAcquisitionReviewDecision(ctx, candidate, "rejected", req)
	if err != nil {
		return AcquisitionReviewResult{}, err
	}
	return AcquisitionReviewResult{Candidate: candidate, Decision: decision}, nil
}
