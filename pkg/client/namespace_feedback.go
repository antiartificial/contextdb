package client

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/ingest"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/google/uuid"
	"time"
)

// FeedbackResult describes the result of a claim or memory feedback operation.
type FeedbackResult struct {
	NodeID            uuid.UUID
	Action            string
	Confidence        float64
	Utility           float64
	SourceID          string
	SourceCredibility float64
	Reason            string
}

// FeedbackEvent is an auditable record of validate/refute/useful/stale feedback.
type FeedbackEvent struct {
	EventID           uuid.UUID `json:"event_id,omitempty"`
	Namespace         string    `json:"namespace"`
	NodeID            uuid.UUID `json:"node_id"`
	NodeVersion       uint64    `json:"node_version,omitempty"`
	Action            string    `json:"action"`
	Confidence        float64   `json:"confidence"`
	Utility           float64   `json:"utility"`
	SourceID          string    `json:"source_id,omitempty"`
	SourceCredibility float64   `json:"source_credibility,omitempty"`
	Reason            string    `json:"reason,omitempty"`
	Quality           int       `json:"quality,omitempty"`
	TxTime            time.Time `json:"tx_time"`
}

// SourceTrustPoint records one observed source credibility value over time.
type SourceTrustPoint struct {
	SourceID          string    `json:"source_id"`
	NodeID            uuid.UUID `json:"node_id"`
	Action            string    `json:"action"`
	SourceCredibility float64   `json:"source_credibility"`
	Reason            string    `json:"reason,omitempty"`
	TxTime            time.Time `json:"tx_time"`
}

// ValidateClaim records that a claim has been externally validated.
func (h *NamespaceHandle) ValidateClaim(ctx context.Context, nodeID uuid.UUID) (FeedbackResult, error) {
	return h.applyFeedback(ctx, nodeID, feedbackUpdate{
		action:          "validated",
		confidenceDelta: 0.15,
		utilityDelta:    0.05,
		sourceValidated: ptrBool(true),
		quality:         5,
	})
}

// RefuteClaim records that a claim has been externally refuted.
func (h *NamespaceHandle) RefuteClaim(ctx context.Context, nodeID uuid.UUID, reason string) (FeedbackResult, error) {
	return h.applyFeedback(ctx, nodeID, feedbackUpdate{
		action:          "refuted",
		confidenceSet:   ptrFloat64(0.05),
		utilityDelta:    -0.2,
		sourceValidated: ptrBool(false),
		quality:         1,
		reason:          reason,
	})
}

// MarkUseful records positive task-outcome feedback for an agent memory.
func (h *NamespaceHandle) MarkUseful(ctx context.Context, nodeID uuid.UUID, quality int) (FeedbackResult, error) {
	if quality == 0 {
		quality = 5
	}
	return h.applyFeedback(ctx, nodeID, feedbackUpdate{
		action:       "useful",
		utilityDelta: 0.15,
		quality:      quality,
	})
}

// MarkStale records that a node is no longer useful as current context.
func (h *NamespaceHandle) MarkStale(ctx context.Context, nodeID uuid.UUID, reason string) (FeedbackResult, error) {
	return h.applyFeedback(ctx, nodeID, feedbackUpdate{
		action:          "stale",
		confidenceDelta: -0.1,
		utilityDelta:    -0.25,
		quality:         1,
		reason:          reason,
	})
}

// FeedbackEvents returns durable feedback audit events after the given time.
func (h *NamespaceHandle) FeedbackEvents(ctx context.Context, after time.Time) ([]FeedbackEvent, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, after)
	if err != nil {
		return nil, fmt.Errorf("feedback events: %w", err)
	}
	out := make([]FeedbackEvent, 0, len(events))
	for _, event := range events {
		if event.Type != store.EventFeedback {
			continue
		}
		var feedback FeedbackEvent
		if err := json.Unmarshal(event.Payload, &feedback); err != nil {
			return nil, fmt.Errorf("feedback events: decode %s: %w", event.ID, err)
		}
		feedback.EventID = event.ID
		if feedback.Namespace == "" {
			feedback.Namespace = event.Namespace
		}
		if feedback.TxTime.IsZero() {
			feedback.TxTime = event.TxTime
		}
		out = append(out, feedback)
	}
	return out, nil
}

// SourceTrustTimeline returns credibility observations for one source after the given time.
func (h *NamespaceHandle) SourceTrustTimeline(ctx context.Context, sourceID string, after time.Time) ([]SourceTrustPoint, error) {
	events, err := h.FeedbackEvents(ctx, after)
	if err != nil {
		return nil, err
	}
	out := make([]SourceTrustPoint, 0, len(events))
	for _, event := range events {
		if event.SourceID != sourceID || event.SourceCredibility == 0 {
			continue
		}
		out = append(out, SourceTrustPoint{
			SourceID:          event.SourceID,
			NodeID:            event.NodeID,
			Action:            event.Action,
			SourceCredibility: event.SourceCredibility,
			Reason:            event.Reason,
			TxTime:            event.TxTime,
		})
	}
	return out, nil
}

