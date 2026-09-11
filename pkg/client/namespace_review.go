package client

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/google/uuid"
	"sort"
	"strings"
	"time"
)

// ReviewQueueRequest configures claim review queue generation.
type ReviewQueueRequest struct {
	After                           time.Time
	Now                             time.Time
	LowConfidenceThreshold          float64
	SourceTrustThreshold            float64
	SourceTrustDropThreshold        float64
	SourceRefutationThreshold       int
	EscalationAfter                 time.Duration
	SourceAnomalyEscalationPriority float64
	Types                           []string
	SourceID                        string
	Status                          string
	Owner                           string
	Limit                           int
}

// SourceQuarantineRequest builds or applies a dry-run-first source quarantine plan.
type SourceQuarantineRequest struct {
	After                     time.Time
	Now                       time.Time
	SourceTrustThreshold      float64
	SourceTrustDropThreshold  float64
	SourceRefutationThreshold int
	SourceID                  string
	Limit                     int
	Execute                   bool
	Labels                    []string
}

// SourceQuarantinePlan describes sources recommended for quarantine or exclusion.
type SourceQuarantinePlan struct {
	Namespace   string                      `json:"namespace"`
	GeneratedAt time.Time                   `json:"generated_at"`
	DryRun      bool                        `json:"dry_run"`
	Executed    bool                        `json:"executed"`
	Candidates  []SourceQuarantineCandidate `json:"candidates"`
}

// SourceQuarantineCandidate is one source with repeated refutations or trust drift.
type SourceQuarantineCandidate struct {
	SourceID          string      `json:"source_id"`
	Action            string      `json:"action"`
	Reason            string      `json:"reason"`
	Priority          float64     `json:"priority"`
	LatestCredibility float64     `json:"latest_credibility,omitempty"`
	NodeIDs           []uuid.UUID `json:"node_ids,omitempty"`
	ExistingLabels    []string    `json:"existing_labels,omitempty"`
	SuggestedLabels   []string    `json:"suggested_labels"`
	AppliedLabels     []string    `json:"applied_labels,omitempty"`
	Applied           bool        `json:"applied,omitempty"`
}

// ReviewDecisionRequest records durable workflow state for a derived review task.
type ReviewDecisionRequest struct {
	ReviewID  string
	Status    string
	Owner     string
	Decision  string
	Note      string
	RecheckAt time.Time
}

// ReviewDecision is an append-only workflow event attached to a derived review item.
type ReviewDecision struct {
	EventID   uuid.UUID `json:"event_id,omitempty"`
	Namespace string    `json:"namespace"`
	ReviewID  string    `json:"review_id"`
	Status    string    `json:"status"`
	Owner     string    `json:"owner,omitempty"`
	Decision  string    `json:"decision,omitempty"`
	Note      string    `json:"note,omitempty"`
	RecheckAt time.Time `json:"recheck_at,omitempty"`
	TxTime    time.Time `json:"tx_time"`
}

// ReviewItem is a derived operator task for claims that need attention.
type ReviewItem struct {
	ID                 string      `json:"id"`
	Type               string      `json:"type"`
	Priority           float64     `json:"priority"`
	Reason             string      `json:"reason"`
	NodeID             uuid.UUID   `json:"node_id,omitempty"`
	NodeIDs            []uuid.UUID `json:"node_ids,omitempty"`
	SourceID           string      `json:"source_id,omitempty"`
	Action             string      `json:"action,omitempty"`
	Text               string      `json:"text,omitempty"`
	CreatedAt          time.Time   `json:"created_at"`
	Suggested          string      `json:"suggested_action"`
	Confidence         float64     `json:"confidence,omitempty"`
	Status             string      `json:"status,omitempty"`
	Owner              string      `json:"owner,omitempty"`
	Decision           string      `json:"decision,omitempty"`
	Note               string      `json:"note,omitempty"`
	RecheckAt          time.Time   `json:"recheck_at,omitempty"`
	ReviewedAt         time.Time   `json:"reviewed_at,omitempty"`
	Escalated          bool        `json:"escalated,omitempty"`
	EscalationLevel    string      `json:"escalation_level,omitempty"`
	EscalationReason   string      `json:"escalation_reason,omitempty"`
	EscalationAgeHours float64     `json:"escalation_age_hours,omitempty"`
}

