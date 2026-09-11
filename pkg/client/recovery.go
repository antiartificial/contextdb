package client

import (
	"context"
	"encoding/json"
	"github.com/antiartificial/contextdb/internal/store"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/ingest"
)

// PendingWrite is a recoverable cross-store write that has not completed.
type PendingWrite struct {
	OperationID uuid.UUID `json:"operation_id"`
	NodeID      uuid.UUID `json:"node_id"`
	Namespace   string    `json:"namespace"`
	Kind        string    `json:"kind"`
}

// PendingWrites lists writes that remain repairable. It does not replay them.
func (h *NamespaceHandle) PendingWrites(ctx context.Context) ([]PendingWrite, error) {
	pending, err := ingest.PendingWrites(ctx, h.db.log, h.cfg.ID)
	if err != nil {
		return nil, err
	}
	out := make([]PendingWrite, len(pending))
	for i, item := range pending {
		out[i] = PendingWrite{OperationID: item.OperationID, NodeID: item.NodeID, Namespace: item.Namespace, Kind: "write"}
	}
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, time.Time{})
	if err != nil {
		return nil, err
	}
	completed := map[uuid.UUID]bool{}
	for _, event := range events {
		if event.Type != store.EventFeedbackComplete {
			continue
		}
		var marker struct {
			OperationID uuid.UUID `json:"operation_id"`
		}
		if err := json.Unmarshal(event.Payload, &marker); err != nil {
			return nil, err
		}
		completed[marker.OperationID] = true
	}
	for _, event := range events {
		if event.Type != store.EventFeedbackIntent {
			continue
		}
		var plan ingest.FeedbackPlan
		if err := json.Unmarshal(event.Payload, &plan); err != nil {
			return nil, err
		}
		if !completed[plan.ID] {
			out = append(out, PendingWrite{OperationID: plan.ID, NodeID: plan.Node.ID, Namespace: h.cfg.ID, Kind: "feedback"})
		}
	}
	return out, nil
}
