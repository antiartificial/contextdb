package client

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ReviewHandoffRequest filters saved escalation digest snapshots for handoff feeds.
type ReviewHandoffRequest struct {
	After           time.Time
	Owner           string
	EscalationLevel string
	Limit           int
}

// ReviewHandoffWebhookRequest configures dry-run webhook delivery plans for saved handoff feeds.
type ReviewHandoffWebhookRequest struct {
	ReviewHandoffRequest
	TargetURL   string
	Secret      string
	MaxAttempts int
	Now         time.Time
	Execute     bool
	Timeout     time.Duration
	HTTPClient  *http.Client
}

// ReviewHandoffRetryRequest configures explicit retry execution for one failed handoff delivery.
type ReviewHandoffRetryRequest struct {
	After         time.Time
	DigestEventID uuid.UUID
	TargetURL     string
	Secret        string
	MaxAttempts   int
	Now           time.Time
	Execute       bool
	Timeout       time.Duration
	HTTPClient    *http.Client
}

// ReviewHandoffRetryFatigueRequest filters unresolved retry fatigue summaries.
type ReviewHandoffRetryFatigueRequest struct {
	After           time.Time
	Now             time.Time
	Preset          string
	Owner           string
	EscalationLevel string
}

// ReviewHandoffRetryFatiguePreset names a reusable retry fatigue filter lane.
type ReviewHandoffRetryFatiguePreset struct {
	Name             string `json:"name"`
	Owner            string `json:"owner,omitempty"`
	EscalationLevel  string `json:"escalation_level,omitempty"`
	Description      string `json:"description"`
	ExampleRESTQuery string `json:"example_rest_query"`
	ExampleGraphQL   string `json:"example_graphql"`
}

// ReviewHandoffWebhookDelivery describes one dry-run webhook delivery that would be sent.
type ReviewHandoffWebhookDelivery struct {
	TargetURL       string                  `json:"target_url"`
	Method          string                  `json:"method"`
	DryRun          bool                    `json:"dry_run"`
	EventID         uuid.UUID               `json:"event_id"`
	Namespace       string                  `json:"namespace,omitempty"`
	GeneratedAt     time.Time               `json:"generated_at"`
	PlannedAt       time.Time               `json:"planned_at"`
	Owner           string                  `json:"owner,omitempty"`
	EscalationLevel string                  `json:"escalation_level,omitempty"`
	TotalEscalated  int                     `json:"total_escalated"`
	Groups          []ReviewEscalationGroup `json:"groups"`
	Payload         json.RawMessage         `json:"payload"`
	PayloadSHA256   string                  `json:"payload_sha256"`
	Signature       string                  `json:"signature,omitempty"`
	Headers         map[string]string       `json:"headers"`
	Attempt         int                     `json:"attempt"`
	MaxAttempts     int                     `json:"max_attempts"`
	NextRetryAfter  time.Duration           `json:"next_retry_after,omitempty"`
	Executed        bool                    `json:"executed,omitempty"`
	StatusCode      int                     `json:"status_code,omitempty"`
	ResponseBody    string                  `json:"response_body,omitempty"`
	Error           string                  `json:"error,omitempty"`
}

// ReviewHandoffDeliveryReceipt is an append-only audit record for executed webhook delivery.
type ReviewHandoffDeliveryReceipt struct {
	ReceiptID       uuid.UUID `json:"receipt_id,omitempty"`
	DigestEventID   uuid.UUID `json:"digest_event_id"`
	Namespace       string    `json:"namespace,omitempty"`
	TargetURL       string    `json:"target_url"`
	DeliveredAt     time.Time `json:"delivered_at"`
	Owner           string    `json:"owner,omitempty"`
	EscalationLevel string    `json:"escalation_level,omitempty"`
	Success         bool      `json:"success"`
	StatusCode      int       `json:"status_code,omitempty"`
	PayloadSHA256   string    `json:"payload_sha256"`
	ResponseSHA256  string    `json:"response_sha256,omitempty"`
	Error           string    `json:"error,omitempty"`
}

// ReviewHandoffRetryCandidate groups failed delivery receipts that may need retry.
type ReviewHandoffRetryCandidate struct {
	DigestEventID   uuid.UUID `json:"digest_event_id"`
	TargetURL       string    `json:"target_url"`
	LastReceiptID   uuid.UUID `json:"last_receipt_id"`
	LastAttemptAt   time.Time `json:"last_attempt_at"`
	Owner           string    `json:"owner,omitempty"`
	EscalationLevel string    `json:"escalation_level,omitempty"`
	Attempts        int       `json:"attempts"`
	LastStatusCode  int       `json:"last_status_code,omitempty"`
	PayloadSHA256   string    `json:"payload_sha256"`
	LastError       string    `json:"last_error,omitempty"`
}

