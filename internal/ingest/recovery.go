package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// WritePlan is the durable unit used for a write which spans graph and vector
// stores. IDs must be assigned before Persist is called: replays use them to
// replace the same records rather than create duplicates.
type WritePlan struct {
	ID          uuid.UUID         `json:"id"`
	RequestHash string            `json:"request_hash,omitempty"`
	Node        *core.Node        `json:"node,omitempty"`
	Vector      *core.VectorEntry `json:"vector,omitempty"`
	Edges       []core.Edge       `json:"edges,omitempty"`
}

// PendingWrite describes a durable cross-store operation that has not recorded
// completion. It is intended for inspection and repair UIs.
type PendingWrite struct {
	OperationID uuid.UUID
	NodeID      uuid.UUID
	Namespace   string
}

func PendingWrites(ctx context.Context, log store.EventLog, namespace string) ([]PendingWrite, error) {
	events, err := log.SinceAll(ctx, namespace, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("read write recovery log: %w", err)
	}
	plans := map[uuid.UUID]WritePlan{}
	completed := map[uuid.UUID]bool{}
	var order []uuid.UUID
	for _, event := range events {
		switch event.Type {
		case store.EventWriteIntent:
			var plan WritePlan
			if json.Unmarshal(event.Payload, &plan) == nil && plan.ID != uuid.Nil {
				if _, ok := plans[plan.ID]; !ok {
					plans[plan.ID] = plan
					order = append(order, plan.ID)
				}
			}
		case store.EventWriteComplete:
			var complete struct {
				OperationID uuid.UUID `json:"operation_id"`
			}
			if json.Unmarshal(event.Payload, &complete) == nil {
				completed[complete.OperationID] = true
			}
		}
	}
	out := make([]PendingWrite, 0)
	for _, id := range order {
		if completed[id] {
			continue
		}
		plan := plans[id]
		nodeID := uuid.Nil
		if plan.Node != nil {
			nodeID = plan.Node.ID
		}
		out = append(out, PendingWrite{OperationID: id, NodeID: nodeID, Namespace: namespace})
	}
	return out, nil
}

// FeedbackPlan saves computed feedback state before source, node, and event
// changes. A replay uses these values and never applies the Bayesian delta again.
type FeedbackPlan struct {
	ID     uuid.UUID    `json:"id"`
	Source *core.Source `json:"source,omitempty"`
	Node   core.Node    `json:"node"`
	Event  store.Event  `json:"event"`
}