// ReviewEscalationDigest summarizes escalated review queue items for dashboards.
type ReviewEscalationDigest struct {
	EventID         uuid.UUID               `json:"event_id,omitempty"`
	Namespace       string                  `json:"namespace,omitempty"`
	GeneratedAt     time.Time               `json:"generated_at"`
	EscalationAfter time.Duration           `json:"escalation_after,omitempty"`
	Note            string                  `json:"note,omitempty"`
	TotalEscalated  int                     `json:"total_escalated"`
	Groups          []ReviewEscalationGroup `json:"groups"`
}

// ReviewEscalationGroup is one owner/source/type/severity bucket in a digest.
type ReviewEscalationGroup struct {
	Owner           string   `json:"owner"`
	SourceID        string   `json:"source_id,omitempty"`
	Type            string   `json:"type"`
	EscalationLevel string   `json:"escalation_level"`
	Count           int      `json:"count"`
	MaxPriority     float64  `json:"max_priority"`
	MaxAgeHours     float64  `json:"max_age_hours"`
	ReviewIDs       []string `json:"review_ids,omitempty"`
}

// RecordReviewDecision appends durable workflow state for a derived review task.
func (h *NamespaceHandle) RecordReviewDecision(ctx context.Context, req ReviewDecisionRequest) (ReviewDecision, error) {
	reviewID := strings.TrimSpace(req.ReviewID)
	if reviewID == "" {
		return ReviewDecision{}, fmt.Errorf("review decision: review_id is required")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "open"
	}
	switch status {
	case "open", "assigned", "resolved", "snoozed":
	default:
		return ReviewDecision{}, fmt.Errorf("review decision: unsupported status %q", status)
	}
	if status == "snoozed" && req.RecheckAt.IsZero() {
		return ReviewDecision{}, fmt.Errorf("review decision: recheck_at is required for snoozed status")
	}

	decision := ReviewDecision{
		EventID:   uuid.New(),
		Namespace: h.cfg.ID,
		ReviewID:  reviewID,
		Status:    status,
		Owner:     strings.TrimSpace(req.Owner),
		Decision:  strings.TrimSpace(req.Decision),
		Note:      strings.TrimSpace(req.Note),
		RecheckAt: req.RecheckAt,
		TxTime:    time.Now(),
	}
	payload, err := json.Marshal(decision)
	if err != nil {
		return ReviewDecision{}, fmt.Errorf("review decision: marshal: %w", err)
	}
	event := store.Event{
		ID:        decision.EventID,
		Namespace: h.cfg.ID,
		Type:      store.EventReviewDecision,
		Payload:   payload,
		TxTime:    decision.TxTime,
	}
	if err := h.db.log.Append(ctx, event); err != nil {
		return ReviewDecision{}, fmt.Errorf("review decision: append event: %w", err)
	}
	return decision, nil
}

// ReviewDecisions returns durable review workflow events after the given time.
func (h *NamespaceHandle) ReviewDecisions(ctx context.Context, after time.Time) ([]ReviewDecision, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, after)
	if err != nil {
		return nil, fmt.Errorf("review decisions: %w", err)
	}
	out := make([]ReviewDecision, 0, len(events))
	for _, event := range events {
		if event.Type != store.EventReviewDecision {
			continue
		}
		var decision ReviewDecision
		if err := json.Unmarshal(event.Payload, &decision); err != nil {
			return nil, fmt.Errorf("review decisions: decode %s: %w", event.ID, err)
		}
		decision.EventID = event.ID
		if decision.Namespace == "" {
			decision.Namespace = event.Namespace
		}
		if decision.TxTime.IsZero() {
			decision.TxTime = event.TxTime
		}
		out = append(out, decision)
	}
	return out, nil
}