// ReviewHandoffRetryRecommendation adds dry-run retry pacing guidance to a failed delivery candidate.
type ReviewHandoffRetryRecommendation struct {
	ReviewHandoffRetryCandidate
	RecommendedAfter time.Time `json:"recommended_after"`
	DelaySeconds     int       `json:"delay_seconds"`
	Ready            bool      `json:"ready"`
	Reason           string    `json:"reason"`
}

// ReviewHandoffRetryStatusFamilyCount summarizes retry recommendations by HTTP status family.
type ReviewHandoffRetryStatusFamilyCount struct {
	Family string `json:"family"`
	Count  int    `json:"count"`
}

// ReviewHandoffRetryOwnerCount summarizes retry recommendations by review owner.
type ReviewHandoffRetryOwnerCount struct {
	Owner string `json:"owner"`
	Count int    `json:"count"`
}

// ReviewHandoffRetryEscalationCount summarizes retry recommendations by escalation level.
type ReviewHandoffRetryEscalationCount struct {
	EscalationLevel string `json:"escalation_level"`
	Count           int    `json:"count"`
}

// ReviewHandoffRetryFatigueSummary groups unresolved retry pressure by target URL.
type ReviewHandoffRetryFatigueSummary struct {
	TargetURL        string                                `json:"target_url"`
	Candidates       int                                   `json:"candidates"`
	TotalAttempts    int                                   `json:"total_attempts"`
	Ready            int                                   `json:"ready"`
	Waiting          int                                   `json:"waiting"`
	StatusFamilies   []ReviewHandoffRetryStatusFamilyCount `json:"status_families"`
	Owners           []ReviewHandoffRetryOwnerCount        `json:"owners,omitempty"`
	EscalationLevels []ReviewHandoffRetryEscalationCount   `json:"escalation_levels,omitempty"`
	LastStatusCode   int                                   `json:"last_status_code,omitempty"`
	LastError        string                                `json:"last_error,omitempty"`
	LastAttemptAt    time.Time                             `json:"last_attempt_at,omitempty"`
}

// ReviewHandoffRetryFatigueMarkdown renders endpoint fatigue summaries for handoffs.
func ReviewHandoffRetryFatigueMarkdown(summaries []ReviewHandoffRetryFatigueSummary) string {
	var b strings.Builder
	b.WriteString("# Review Handoff Retry Fatigue\n\n")
	if len(summaries) == 0 {
		b.WriteString("No unresolved retry fatigue.\n")
		return b.String()
	}
	top := summaries[0]
	fmt.Fprintf(&b, "Top failing endpoint: `%s` with %d unresolved candidate(s) and %d total attempt(s).\n\n", markdownInline(top.TargetURL), top.Candidates, top.TotalAttempts)
	b.WriteString("| Target URL | Candidates | Attempts | Ready | Waiting | Owners | Escalations | Status Families | Latest Failure |\n")
	b.WriteString("|:-----------|-----------:|---------:|------:|--------:|:-------|:------------|:----------------|:---------------|\n")
	for _, summary := range summaries {
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %d | %s | %s | %s | %s |\n",
			markdownTable(summary.TargetURL),
			summary.Candidates,
			summary.TotalAttempts,
			summary.Ready,
			summary.Waiting,
			markdownTable(formatRetryOwners(summary.Owners)),
			markdownTable(formatRetryEscalations(summary.EscalationLevels)),
			markdownTable(formatRetryStatusFamilies(summary.StatusFamilies)),
			markdownTable(formatRetryLatestFailure(summary)),
		)
	}
	return b.String()
}