func PersistFeedback(ctx context.Context, graph store.GraphStore, log store.EventLog, plan FeedbackPlan) error {
	if plan.ID == uuid.Nil || plan.Node.ID == uuid.Nil || plan.Event.ID == uuid.Nil {
		return errors.New("feedback intent requires operation, node, and event IDs")
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	ns := plan.Node.Namespace
	if err := log.Append(ctx, store.Event{ID: plan.ID, Namespace: ns, Type: store.EventFeedbackIntent, Payload: payload, TxTime: time.Now()}); err != nil {
		return fmt.Errorf("append feedback intent: %w", err)
	}
	if err := applyFeedbackPlan(ctx, graph, log, plan, nil); err != nil {
		return &RecoveryError{OperationID: plan.ID, Err: err}
	}
	if err := feedbackEvent(ctx, log, ns, store.EventFeedbackComplete, plan.ID, ""); err != nil {
		return &RecoveryError{OperationID: plan.ID, Err: err}
	}
	return nil
}

func RecoverFeedback(ctx context.Context, graph store.GraphStore, log store.EventLog, ns string) error {
	events, err := log.SinceAll(ctx, ns, time.Time{})
	if err != nil {
		return err
	}
	plans := map[uuid.UUID]FeedbackPlan{}
	done := map[uuid.UUID]bool{}
	stages := map[uuid.UUID]map[string]bool{}
	var order []uuid.UUID
	for _, e := range events {
		switch e.Type {
		case store.EventFeedbackIntent:
			var p FeedbackPlan
			if json.Unmarshal(e.Payload, &p) != nil || p.ID == uuid.Nil {
				return fmt.Errorf("decode feedback intent %s", e.ID)
			}
			if _, ok := plans[p.ID]; !ok {
				plans[p.ID] = p
				order = append(order, p.ID)
			}
		case store.EventFeedbackComplete:
			var p struct {
				OperationID uuid.UUID `json:"operation_id"`
			}
			_ = json.Unmarshal(e.Payload, &p)
			done[p.OperationID] = true
		case store.EventFeedbackStage:
			var p struct {
				OperationID uuid.UUID `json:"operation_id"`
				Stage       string    `json:"stage"`
			}
			_ = json.Unmarshal(e.Payload, &p)
			if p.OperationID != uuid.Nil && p.Stage != "" {
				if stages[p.OperationID] == nil {
					stages[p.OperationID] = map[string]bool{}
				}
				stages[p.OperationID][p.Stage] = true
			}
		}
	}
	for _, id := range order {
		if done[id] {
			continue
		}
		if err := applyFeedbackPlan(ctx, graph, log, plans[id], stages[id]); err != nil {
			return &RecoveryError{OperationID: id, Err: err}
		}
		if err := feedbackEvent(ctx, log, ns, store.EventFeedbackComplete, id, ""); err != nil {
			return &RecoveryError{OperationID: id, Err: err}
		}
	}
	return nil
}

func applyFeedbackPlan(ctx context.Context, graph store.GraphStore, log store.EventLog, plan FeedbackPlan, done map[string]bool) error {
	ns := plan.Node.Namespace
	if plan.Source != nil && !done["source"] {
		current, err := graph.GetSourceByExternalID(ctx, ns, plan.Source.ExternalID)
		if err != nil {
			return err
		}
		if done == nil || current == nil {
			if err := graph.UpsertSource(ctx, *plan.Source); err != nil {
				return err
			}
		} else if !reflect.DeepEqual(*current, *plan.Source) {
			return &RecoveryConflictError{OperationID: plan.ID, NodeID: plan.Node.ID, Reason: "current source differs"}
		}
		if err := feedbackEvent(ctx, log, ns, store.EventFeedbackStage, plan.ID, "source"); err != nil {
			return err
		}
	}
	if !done["node"] {
		current, err := graph.GetNode(ctx, ns, plan.Node.ID)
		if err != nil {
			return err
		}
		if done == nil || current == nil {
			if err := graph.UpsertNode(ctx, plan.Node); err != nil {
				return err
			}
		} else if !sameReplayNode(*current, plan.Node) {
			return &RecoveryConflictError{OperationID: plan.ID, NodeID: plan.Node.ID, Reason: "current node differs or has been retracted"}
		}
		if err := feedbackEvent(ctx, log, ns, store.EventFeedbackStage, plan.ID, "node"); err != nil {
			return err
		}
	}
	if !done["event"] {
		exists, err := eventExists(ctx, log, ns, plan.Event.ID)
		if err != nil {
			return err
		}
		if !exists {
			if err := log.Append(ctx, plan.Event); err != nil {
				return err
			}
		}
		if err := feedbackEvent(ctx, log, ns, store.EventFeedbackStage, plan.ID, "event"); err != nil {
			return err
		}
	}
	return nil
}

func feedbackEvent(ctx context.Context, log store.EventLog, ns string, typ store.EventType, id uuid.UUID, stage string) error {
	payload, err := json.Marshal(struct {
		OperationID uuid.UUID `json:"operation_id"`
		Stage       string    `json:"stage,omitempty"`
	}{id, stage})
	if err != nil {
		return err
	}
	return log.Append(ctx, store.Event{ID: uuid.New(), Namespace: ns, Type: typ, Payload: payload, TxTime: time.Now()})
}
func eventExists(ctx context.Context, log store.EventLog, ns string, id uuid.UUID) (bool, error) {
	events, err := log.SinceAll(ctx, ns, time.Time{})
	if err != nil {
		return false, err
	}
	for _, e := range events {
		if e.ID == id {
			return true, nil
		}
	}
	return false, nil
}

// RecoveryError reports a write that remains durable but incomplete. Callers
// must not treat this as success; the operation can be repaired by Recover.
type RecoveryError struct {
	OperationID uuid.UUID
	Err         error
}

// RecoveryConflictError means replay found a later or retracted graph record.
// It intentionally leaves the intent pending for an operator to inspect rather
// than overwriting history or resurrecting removed content.
type RecoveryConflictError struct {
	OperationID uuid.UUID
	NodeID      uuid.UUID
	Reason      string
}

func (e *RecoveryConflictError) Error() string {
	return fmt.Sprintf("write %s requires manual recovery for node %s: %s", e.OperationID, e.NodeID, e.Reason)
}

func (e *RecoveryError) Error() string {
	return fmt.Sprintf("write %s is pending recovery: %v", e.OperationID, e.Err)
}

func (e *RecoveryError) Unwrap() error { return e.Err }

// Persist records the write intent before it changes any target store. The
// completion record is only appended after all graph, vector, and edge writes
// have succeeded. This is recovery, not a distributed transaction: a returned
// error means some effects may already be visible and will be reconciled.
func Persist(ctx context.Context, graph store.GraphStore, vecs store.VectorIndex, log store.EventLog, plan WritePlan) error {
	return persist(ctx, graph, vecs, log, plan, true)
}

// PersistNew records a new operation without scanning historical intents.
// Callers that supply a retry/idempotency key must use Persist instead.
func PersistNew(ctx context.Context, graph store.GraphStore, vecs store.VectorIndex, log store.EventLog, plan WritePlan) error {
	return persist(ctx, graph, vecs, log, plan, false)
}

func persist(ctx context.Context, graph store.GraphStore, vecs store.VectorIndex, log store.EventLog, plan WritePlan, lookup bool) error {
	if plan.ID == uuid.Nil {
		plan.ID = uuid.New()
	}
	if err := validatePlan(plan); err != nil {
		return err
	}
	ns := planNamespace(plan)
	known := false
	if lookup {
		known, complete, persisted, err := findIntent(ctx, log, ns, plan.ID)
		if err != nil {
			return err
		}
		if known {
			if persisted.RequestHash != plan.RequestHash {
				return fmt.Errorf("idempotency key conflicts with the existing write intent")
			}
			if complete {
				return nil
			}
			plan = persisted
		}
	}
	if !known {
		payload, err := json.Marshal(plan)
		if err != nil {
			return fmt.Errorf("marshal write intent: %w", err)
		}
		if err := log.Append(ctx, store.Event{ID: plan.ID, Namespace: ns, Type: store.EventWriteIntent, Payload: payload, TxTime: time.Now()}); err != nil {
			return fmt.Errorf("append write intent: %w", err)
		}
	}
	if err := apply(ctx, graph, vecs, log, plan, nil); err != nil {
		return &RecoveryError{OperationID: plan.ID, Err: err}
	}
	if err := appendComplete(ctx, log, ns, plan.ID); err != nil {
		return &RecoveryError{OperationID: plan.ID, Err: fmt.Errorf("append completion: %w", err)}
	}
	return nil
}

func findIntent(ctx context.Context, log store.EventLog, namespace string, id uuid.UUID) (known, complete bool, plan WritePlan, err error) {
	events, err := log.SinceAll(ctx, namespace, time.Time{})
	if err != nil {
		return false, false, WritePlan{}, fmt.Errorf("read write recovery log: %w", err)
	}
	for _, event := range events {
		switch event.Type {
		case store.EventWriteIntent:
			var candidate WritePlan
			if json.Unmarshal(event.Payload, &candidate) == nil && candidate.ID == id {
				known, plan = true, candidate
			}
		case store.EventWriteComplete:
			var completion struct {
				OperationID uuid.UUID `json:"operation_id"`
			}
			if json.Unmarshal(event.Payload, &completion) == nil && completion.OperationID == id {
				complete = true
			}
		}
	}
	return known, complete, plan, nil
}

// Recover replays every uncompleted intent for namespace. It reads SinceAll so
// a compactor marking unrelated events processed cannot hide recovery work.
func Recover(ctx context.Context, graph store.GraphStore, vecs store.VectorIndex, log store.EventLog, namespace string) error {
	events, err := log.SinceAll(ctx, namespace, time.Time{})
	if err != nil {
		return fmt.Errorf("read write recovery log: %w", err)
	}
	completed := make(map[uuid.UUID]struct{})
	intents := make(map[uuid.UUID]WritePlan)
	stages := make(map[uuid.UUID]map[string]struct{})
	var order []uuid.UUID
	for _, event := range events {
		switch event.Type {
		case store.EventWriteComplete:
			var complete struct {
				OperationID uuid.UUID `json:"operation_id"`
			}
			if json.Unmarshal(event.Payload, &complete) == nil && complete.OperationID != uuid.Nil {
				completed[complete.OperationID] = struct{}{}
			}
		case store.EventWriteIntent:
			var plan WritePlan
			if err := json.Unmarshal(event.Payload, &plan); err != nil {
				return fmt.Errorf("decode write intent %s: %w", event.ID, err)
			}
			if plan.ID == uuid.Nil {
				plan.ID = event.ID
			}
			if err := validatePlan(plan); err != nil {
				return fmt.Errorf("invalid write intent %s: %w", event.ID, err)
			}
			if _, exists := intents[plan.ID]; !exists {
				order = append(order, plan.ID)
				intents[plan.ID] = plan
			}
		case store.EventWriteStage:
			var stage struct {
				OperationID uuid.UUID `json:"operation_id"`
				Stage       string    `json:"stage"`
			}
			if json.Unmarshal(event.Payload, &stage) == nil && stage.OperationID != uuid.Nil && stage.Stage != "" {
				if stages[stage.OperationID] == nil {
					stages[stage.OperationID] = make(map[string]struct{})
				}
				stages[stage.OperationID][stage.Stage] = struct{}{}
			}
		}
	}
	for _, id := range order {
		if _, ok := completed[id]; ok {
			continue
		}
		plan := intents[id]
		if err := apply(ctx, graph, vecs, log, plan, stages[id]); err != nil {
			return &RecoveryError{OperationID: id, Err: err}
		}
		if err := appendComplete(ctx, log, namespace, id); err != nil {
			return &RecoveryError{OperationID: id, Err: fmt.Errorf("append completion: %w", err)}
		}
	}
	return nil
}

func apply(ctx context.Context, graph store.GraphStore, vecs store.VectorIndex, log store.EventLog, plan WritePlan, done map[string]struct{}) error {
	if plan.Node != nil {
		if _, ok := done["node"]; !ok {
			current, err := graph.GetNode(ctx, plan.Node.Namespace, plan.Node.ID)
			if err != nil {
				return fmt.Errorf("inspect node before replay: %w", err)
			}
			if current == nil {
				if err := graph.UpsertNode(ctx, *plan.Node); err != nil {
					return fmt.Errorf("upsert node: %w", err)
				}
			} else if !sameReplayNode(*current, *plan.Node) {
				return &RecoveryConflictError{OperationID: plan.ID, NodeID: plan.Node.ID, Reason: "current node differs or has been retracted"}
			}
			if err := appendStage(ctx, log, planNamespace(plan), plan.ID, "node"); err != nil {
				return fmt.Errorf("acknowledge node stage: %w", err)
			}
		}
	}
	if plan.Vector != nil {
		if _, ok := done["vector"]; !ok {
			if err := vecs.Index(ctx, *plan.Vector); err != nil {
				return fmt.Errorf("index vector: %w", err)
			}
			if plan.Node != nil {
				if reg, ok := vecs.(interface{ RegisterNode(core.Node) }); ok {
					reg.RegisterNode(*plan.Node)
				}
			}
			if err := appendStage(ctx, log, planNamespace(plan), plan.ID, "vector"); err != nil {
				return fmt.Errorf("acknowledge vector stage: %w", err)
			}
		}
	}
	for i, edge := range plan.Edges {
		stage := fmt.Sprintf("edge:%d", i)
		if _, ok := done[stage]; ok {
			continue
		}
		if err := graph.UpsertEdge(ctx, edge); err != nil {
			return fmt.Errorf("upsert edge %s: %w", edge.ID, err)
		}
		if err := appendStage(ctx, log, planNamespace(plan), plan.ID, stage); err != nil {
			return fmt.Errorf("acknowledge edge %s: %w", edge.ID, err)
		}
	}
	return nil
}

func appendStage(ctx context.Context, log store.EventLog, namespace string, id uuid.UUID, stage string) error {
	payload, err := json.Marshal(struct {
		OperationID uuid.UUID `json:"operation_id"`
		Stage       string    `json:"stage"`
	}{OperationID: id, Stage: stage})
	if err != nil {
		return err
	}
	return log.Append(ctx, store.Event{ID: uuid.New(), Namespace: namespace, Type: store.EventWriteStage, Payload: payload, TxTime: time.Now()})
}

func appendComplete(ctx context.Context, log store.EventLog, namespace string, id uuid.UUID) error {
	payload, err := json.Marshal(struct {
		OperationID uuid.UUID `json:"operation_id"`
	}{OperationID: id})
	if err != nil {
		return err
	}
	return log.Append(ctx, store.Event{ID: uuid.New(), Namespace: namespace, Type: store.EventWriteComplete, Payload: payload, TxTime: time.Now()})
}

func validatePlan(plan WritePlan) error {
	if plan.ID == uuid.Nil {
		return errors.New("operation ID is required")
	}
	if plan.Node == nil && plan.Vector == nil && len(plan.Edges) == 0 {
		return errors.New("write intent has no side effects")
	}
	if plan.Node != nil && plan.Node.ID == uuid.Nil {
		return errors.New("node ID is required")
	}
	if plan.Vector != nil && plan.Vector.ID == uuid.Nil {
		return errors.New("vector ID is required")
	}
	for _, edge := range plan.Edges {
		if edge.ID == uuid.Nil {
			return errors.New("edge ID is required")
		}
	}
	return nil
}

func planNamespace(plan WritePlan) string {
	if plan.Node != nil {
		return plan.Node.Namespace
	}
	if plan.Vector != nil {
		return plan.Vector.Namespace
	}
	return plan.Edges[0].Namespace
}

func sameReplayNode(current, intended core.Node) bool {
	return current.ID == intended.ID &&
		current.Namespace == intended.Namespace &&
		reflect.DeepEqual(current.Labels, intended.Labels) &&
		reflect.DeepEqual(current.Properties, intended.Properties) &&
		reflect.DeepEqual(current.Vector, intended.Vector) &&
		current.ModelID == intended.ModelID &&
		current.Fingerprint == intended.Fingerprint &&
		current.Confidence == intended.Confidence &&
		current.ValidFrom.Equal(intended.ValidFrom) &&
		current.ValidUntil == nil && intended.ValidUntil == nil
}