// ReviewQueue derives operator review tasks from feedback, low-confidence claims, and contradictions.
func (h *NamespaceHandle) ReviewQueue(ctx context.Context, req ReviewQueueRequest) ([]ReviewItem, error) {
	threshold := req.LowConfidenceThreshold
	if threshold == 0 {
		threshold = 0.35
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	items := make([]ReviewItem, 0)

	events, err := h.FeedbackEvents(ctx, req.After)
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		switch event.Action {
		case "refuted", "stale":
			items = append(items, ReviewItem{
				ID:         fmt.Sprintf("feedback:%s", event.EventID),
				Type:       event.Action,
				Priority:   reviewFeedbackPriority(event),
				Reason:     event.Reason,
				NodeID:     event.NodeID,
				SourceID:   event.SourceID,
				Action:     event.Action,
				CreatedAt:  event.TxTime,
				Suggested:  reviewSuggestion(event.Action),
				Confidence: event.Confidence,
			})
		}
	}
	items = append(items, sourceTrustAnomalyItems(events, req)...)
	acquisitionItems, err := h.acquisitionReviewItems(ctx, req.After)
	if err != nil {
		return nil, fmt.Errorf("review queue: acquisition candidates: %w", err)
	}
	items = append(items, acquisitionItems...)

	nodes, err := h.db.graph.ValidAt(ctx, h.cfg.ID, now, nil)
	if err != nil {
		return nil, fmt.Errorf("review queue: scan nodes: %w", err)
	}
	for _, node := range nodes {
		confidence := node.Confidence
		if confidence == 0 {
			confidence = 0.5
		}
		if confidence <= threshold {
			items = append(items, ReviewItem{
				ID:         "low_confidence:" + node.ID.String(),
				Type:       "low_confidence",
				Priority:   threshold - confidence + 0.2,
				Reason:     fmt.Sprintf("confidence %.2f is below %.2f", confidence, threshold),
				NodeID:     node.ID,
				SourceID:   nodeSourceID(node),
				Text:       core.NodeText(node),
				CreatedAt:  node.TxTime,
				Suggested:  "validate, refute, or attach stronger evidence",
				Confidence: confidence,
			})
		}
	}

	clusters, err := retrieval.FindConflictClusters(ctx, h.db.graph, h.cfg.ID, nil)
	if err != nil {
		return nil, fmt.Errorf("review queue: conflicts: %w", err)
	}
	for _, cluster := range clusters {
		nodeIDs := make([]uuid.UUID, 0, len(cluster.Nodes))
		for _, node := range cluster.Nodes {
			nodeIDs = append(nodeIDs, node.ID)
		}
		items = append(items, ReviewItem{
			ID:        "conflict:" + reviewIDsKey(nodeIDs),
			Type:      "conflict",
			Priority:  0.7 + cluster.CredibilityGap,
			Reason:    fmt.Sprintf("%d claims are connected by contradiction edges", len(cluster.Nodes)),
			NodeIDs:   nodeIDs,
			CreatedAt: now,
			Suggested: "compare evidence and resolve the contradiction",
		})
	}

	decisions, err := h.ReviewDecisions(ctx, time.Time{})
	if err != nil {
		return nil, err
	}
	items = applyReviewDecisions(items, latestReviewDecisions(decisions), now)
	items = applyReviewEscalations(items, req, now)
	items = filterReviewItems(items, req)

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority == items[j].Priority {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].Priority > items[j].Priority
	})
	if req.Limit > 0 && len(items) > req.Limit {
		items = items[:req.Limit]
	}
	return items, nil
}

// SourceQuarantine builds a source quarantine plan and applies labels only when Execute is true.
func (h *NamespaceHandle) SourceQuarantine(ctx context.Context, req SourceQuarantineRequest) (SourceQuarantinePlan, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	labels := dedupeStrings(req.Labels)
	if len(labels) == 0 {
		labels = []string{"flagged", "quarantined"}
	}
	refutationThreshold := req.SourceRefutationThreshold
	if refutationThreshold == 0 {
		refutationThreshold = 2
	}
	trustThreshold := req.SourceTrustThreshold
	if trustThreshold == 0 {
		trustThreshold = 0.35
	}
	dropThreshold := req.SourceTrustDropThreshold
	if dropThreshold == 0 {
		dropThreshold = 0.2
	}
	items, err := h.ReviewQueue(ctx, ReviewQueueRequest{
		After:                     req.After,
		Now:                       now,
		SourceTrustThreshold:      trustThreshold,
		SourceTrustDropThreshold:  dropThreshold,
		SourceRefutationThreshold: refutationThreshold,
		Types:                     []string{"source_trust_anomaly"},
		SourceID:                  req.SourceID,
		Limit:                     req.Limit,
	})
	if err != nil {
		return SourceQuarantinePlan{}, err
	}
	plan := SourceQuarantinePlan{
		Namespace:   h.cfg.ID,
		GeneratedAt: now,
		DryRun:      !req.Execute,
		Executed:    req.Execute,
		Candidates:  make([]SourceQuarantineCandidate, 0, len(items)),
	}
	for _, item := range items {
		sourceID := strings.TrimSpace(item.SourceID)
		if sourceID == "" {
			continue
		}
		src, err := h.db.graph.GetSourceByExternalID(ctx, h.cfg.ID, sourceID)
		if err != nil {
			return SourceQuarantinePlan{}, err
		}
		if src == nil {
			defaultSource := core.DefaultSource(h.cfg.ID, sourceID)
			src = &defaultSource
		}
		candidate := SourceQuarantineCandidate{
			SourceID:          sourceID,
			Action:            item.Action,
			Reason:            item.Reason,
			Priority:          item.Priority,
			LatestCredibility: item.Confidence,
			NodeIDs:           item.NodeIDs,
			ExistingLabels:    append([]string{}, src.Labels...),
			SuggestedLabels:   append([]string{}, labels...),
		}
		if req.Execute {
			src.Labels = dedupeStrings(append(src.Labels, labels...))
			if err := h.db.graph.UpsertSource(ctx, *src); err != nil {
				return SourceQuarantinePlan{}, err
			}
			candidate.Applied = true
			candidate.AppliedLabels = append([]string{}, src.Labels...)
		}
		plan.Candidates = append(plan.Candidates, candidate)
	}
	return plan, nil
}