// ReviewHandoffRetryFatiguePresets returns stable named retry fatigue filter lanes.
func ReviewHandoffRetryFatiguePresets() []ReviewHandoffRetryFatiguePreset {
	return []ReviewHandoffRetryFatiguePreset{
		{
			Name:             "review-overdue",
			EscalationLevel:  "review_overdue",
			Description:      "Retry fatigue for review handoffs that escalated because assigned or snoozed work is overdue.",
			ExampleRESTQuery: "preset=review-overdue",
			ExampleGraphQL:   `preset: "review-overdue"`,
		},
		{
			Name:             "source-trust-anomaly",
			EscalationLevel:  "source_trust_anomaly",
			Description:      "Retry fatigue for handoffs generated from source trust anomaly review tasks.",
			ExampleRESTQuery: "preset=source-trust-anomaly",
			ExampleGraphQL:   `preset: "source-trust-anomaly"`,
		},
		{
			Name:             "unassigned-review-overdue",
			Owner:            "unassigned",
			EscalationLevel:  "review_overdue",
			Description:      "Retry fatigue for overdue review handoffs without an assigned owner.",
			ExampleRESTQuery: "preset=unassigned-review-overdue",
			ExampleGraphQL:   `preset: "unassigned-review-overdue"`,
		},
	}
}

