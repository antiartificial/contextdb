package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/antiartificial/contextdb/internal/store"
	"github.com/google/uuid"
)

type ReviewWorkerRequest struct {
	Execute        bool          `json:"execute"`
	Limit          int           `json:"limit"`
	AllowedActions []string      `json:"allowed_actions"`
	Evaluator      string        `json:"evaluator"`
	WebhookURL     string        `json:"-"`
	WebhookToken   string        `json:"-"`
	HTTPClient     *http.Client  `json:"-"`
	Timeout        time.Duration `json:"-"`
}
type ReviewWorkerDecision struct {
	ReviewID string `json:"review_id"`
	Action   string `json:"action"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}
type ReviewWorkerRun struct {
	Evaluator  string                 `json:"evaluator"`
	Status     string                 `json:"status"`
	Decisions  []ReviewWorkerDecision `json:"decisions,omitempty"`
	RunID      uuid.UUID              `json:"run_id"`
	Namespace  string                 `json:"namespace"`
	StartedAt  time.Time              `json:"started_at"`
	FinishedAt time.Time              `json:"finished_at"`
	DryRun     bool                   `json:"dry_run"`
	Examined   int                    `json:"examined"`
	Resolved   int                    `json:"resolved"`
	Skipped    int                    `json:"skipped"`
	Planned    []string               `json:"planned,omitempty"`
	Errors     []string               `json:"errors,omitempty"`
}

// RunReviewWorker is conservative: rules resolve only workflow items already
// marked stale or refuted, and only when resolve is explicitly allowed.
func (h *NamespaceHandle) RunReviewWorker(ctx context.Context, req ReviewWorkerRequest) (run ReviewWorkerRun, runErr error) {
	if h.db.opts.Mode == ModeRemote {
		return run, fmt.Errorf("review worker: remote clients cannot execute worker cycles; run it on the server")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return run, err
	}
	h.reviewMu.Lock()
	defer h.reviewMu.Unlock()
	if leases, ok := h.db.graph.(store.ReviewLeaseStore); ok {
		release, err := leases.AcquireReviewLease(ctx, h.cfg.ID)
		if err != nil {
			return run, fmt.Errorf("review worker: acquire namespace lease: %w", err)
		}
		defer func() {
			if err := release(); err != nil && runErr == nil {
				runErr = err
			}
		}()
	}
	if req.Evaluator == "" {
		req.Evaluator = "rules"
	}
	if req.Evaluator != "rules" && req.Evaluator != "webhook" {
		return ReviewWorkerRun{}, fmt.Errorf("review worker: unsupported evaluator %q", req.Evaluator)
	}
	allowed := map[string]bool{}
	for _, action := range req.AllowedActions {
		if action != "resolve" && action != "validate" && action != "refute" && action != "stale" {
			return ReviewWorkerRun{}, fmt.Errorf("review worker: unsupported action %q", action)
		}
		allowed[action] = true
	}
	if req.Limit <= 0 {
		req.Limit = 25
	}
	if req.Limit > 100 {
		req.Limit = 100
	}
	run = ReviewWorkerRun{Evaluator: req.Evaluator, Status: "running", RunID: uuid.New(), Namespace: h.cfg.ID, StartedAt: time.Now().UTC(), DryRun: !req.Execute}
	if req.Evaluator == "webhook" && strings.TrimSpace(req.WebhookURL) == "" {
		return run, fmt.Errorf("review worker: webhook evaluator is not configured")
	}
	if err := h.recordReviewWorkerRun(ctx, run); err != nil {
		return run, err
	}
	defer func() {
		run.FinishedAt = time.Now().UTC()
		run.Status = "completed"
		if runErr != nil || len(run.Errors) > 0 {
			run.Status = "needs_attention"
		}
		if runErr != nil {
			run.Errors = append(run.Errors, runErr.Error())
		}
		auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer auditCancel()
		if err := h.recordReviewWorkerRun(auditCtx, run); err != nil {
			runErr = fmt.Errorf("review worker: final audit failed: %w (cycle error: %v)", err, runErr)
		}
	}()
	items, err := h.ReviewQueue(ctx, ReviewQueueRequest{Limit: req.Limit, Status: "open"})
	if err != nil {
		return run, err
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return run, err
		}
		run.Examined++
		if item.Type == "acquisition_candidate" {
			run.Skipped++
			continue
		}
		action := ""
		if req.Evaluator == "rules" {
			if (item.Type == "stale" || item.Type == "refuted") && allowed["resolve"] {
				action = "resolve"
			}
		} else {
			action, err = evaluateReviewWebhook(ctx, req, item)
			if err != nil {
				run.Errors = append(run.Errors, err.Error())
				break
			}
			if !allowed[action] {
				run.Skipped++
				continue
			}
		}
		if action == "stale" && item.Type == "stale" || action == "refute" && item.Type == "refuted" {
			run.Skipped++
			continue
		}
		if action == "" {
			run.Skipped++
			continue
		}
		if action != "resolve" && item.NodeID == uuid.Nil {
			run.Skipped++
			continue
		}
		if !req.Execute {
			run.Planned = append(run.Planned, item.ID+":"+action)
			run.Decisions = append(run.Decisions, ReviewWorkerDecision{ReviewID: item.ID, Action: action, Status: "planned"})
			run.Skipped++
			continue
		}
		if _, err := h.RecordReviewDecision(ctx, ReviewDecisionRequest{ReviewID: item.ID, Status: "assigned", Owner: "review-worker", Decision: "worker_started", Note: "worker run " + run.RunID.String() + " action=" + action}); err != nil {
			return run, err
		}
		run.Decisions = append(run.Decisions, ReviewWorkerDecision{ReviewID: item.ID, Action: action, Status: "assigned"})
		if action != "resolve" {
			var mutationErr error
			switch action {
			case "validate":
				_, mutationErr = h.ValidateClaim(ctx, item.NodeID)
			case "refute":
				_, mutationErr = h.RefuteClaim(ctx, item.NodeID, "review worker webhook")
			case "stale":
				_, mutationErr = h.MarkStale(ctx, item.NodeID, "review worker webhook")
			}
			if mutationErr != nil {
				run.Errors = append(run.Errors, mutationErr.Error())
				run.Decisions[len(run.Decisions)-1].Status = "needs_attention"
				run.Decisions[len(run.Decisions)-1].Error = mutationErr.Error()
				break
			}
		}
		if _, err := h.RecordReviewDecision(ctx, ReviewDecisionRequest{ReviewID: item.ID, Status: "resolved", Decision: "worker_" + req.Evaluator + "_" + action, Note: "worker run " + run.RunID.String()}); err != nil {
			run.Errors = append(run.Errors, err.Error())
			break
		}
		run.Decisions[len(run.Decisions)-1].Status = "resolved"
		run.Resolved++
	}

	return run, nil
}

type webhookReviewDecision struct {
	Action     string  `json:"action"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

func evaluateReviewWebhook(ctx context.Context, req ReviewWorkerRequest, item ReviewItem) (string, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := json.Marshal(item)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, req.WebhookURL, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	if req.WebhookToken != "" {
		request.Header.Set("Authorization", "Bearer "+req.WebhookToken)
	}
	c := req.HTTPClient
	if c == nil {
		c = &http.Client{}
	}
	resp, err := c.Do(request)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("webhook evaluator: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil {
		return "", err
	}
	if len(data) > 64<<10 {
		return "", fmt.Errorf("webhook evaluator: response too large")
	}
	var decision webhookReviewDecision
	if err := json.Unmarshal(data, &decision); err != nil {
		return "", fmt.Errorf("webhook evaluator: malformed response: %w", err)
	}
	if decision.Confidence < 0 || decision.Confidence > 1 {
		return "", fmt.Errorf("webhook evaluator: confidence must be between 0 and 1")
	}
	if decision.Confidence < 0.8 {
		return "", nil
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return "", fmt.Errorf("webhook evaluator: reason is required")
	}
	switch decision.Action {
	case "validate", "refute", "stale", "resolve":
		return decision.Action, nil
	}
	return "", fmt.Errorf("webhook evaluator: unsupported action %q", decision.Action)
}

func (h *NamespaceHandle) recordReviewWorkerRun(ctx context.Context, run ReviewWorkerRun) error {
	payload, err := json.Marshal(run)
	if err != nil {
		return err
	}
	return h.db.log.Append(ctx, store.Event{ID: uuid.New(), Namespace: h.cfg.ID, Type: store.EventType("review_worker_run"), Payload: payload, TxTime: time.Now().UTC()})
}

// ReviewWorkerRuns returns the latest durable state of each worker cycle.
func (h *NamespaceHandle) ReviewWorkerRuns(ctx context.Context, after time.Time) ([]ReviewWorkerRun, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, after)
	if err != nil {
		return nil, err
	}
	out := []ReviewWorkerRun{}
	positions := map[uuid.UUID]int{}
	for _, event := range events {
		if event.Type != store.EventType("review_worker_run") {
			continue
		}
		var run ReviewWorkerRun
		if err := json.Unmarshal(event.Payload, &run); err != nil {
			return nil, err
		}
		if i, ok := positions[run.RunID]; ok {
			out[i] = run
		} else {
			positions[run.RunID] = len(out)
			out = append(out, run)
		}
	}
	return out, nil
}