// ReviewEscalationDigest groups escalated review queue items by owner, source, type, and level.
func (h *NamespaceHandle) ReviewEscalationDigest(ctx context.Context, req ReviewQueueRequest) (ReviewEscalationDigest, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	if req.EscalationAfter <= 0 {
		req.EscalationAfter = 72 * time.Hour
	}
	req.Now = now
	items, err := h.ReviewQueue(ctx, req)
	if err != nil {
		return ReviewEscalationDigest{}, err
	}
	digest := ReviewEscalationDigest{
		Namespace:       h.cfg.ID,
		GeneratedAt:     now,
		EscalationAfter: req.EscalationAfter,
		Groups:          []ReviewEscalationGroup{},
	}
	grouped := map[string]*ReviewEscalationGroup{}
	for _, item := range items {
		if !item.Escalated {
			continue
		}
		digest.TotalEscalated++
		owner := strings.TrimSpace(item.Owner)
		if owner == "" {
			owner = "unassigned"
		}
		sourceID := strings.TrimSpace(item.SourceID)
		if sourceID == "" {
			sourceID = "unsourced"
		}
		level := strings.TrimSpace(item.EscalationLevel)
		if level == "" {
			level = "escalated"
		}
		key := strings.Join([]string{owner, sourceID, item.Type, level}, "\x00")
		group := grouped[key]
		if group == nil {
			group = &ReviewEscalationGroup{
				Owner:           owner,
				SourceID:        sourceID,
				Type:            item.Type,
				EscalationLevel: level,
			}
			grouped[key] = group
		}
		group.Count++
		group.MaxPriority = maxFloat(group.MaxPriority, item.Priority)
		group.MaxAgeHours = maxFloat(group.MaxAgeHours, item.EscalationAgeHours)
		group.ReviewIDs = append(group.ReviewIDs, item.ID)
	}
	for _, group := range grouped {
		sort.Strings(group.ReviewIDs)
		digest.Groups = append(digest.Groups, *group)
	}
	sort.SliceStable(digest.Groups, func(i, j int) bool {
		left, right := digest.Groups[i], digest.Groups[j]
		if left.Count != right.Count {
			return left.Count > right.Count
		}
		if left.MaxAgeHours != right.MaxAgeHours {
			return left.MaxAgeHours > right.MaxAgeHours
		}
		return strings.Join([]string{left.Owner, left.SourceID, left.Type, left.EscalationLevel}, "\x00") <
			strings.Join([]string{right.Owner, right.SourceID, right.Type, right.EscalationLevel}, "\x00")
	})
	return digest, nil
}

// RecordReviewEscalationDigest appends a durable digest snapshot for handoffs.
func (h *NamespaceHandle) RecordReviewEscalationDigest(ctx context.Context, req ReviewQueueRequest, note string) (ReviewEscalationDigest, error) {
	digest, err := h.ReviewEscalationDigest(ctx, req)
	if err != nil {
		return ReviewEscalationDigest{}, err
	}
	digest.EventID = uuid.New()
	digest.Namespace = h.cfg.ID
	digest.Note = strings.TrimSpace(note)
	payload, err := json.Marshal(digest)
	if err != nil {
		return ReviewEscalationDigest{}, fmt.Errorf("review escalation digest: marshal: %w", err)
	}
	event := store.Event{
		ID:        digest.EventID,
		Namespace: h.cfg.ID,
		Type:      store.EventReviewEscalationDigest,
		Payload:   payload,
		TxTime:    digest.GeneratedAt,
	}
	if err := h.db.log.Append(ctx, event); err != nil {
		return ReviewEscalationDigest{}, fmt.Errorf("review escalation digest: append event: %w", err)
	}
	return digest, nil
}