// AcquisitionRetryCandidate groups failed connector attempts that may need retry.
type AcquisitionRetryCandidate struct {
	TaskID         string    `json:"task_id"`
	ConnectorID    string    `json:"connector_id"`
	ConnectorType  string    `json:"connector_type"`
	TargetURL      string    `json:"target_url,omitempty"`
	LastReceiptID  uuid.UUID `json:"last_receipt_id"`
	LastAttemptAt  time.Time `json:"last_attempt_at"`
	Attempts       int       `json:"attempts"`
	LastStatusCode int       `json:"last_status_code,omitempty"`
	PayloadSHA256  string    `json:"payload_sha256"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	Retryable      bool      `json:"retryable,omitempty"`
}

// AcquisitionRetryRecommendation adds dry-run retry pacing guidance to a failed acquisition attempt.
type AcquisitionRetryRecommendation struct {
	AcquisitionRetryCandidate
	RecommendedAfter time.Time `json:"recommended_after"`
	DelaySeconds     int       `json:"delay_seconds"`
	Ready            bool      `json:"ready"`
	Reason           string    `json:"reason"`
}

// ReviewHandoffs returns saved digest snapshots filtered for polling by owner or escalation level.
func (h *NamespaceHandle) ReviewHandoffs(ctx context.Context, req ReviewHandoffRequest) ([]ReviewEscalationDigest, error) {
	digests, err := h.ReviewEscalationDigests(ctx, req.After)
	if err != nil {
		return nil, err
	}
	owner := strings.TrimSpace(req.Owner)
	level := strings.TrimSpace(req.EscalationLevel)
	out := make([]ReviewEscalationDigest, 0, len(digests))
	for _, digest := range digests {
		filtered := digest
		if owner != "" || level != "" {
			filtered.Groups = matchingEscalationGroups(digest.Groups, owner, level)
			total := 0
			for _, group := range filtered.Groups {
				total += group.Count
			}
			filtered.TotalEscalated = total
		}
		if len(filtered.Groups) == 0 {
			continue
		}
		out = append(out, filtered)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].GeneratedAt.After(out[j].GeneratedAt)
	})
	if req.Limit > 0 && len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out, nil
}

// ReviewHandoffWebhookPlan returns signed dry-run webhook deliveries for saved handoff snapshots.
func (h *NamespaceHandle) ReviewHandoffWebhookPlan(ctx context.Context, req ReviewHandoffWebhookRequest) ([]ReviewHandoffWebhookDelivery, error) {
	return h.reviewHandoffWebhookDeliveries(ctx, req, false)
}

// ReviewHandoffWebhookDeliver sends handoff webhook payloads when Execute is explicitly true.
func (h *NamespaceHandle) ReviewHandoffWebhookDeliver(ctx context.Context, req ReviewHandoffWebhookRequest) ([]ReviewHandoffWebhookDelivery, error) {
	if !req.Execute {
		return nil, fmt.Errorf("review handoff webhook: execute must be true")
	}
	return h.reviewHandoffWebhookDeliveries(ctx, req, true)
}

// ReviewHandoffWebhookRetry resends one unresolved failed handoff delivery candidate.
func (h *NamespaceHandle) ReviewHandoffWebhookRetry(ctx context.Context, req ReviewHandoffRetryRequest) (ReviewHandoffWebhookDelivery, error) {
	if !req.Execute {
		return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: execute must be true")
	}
	if req.DigestEventID == uuid.Nil {
		return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: digest event id is required")
	}
	target := strings.TrimSpace(req.TargetURL)
	if target == "" {
		return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: target URL is required")
	}
	candidates, err := h.ReviewHandoffRetryCandidates(ctx, req.After)
	if err != nil {
		return ReviewHandoffWebhookDelivery{}, err
	}
	var candidate ReviewHandoffRetryCandidate
	found := false
	for _, item := range candidates {
		if item.DigestEventID == req.DigestEventID && item.TargetURL == target {
			candidate = item
			found = true
			break
		}
	}
	if !found {
		return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: candidate not found")
	}
	digests, err := h.ReviewEscalationDigests(ctx, req.After)
	if err != nil {
		return ReviewHandoffWebhookDelivery{}, err
	}
	for _, digest := range digests {
		if digest.EventID != req.DigestEventID {
			continue
		}
		filtered := digest
		if candidate.Owner != "" || candidate.EscalationLevel != "" {
			filtered.Groups = matchingEscalationGroups(digest.Groups, candidate.Owner, candidate.EscalationLevel)
			total := 0
			for _, group := range filtered.Groups {
				total += group.Count
			}
			filtered.TotalEscalated = total
		}
		if len(filtered.Groups) == 0 {
			return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: candidate groups not found")
		}
		deliveries, err := h.reviewHandoffWebhookDeliveriesForDigests(ctx, []ReviewEscalationDigest{filtered}, ReviewHandoffWebhookRequest{
			ReviewHandoffRequest: ReviewHandoffRequest{
				Owner:           candidate.Owner,
				EscalationLevel: candidate.EscalationLevel,
			},
			TargetURL:   target,
			Secret:      req.Secret,
			MaxAttempts: req.MaxAttempts,
			Now:         req.Now,
			Execute:     req.Execute,
			Timeout:     req.Timeout,
			HTTPClient:  req.HTTPClient,
		}, true)
		if err != nil {
			return ReviewHandoffWebhookDelivery{}, err
		}
		if len(deliveries) == 0 {
			return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: no delivery produced")
		}
		return deliveries[0], nil
	}
	return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: digest not found")
}

func (h *NamespaceHandle) reviewHandoffWebhookDeliveries(ctx context.Context, req ReviewHandoffWebhookRequest, execute bool) ([]ReviewHandoffWebhookDelivery, error) {
	handoffs, err := h.ReviewHandoffs(ctx, req.ReviewHandoffRequest)
	if err != nil {
		return nil, err
	}
	return h.reviewHandoffWebhookDeliveriesForDigests(ctx, handoffs, req, execute)
}

func (h *NamespaceHandle) reviewHandoffWebhookDeliveriesForDigests(ctx context.Context, handoffs []ReviewEscalationDigest, req ReviewHandoffWebhookRequest, execute bool) ([]ReviewHandoffWebhookDelivery, error) {
	target := strings.TrimSpace(req.TargetURL)
	if target == "" {
		return nil, fmt.Errorf("review handoff webhook: target URL is required")
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("review handoff webhook: invalid target URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("review handoff webhook: target URL must be http or https")
	}
	plannedAt := req.Now
	if plannedAt.IsZero() {
		plannedAt = time.Now().UTC()
	}
	maxAttempts := req.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	mode := "dry-run"
	if execute {
		mode = "execute"
	}
	out := make([]ReviewHandoffWebhookDelivery, 0, len(handoffs))
	for _, digest := range handoffs {
		payload, err := json.Marshal(digest)
		if err != nil {
			return nil, fmt.Errorf("review handoff webhook: marshal payload: %w", err)
		}
		sum := sha256.Sum256(payload)
		payloadSHA := hex.EncodeToString(sum[:])
		headers := map[string]string{
			"Content-Type":                 "application/json",
			"X-ContextDB-Delivery-Mode":    mode,
			"X-ContextDB-Handoff-Event-ID": digest.EventID.String(),
			"X-ContextDB-Payload-SHA256":   payloadSHA,
		}
		signature := ""
		if req.Secret != "" {
			mac := hmac.New(sha256.New, []byte(req.Secret))
			_, _ = mac.Write(payload)
			signature = "sha256=" + hex.EncodeToString(mac.Sum(nil))
			headers["X-ContextDB-Signature"] = signature
		}
		delivery := ReviewHandoffWebhookDelivery{
			TargetURL:       target,
			Method:          "POST",
			DryRun:          !execute,
			EventID:         digest.EventID,
			Namespace:       digest.Namespace,
			GeneratedAt:     digest.GeneratedAt,
			PlannedAt:       plannedAt,
			Owner:           strings.TrimSpace(req.Owner),
			EscalationLevel: strings.TrimSpace(req.EscalationLevel),
			TotalEscalated:  digest.TotalEscalated,
			Groups:          digest.Groups,
			Payload:         json.RawMessage(payload),
			PayloadSHA256:   payloadSHA,
			Signature:       signature,
			Headers:         headers,
			Attempt:         1,
			MaxAttempts:     maxAttempts,
			NextRetryAfter:  time.Minute,
		}
		if execute {
			delivery = executeReviewHandoffWebhook(ctx, req, delivery)
			if err := h.recordReviewHandoffDeliveryReceipt(ctx, delivery); err != nil {
				return nil, err
			}
		}
		out = append(out, delivery)
	}
	return out, nil
}

func executeReviewHandoffWebhook(ctx context.Context, req ReviewHandoffWebhookRequest, delivery ReviewHandoffWebhookDelivery) ReviewHandoffWebhookDelivery {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := req.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(callCtx, delivery.Method, delivery.TargetURL, bytes.NewReader(delivery.Payload))
	if err != nil {
		delivery.Executed = true
		delivery.Error = err.Error()
		return delivery
	}
	for key, value := range delivery.Headers {
		httpReq.Header.Set(key, value)
	}
	resp, err := client.Do(httpReq)
	delivery.Executed = true
	if err != nil {
		delivery.Error = err.Error()
		return delivery
	}
	defer resp.Body.Close()
	delivery.StatusCode = resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		delivery.Error = err.Error()
		return delivery
	}
	delivery.ResponseBody = string(body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		delivery.Error = fmt.Sprintf("webhook returned status %d", resp.StatusCode)
	}
	return delivery
}

func (h *NamespaceHandle) recordReviewHandoffDeliveryReceipt(ctx context.Context, delivery ReviewHandoffWebhookDelivery) error {
	responseSHA := ""
	if delivery.ResponseBody != "" {
		sum := sha256.Sum256([]byte(delivery.ResponseBody))
		responseSHA = hex.EncodeToString(sum[:])
	}
	receipt := ReviewHandoffDeliveryReceipt{
		ReceiptID:       uuid.New(),
		DigestEventID:   delivery.EventID,
		Namespace:       h.cfg.ID,
		TargetURL:       delivery.TargetURL,
		DeliveredAt:     time.Now().UTC(),
		Owner:           delivery.Owner,
		EscalationLevel: delivery.EscalationLevel,
		Success:         delivery.Executed && delivery.Error == "" && delivery.StatusCode >= 200 && delivery.StatusCode < 300,
		StatusCode:      delivery.StatusCode,
		PayloadSHA256:   delivery.PayloadSHA256,
		ResponseSHA256:  responseSHA,
		Error:           delivery.Error,
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("review handoff receipt: marshal: %w", err)
	}
	event := store.Event{
		ID:        receipt.ReceiptID,
		Namespace: h.cfg.ID,
		Type:      store.EventReviewHandoffReceipt,
		Payload:   payload,
		TxTime:    receipt.DeliveredAt,
	}
	if err := h.db.log.Append(ctx, event); err != nil {
		return fmt.Errorf("review handoff receipt: append event: %w", err)
	}
	return nil
}

// ReviewHandoffDeliveryReceipts returns delivery receipt audit records after the given time.
func (h *NamespaceHandle) ReviewHandoffDeliveryReceipts(ctx context.Context, after time.Time) ([]ReviewHandoffDeliveryReceipt, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, after)
	if err != nil {
		return nil, fmt.Errorf("review handoff receipts: %w", err)
	}
	out := make([]ReviewHandoffDeliveryReceipt, 0, len(events))
	for _, event := range events {
		if event.Type != store.EventReviewHandoffReceipt {
			continue
		}
		var receipt ReviewHandoffDeliveryReceipt
		if err := json.Unmarshal(event.Payload, &receipt); err != nil {
			return nil, fmt.Errorf("review handoff receipts: decode %s: %w", event.ID, err)
		}
		receipt.ReceiptID = event.ID
		if receipt.Namespace == "" {
			receipt.Namespace = event.Namespace
		}
		if receipt.DeliveredAt.IsZero() {
			receipt.DeliveredAt = event.TxTime
		}
		out = append(out, receipt)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].DeliveredAt.After(out[j].DeliveredAt)
	})
	return out, nil
}

// ReviewHandoffRetryCandidates returns latest failed handoff delivery groups without sending retries.
func (h *NamespaceHandle) ReviewHandoffRetryCandidates(ctx context.Context, after time.Time) ([]ReviewHandoffRetryCandidate, error) {
	receipts, err := h.ReviewHandoffDeliveryReceipts(ctx, after)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(receipts, func(i, j int) bool {
		return receipts[i].DeliveredAt.Before(receipts[j].DeliveredAt)
	})
	candidates := map[string]ReviewHandoffRetryCandidate{}
	for _, receipt := range receipts {
		key := receipt.DigestEventID.String() + "\x00" + receipt.TargetURL
		candidate := candidates[key]
		candidate.Attempts++
		if receipt.Success {
			delete(candidates, key)
			continue
		}
		candidate.DigestEventID = receipt.DigestEventID
		candidate.TargetURL = receipt.TargetURL
		candidate.LastReceiptID = receipt.ReceiptID
		candidate.LastAttemptAt = receipt.DeliveredAt
		candidate.Owner = receipt.Owner
		candidate.EscalationLevel = receipt.EscalationLevel
		candidate.LastStatusCode = receipt.StatusCode
		candidate.PayloadSHA256 = receipt.PayloadSHA256
		candidate.LastError = receipt.Error
		candidates[key] = candidate
	}
	out := make([]ReviewHandoffRetryCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastAttemptAt.After(out[j].LastAttemptAt)
	})
	return out, nil
}

// ReviewHandoffRetryRecommendations returns read-only backoff guidance for unresolved failed handoff deliveries.
func (h *NamespaceHandle) ReviewHandoffRetryRecommendations(ctx context.Context, after time.Time, now time.Time) ([]ReviewHandoffRetryRecommendation, error) {
	candidates, err := h.ReviewHandoffRetryCandidates(ctx, after)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := make([]ReviewHandoffRetryRecommendation, 0, len(candidates))
	for _, candidate := range candidates {
		delay := reviewHandoffRetryBackoff(candidate.Attempts)
		recommendedAfter := candidate.LastAttemptAt.Add(delay)
		ready := !now.Before(recommendedAfter)
		reason := "waiting_for_backoff"
		if ready {
			reason = "ready_for_operator_retry"
		}
		out = append(out, ReviewHandoffRetryRecommendation{
			ReviewHandoffRetryCandidate: candidate,
			RecommendedAfter:            recommendedAfter,
			DelaySeconds:                int(delay.Seconds()),
			Ready:                       ready,
			Reason:                      reason,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Ready != out[j].Ready {
			return out[i].Ready
		}
		return out[i].RecommendedAfter.Before(out[j].RecommendedAfter)
	})
	return out, nil
}

// ReviewHandoffRetryFatigue groups retry recommendations by endpoint without sending retries.
func (h *NamespaceHandle) ReviewHandoffRetryFatigue(ctx context.Context, after time.Time, now time.Time) ([]ReviewHandoffRetryFatigueSummary, error) {
	return h.ReviewHandoffRetryFatigueFiltered(ctx, ReviewHandoffRetryFatigueRequest{After: after, Now: now})
}

// ReviewHandoffRetryFatigueFiltered groups filtered retry recommendations by endpoint without sending retries.
func (h *NamespaceHandle) ReviewHandoffRetryFatigueFiltered(ctx context.Context, req ReviewHandoffRetryFatigueRequest) ([]ReviewHandoffRetryFatigueSummary, error) {
	recommendations, err := h.ReviewHandoffRetryRecommendations(ctx, req.After, req.Now)
	if err != nil {
		return nil, err
	}
	recommendations = filterReviewHandoffRetryRecommendations(recommendations, req)
	summaries := map[string]*ReviewHandoffRetryFatigueSummary{}
	statusFamilies := map[string]map[string]int{}
	owners := map[string]map[string]int{}
	escalationLevels := map[string]map[string]int{}
	for _, recommendation := range recommendations {
		summary := summaries[recommendation.TargetURL]
		if summary == nil {
			summary = &ReviewHandoffRetryFatigueSummary{TargetURL: recommendation.TargetURL}
			summaries[recommendation.TargetURL] = summary
			statusFamilies[recommendation.TargetURL] = map[string]int{}
			owners[recommendation.TargetURL] = map[string]int{}
			escalationLevels[recommendation.TargetURL] = map[string]int{}
		}
		summary.Candidates++
		summary.TotalAttempts += recommendation.Attempts
		if recommendation.Ready {
			summary.Ready++
		} else {
			summary.Waiting++
		}
		family := reviewHandoffStatusFamily(recommendation.LastStatusCode)
		statusFamilies[recommendation.TargetURL][family]++
		owners[recommendation.TargetURL][reviewHandoffFatigueGroupValue(recommendation.Owner, "unassigned")]++
		escalationLevels[recommendation.TargetURL][reviewHandoffFatigueGroupValue(recommendation.EscalationLevel, "none")]++
		if recommendation.LastAttemptAt.After(summary.LastAttemptAt) {
			summary.LastAttemptAt = recommendation.LastAttemptAt
			summary.LastStatusCode = recommendation.LastStatusCode
			summary.LastError = recommendation.LastError
		}
	}
	out := make([]ReviewHandoffRetryFatigueSummary, 0, len(summaries))
	for targetURL, summary := range summaries {
		for family, count := range statusFamilies[targetURL] {
			summary.StatusFamilies = append(summary.StatusFamilies, ReviewHandoffRetryStatusFamilyCount{
				Family: family,
				Count:  count,
			})
		}
		sort.SliceStable(summary.StatusFamilies, func(i, j int) bool {
			return summary.StatusFamilies[i].Family < summary.StatusFamilies[j].Family
		})
		for owner, count := range owners[targetURL] {
			summary.Owners = append(summary.Owners, ReviewHandoffRetryOwnerCount{
				Owner: owner,
				Count: count,
			})
		}
		sort.SliceStable(summary.Owners, func(i, j int) bool {
			if summary.Owners[i].Count != summary.Owners[j].Count {
				return summary.Owners[i].Count > summary.Owners[j].Count
			}
			return summary.Owners[i].Owner < summary.Owners[j].Owner
		})
		for escalationLevel, count := range escalationLevels[targetURL] {
			summary.EscalationLevels = append(summary.EscalationLevels, ReviewHandoffRetryEscalationCount{
				EscalationLevel: escalationLevel,
				Count:           count,
			})
		}
		sort.SliceStable(summary.EscalationLevels, func(i, j int) bool {
			if summary.EscalationLevels[i].Count != summary.EscalationLevels[j].Count {
				return summary.EscalationLevels[i].Count > summary.EscalationLevels[j].Count
			}
			return summary.EscalationLevels[i].EscalationLevel < summary.EscalationLevels[j].EscalationLevel
		})
		out = append(out, *summary)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Ready != out[j].Ready {
			return out[i].Ready > out[j].Ready
		}
		if out[i].TotalAttempts != out[j].TotalAttempts {
			return out[i].TotalAttempts > out[j].TotalAttempts
		}
		return out[i].LastAttemptAt.After(out[j].LastAttemptAt)
	})
	return out, nil
}

func filterReviewHandoffRetryRecommendations(recommendations []ReviewHandoffRetryRecommendation, req ReviewHandoffRetryFatigueRequest) []ReviewHandoffRetryRecommendation {
	req = applyReviewHandoffRetryFatiguePreset(req)
	owner := strings.TrimSpace(req.Owner)
	escalationLevel := strings.TrimSpace(req.EscalationLevel)
	if owner == "" && escalationLevel == "" {
		return recommendations
	}
	out := make([]ReviewHandoffRetryRecommendation, 0, len(recommendations))
	for _, recommendation := range recommendations {
		recommendationOwner := reviewHandoffFatigueGroupValue(recommendation.Owner, "unassigned")
		if owner != "" && recommendationOwner != owner {
			continue
		}
		if escalationLevel != "" && recommendation.EscalationLevel != escalationLevel {
			continue
		}
		out = append(out, recommendation)
	}
	return out
}

func applyReviewHandoffRetryFatiguePreset(req ReviewHandoffRetryFatigueRequest) ReviewHandoffRetryFatigueRequest {
	presetName := strings.TrimSpace(req.Preset)
	if presetName == "" {
		return req
	}
	for _, preset := range ReviewHandoffRetryFatiguePresets() {
		if preset.Name != presetName {
			continue
		}
		if strings.TrimSpace(req.Owner) == "" {
			req.Owner = preset.Owner
		}
		if strings.TrimSpace(req.EscalationLevel) == "" {
			req.EscalationLevel = preset.EscalationLevel
		}
		return req
	}
	return req
}

func reviewHandoffFatigueGroupValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func reviewHandoffRetryBackoff(attempts int) time.Duration {
	if attempts <= 1 {
		return time.Minute
	}
	delay := time.Minute
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= time.Hour {
			return time.Hour
		}
	}
	return delay
}

func reviewHandoffStatusFamily(statusCode int) string {
	if statusCode <= 0 {
		return "network_error"
	}
	return fmt.Sprintf("%dxx", statusCode/100)
}

func formatRetryStatusFamilies(counts []ReviewHandoffRetryStatusFamilyCount) string {
	if len(counts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(counts))
	for _, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", count.Family, count.Count))
	}
	return strings.Join(parts, ", ")
}

func formatRetryOwners(counts []ReviewHandoffRetryOwnerCount) string {
	if len(counts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(counts))
	for _, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", count.Owner, count.Count))
	}
	return strings.Join(parts, ", ")
}

func formatRetryEscalations(counts []ReviewHandoffRetryEscalationCount) string {
	if len(counts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(counts))
	for _, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", count.EscalationLevel, count.Count))
	}
	return strings.Join(parts, ", ")
}

func formatRetryLatestFailure(summary ReviewHandoffRetryFatigueSummary) string {
	parts := []string{}
	if summary.LastStatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status %d", summary.LastStatusCode))
	}
	if strings.TrimSpace(summary.LastError) != "" {
		parts = append(parts, summary.LastError)
	}
	if !summary.LastAttemptAt.IsZero() {
		parts = append(parts, summary.LastAttemptAt.UTC().Format(time.RFC3339))
	}
	return strings.Join(parts, "; ")
}

func markdownInline(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "`", "'")
}