type feedbackUpdate struct {
	action          string
	confidenceDelta float64
	confidenceSet   *float64
	utilityDelta    float64
	sourceValidated *bool
	quality         int
	reason          string
}

func (h *NamespaceHandle) applyFeedback(ctx context.Context, nodeID uuid.UUID, update feedbackUpdate) (FeedbackResult, error) {
	node, err := h.db.graph.GetNode(ctx, h.cfg.ID, nodeID)
	if err != nil {
		return FeedbackResult{}, fmt.Errorf("%s: get node: %w", update.action, err)
	}
	if node == nil {
		return FeedbackResult{}, fmt.Errorf("%s: node %s not found", update.action, nodeID)
	}
	if node.Properties == nil {
		node.Properties = make(map[string]any)
	}

	now := time.Now()
	baseConf := node.Confidence
	if baseConf == 0 {
		baseConf = 0.5
	}
	if update.confidenceSet != nil {
		node.Confidence = clampFeedback(*update.confidenceSet)
	} else if update.confidenceDelta != 0 {
		node.Confidence = clampFeedback(baseConf + update.confidenceDelta)
	}

	utility := propertyFloat(node.Properties, "utility", 1.0)
	if update.utilityDelta != 0 {
		utility = clampFeedback(utility + update.utilityDelta)
		node.Properties["utility"] = utility
	}
	if update.quality != 0 {
		sm2 := core.Sm2FromProperties(node.Properties)
		node.Properties = sm2.Update(update.quality).ToProperties(node.Properties)
	}

	countKey := update.action + "_count"
	node.Properties[countKey] = propertyFloat(node.Properties, countKey, 0) + 1
	node.Properties[update.action+"_at"] = now.Format(time.RFC3339)
	if update.reason != "" {
		node.Properties[update.action+"_reason"] = update.reason
	}
	node.TxTime = now

	result := FeedbackResult{
		NodeID:     nodeID,
		Action:     update.action,
		Confidence: node.Confidence,
		Utility:    utility,
		Reason:     update.reason,
	}

	sourceID, _ := node.Properties["source_id"].(string)
	result.SourceID = sourceID
	var plannedSource *core.Source
	if sourceID != "" && update.sourceValidated != nil {
		src, err := h.db.graph.GetSourceByExternalID(ctx, h.cfg.ID, sourceID)
		if err != nil {
			return FeedbackResult{}, fmt.Errorf("%s: get source: %w", update.action, err)
		}
		if src != nil {
			src.BayesianUpdate(*update.sourceValidated)
			plannedSource = src
			result.SourceCredibility = src.EffectiveCredibility()
		}
	}

	// The audit event is fixed in the intent with the computed source and node
	// state. Recovery therefore never applies a feedback delta twice.
	feedback := FeedbackEvent{EventID: uuid.New(), Namespace: h.cfg.ID, NodeID: node.ID, NodeVersion: node.Version + 1, Action: update.action, Confidence: result.Confidence, Utility: result.Utility, SourceID: result.SourceID, SourceCredibility: result.SourceCredibility, Reason: result.Reason, Quality: update.quality, TxTime: now}
	payload, err := json.Marshal(feedback)
	if err != nil {
		return FeedbackResult{}, fmt.Errorf("%s: marshal feedback event: %w", update.action, err)
	}
	if err := ingest.PersistFeedback(ctx, h.db.graph, h.db.log, ingest.FeedbackPlan{
		ID:     uuid.New(),
		Source: plannedSource,
		Node:   *node,
		Event:  store.Event{ID: feedback.EventID, Namespace: h.cfg.ID, Type: store.EventFeedback, Payload: payload, TxTime: now},
	}); err != nil {
		return FeedbackResult{}, fmt.Errorf("%s: persist recoverable feedback: %w", update.action, err)
	}
	return result, nil
}

func (h *NamespaceHandle) appendFeedbackEvent(ctx context.Context, node *core.Node, result FeedbackResult, update feedbackUpdate, txTime time.Time) error {
	feedback := FeedbackEvent{
		Namespace:         h.cfg.ID,
		NodeID:            result.NodeID,
		NodeVersion:       node.Version,
		Action:            result.Action,
		Confidence:        result.Confidence,
		Utility:           result.Utility,
		SourceID:          result.SourceID,
		SourceCredibility: result.SourceCredibility,
		Reason:            result.Reason,
		Quality:           update.quality,
		TxTime:            txTime,
	}
	payload, err := json.Marshal(feedback)
	if err != nil {
		return fmt.Errorf("%s: marshal feedback event: %w", update.action, err)
	}
	if err := h.db.log.Append(ctx, store.Event{
		Namespace: h.cfg.ID,
		Type:      store.EventFeedback,
		Payload:   payload,
		TxTime:    txTime,
	}); err != nil {
		return fmt.Errorf("%s: append feedback event: %w", update.action, err)
	}
	return nil
}

func clampFeedback(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