// ReviewEscalationDigests returns durable escalation digest snapshots after the given time.
func (h *NamespaceHandle) ReviewEscalationDigests(ctx context.Context, after time.Time) ([]ReviewEscalationDigest, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, after)
	if err != nil {
		return nil, fmt.Errorf("review escalation digests: %w", err)
	}
	out := make([]ReviewEscalationDigest, 0, len(events))
	for _, event := range events {
		if event.Type != store.EventReviewEscalationDigest {
			continue
		}
		var digest ReviewEscalationDigest
		if err := json.Unmarshal(event.Payload, &digest); err != nil {
			return nil, fmt.Errorf("review escalation digests: decode %s: %w", event.ID, err)
		}
		digest.EventID = event.ID
		if digest.Namespace == "" {
			digest.Namespace = event.Namespace
		}
		if digest.GeneratedAt.IsZero() {
			digest.GeneratedAt = event.TxTime
		}
		out = append(out, digest)
	}
	return out, nil
}

func matchingEscalationGroups(groups []ReviewEscalationGroup, owner, level string) []ReviewEscalationGroup {
	out := make([]ReviewEscalationGroup, 0, len(groups))
	for _, group := range groups {
		if owner != "" && group.Owner != owner {
			continue
		}
		if level != "" && group.EscalationLevel != level {
			continue
		}
		out = append(out, group)
	}
	return out
}

func reviewFeedbackPriority(event FeedbackEvent) float64 {
	switch event.Action {
	case "refuted":
		return 1.0
	case "stale":
		return 0.75
	default:
		return 0.5
	}
}

func reviewSuggestion(action string) string {
	switch action {
	case "refuted":
		return "verify the refutation and retract or replace the claim"
	case "stale":
		return "refresh the claim or mark its replacement"
	default:
		return "review the claim"
	}
}