func markdownTable(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "`", "'")
}

// AcquisitionRetryCandidates returns latest unresolved failed connector attempts without executing retries.
func (h *NamespaceHandle) AcquisitionRetryCandidates(ctx context.Context, after time.Time) ([]AcquisitionRetryCandidate, error) {
	receipts, err := h.AcquisitionExecutionReceipts(ctx, after)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(receipts, func(i, j int) bool {
		return receipts[i].ExecutedAt.Before(receipts[j].ExecutedAt)
	})
	candidates := map[string]AcquisitionRetryCandidate{}
	for _, receipt := range receipts {
		key := receipt.TaskID + "\x00" + receipt.ConnectorID + "\x00" + receipt.PayloadSHA256
		candidate := candidates[key]
		candidate.Attempts++
		if receipt.Success {
			delete(candidates, key)
			continue
		}
		candidate.TaskID = receipt.TaskID
		candidate.ConnectorID = receipt.ConnectorID
		candidate.ConnectorType = receipt.ConnectorType
		candidate.TargetURL = receipt.TargetURL
		candidate.LastReceiptID = receipt.ReceiptID
		candidate.LastAttemptAt = receipt.ExecutedAt
		candidate.LastStatusCode = receipt.StatusCode
		candidate.PayloadSHA256 = receipt.PayloadSHA256
		candidate.IdempotencyKey = receipt.IdempotencyKey
		candidate.LastError = receipt.Error
		candidate.Retryable = receipt.Retryable
		candidates[key] = candidate
	}
	out := make([]AcquisitionRetryCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastAttemptAt.After(out[j].LastAttemptAt)
	})
	return out, nil
}

// AcquisitionRetryRecommendations returns read-only backoff guidance for unresolved acquisition failures.
func (h *NamespaceHandle) AcquisitionRetryRecommendations(ctx context.Context, after time.Time, now time.Time) ([]AcquisitionRetryRecommendation, error) {
	candidates, err := h.AcquisitionRetryCandidates(ctx, after)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := make([]AcquisitionRetryRecommendation, 0, len(candidates))
	for _, candidate := range candidates {
		delay := acquisitionRetryBackoff(candidate.Attempts)
		recommendedAfter := candidate.LastAttemptAt.Add(delay)
		ready := candidate.Retryable && !now.Before(recommendedAfter)
		reason := "terminal_failure"
		if candidate.Retryable {
			reason = "waiting_for_backoff"
			if ready {
				reason = "ready_for_operator_retry"
			}
		}
		out = append(out, AcquisitionRetryRecommendation{
			AcquisitionRetryCandidate: candidate,
			RecommendedAfter:          recommendedAfter,
			DelaySeconds:              int(delay.Seconds()),
			Ready:                     ready,
			Reason:                    reason,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Ready != out[j].Ready {
			return out[i].Ready
		}
		return out[i].RecommendedAfter.Before(out[j].RecommendedAfter)
	})
	return out, nil
}

func acquisitionStatusRetryable(statusCode int) bool {
	switch statusCode {
	case 408, 425, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func acquisitionRetryBackoff(attempts int) time.Duration {
	if attempts <= 1 {
		return time.Minute
	}
	delay := time.Minute
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= time.Hour {
			return time.Hour
		}
	}
	return delay
}