func reviewIDsKey(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func sourceTrustAnomalyItems(events []FeedbackEvent, req ReviewQueueRequest) []ReviewItem {
	dropThreshold := req.SourceTrustDropThreshold
	lowTrustThreshold := req.SourceTrustThreshold
	refutationThreshold := req.SourceRefutationThreshold

	type sourceState struct {
		firstCredibility  float64
		latestCredibility float64
		latestAt          time.Time
		refutations       int
		nodeIDs           []uuid.UUID
	}
	states := map[string]*sourceState{}
	for _, event := range events {
		if event.SourceID == "" || event.SourceCredibility == 0 {
			continue
		}
		state := states[event.SourceID]
		if state == nil {
			state = &sourceState{firstCredibility: event.SourceCredibility}
			states[event.SourceID] = state
		}
		state.latestCredibility = event.SourceCredibility
		state.latestAt = event.TxTime
		if event.NodeID != uuid.Nil {
			state.nodeIDs = append(state.nodeIDs, event.NodeID)
		}
		if event.Action == "refuted" {
			state.refutations++
		}
	}

	items := make([]ReviewItem, 0, len(states))
	for sourceID, state := range states {
		drop := state.firstCredibility - state.latestCredibility
		reasons := []string{}
		priority := 0.0
		action := ""
		if dropThreshold > 0 && drop >= dropThreshold {
			reasons = append(reasons, fmt.Sprintf("source credibility dropped %.2f", drop))
			priority = maxFloat(priority, 0.65+drop)
			action = "credibility_drop"
		}
		if lowTrustThreshold > 0 && state.latestCredibility <= lowTrustThreshold {
			reasons = append(reasons, fmt.Sprintf("source credibility %.2f is at or below %.2f", state.latestCredibility, lowTrustThreshold))
			priority = maxFloat(priority, 0.75+(lowTrustThreshold-state.latestCredibility))
			if action == "" {
				action = "low_trust"
			}
		}
		if refutationThreshold > 0 && state.refutations >= refutationThreshold {
			reasons = append(reasons, fmt.Sprintf("source has %d recent refutations", state.refutations))
			priority = maxFloat(priority, 0.8+float64(state.refutations)*0.05)
			if action == "" {
				action = "repeated_refutations"
			}
		}
		if len(reasons) == 0 {
			continue
		}
		items = append(items, ReviewItem{
			ID:         "source_trust:" + sourceID,
			Type:       "source_trust_anomaly",
			Priority:   priority,
			Reason:     strings.Join(reasons, "; "),
			NodeIDs:    uniqueUUIDs(state.nodeIDs),
			SourceID:   sourceID,
			Action:     action,
			CreatedAt:  state.latestAt,
			Suggested:  "review recent claims from this source and decide whether to relabel, exclude, or verify manually",
			Confidence: state.latestCredibility,
		})
	}
	return items
}

func latestReviewDecisions(decisions []ReviewDecision) map[string]ReviewDecision {
	latest := make(map[string]ReviewDecision, len(decisions))
	for _, decision := range decisions {
		if existing, ok := latest[decision.ReviewID]; !ok || decision.TxTime.After(existing.TxTime) {
			latest[decision.ReviewID] = decision
		}
	}
	return latest
}

func applyReviewDecisions(items []ReviewItem, decisions map[string]ReviewDecision, now time.Time) []ReviewItem {
	if len(decisions) == 0 {
		return items
	}
	out := make([]ReviewItem, 0, len(items))
	for _, item := range items {
		decision, ok := decisions[item.ID]
		if !ok {
			out = append(out, item)
			continue
		}
		if decision.Status == "resolved" {
			continue
		}
		if decision.Status == "snoozed" && decision.RecheckAt.After(now) {
			continue
		}
		item.Status = decision.Status
		item.Owner = decision.Owner
		item.Decision = decision.Decision
		item.Note = decision.Note
		item.RecheckAt = decision.RecheckAt
		item.ReviewedAt = decision.TxTime
		out = append(out, item)
	}
	return out
}

func applyReviewEscalations(items []ReviewItem, req ReviewQueueRequest, now time.Time) []ReviewItem {
	if req.EscalationAfter <= 0 {
		return items
	}
	sourcePriority := req.SourceAnomalyEscalationPriority
	if sourcePriority == 0 {
		sourcePriority = 0.9
	}
	for i := range items {
		item := &items[i]
		status := reviewItemStatus(*item)
		switch {
		case (status == "assigned" || status == "snoozed") && !item.ReviewedAt.IsZero():
			age := now.Sub(item.ReviewedAt)
			if age >= req.EscalationAfter {
				item.Escalated = true
				item.EscalationLevel = "review_overdue"
				item.EscalationReason = fmt.Sprintf("%s review has waited %.1f hours", status, age.Hours())
				item.EscalationAgeHours = age.Hours()
				item.Priority = maxFloat(item.Priority, 1.0+age.Hours()/168.0)
			}
		case item.Type == "source_trust_anomaly" && item.Priority >= sourcePriority && !item.CreatedAt.IsZero():
			age := now.Sub(item.CreatedAt)
			if age >= req.EscalationAfter {
				item.Escalated = true
				item.EscalationLevel = "source_anomaly_high"
				item.EscalationReason = fmt.Sprintf("source anomaly priority %.2f has waited %.1f hours", item.Priority, age.Hours())
				item.EscalationAgeHours = age.Hours()
				item.Priority = maxFloat(item.Priority, 1.1+age.Hours()/168.0)
			}
		}
	}
	return items
}

func filterReviewItems(items []ReviewItem, req ReviewQueueRequest) []ReviewItem {
	types := stringSet(req.Types)
	sourceID := strings.TrimSpace(req.SourceID)
	status := strings.TrimSpace(req.Status)
	owner := strings.TrimSpace(req.Owner)
	if len(types) == 0 && sourceID == "" && status == "" && owner == "" {
		return items
	}
	out := make([]ReviewItem, 0, len(items))
	for _, item := range items {
		if len(types) > 0 && !types[item.Type] {
			continue
		}
		if sourceID != "" && item.SourceID != sourceID {
			continue
		}
		if status != "" && reviewItemStatus(item) != status {
			continue
		}
		if owner != "" && item.Owner != owner {
			continue
		}
		out = append(out, item)
	}
	return out
}

func reviewItemStatus(item ReviewItem) string {
	if strings.TrimSpace(item.Status) == "" {
		return "open"
	}
	return item.Status
}
