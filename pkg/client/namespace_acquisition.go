package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/google/uuid"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// AcquisitionPlanRequest configures knowledge acquisition planning.
type AcquisitionPlanRequest struct {
	TopK       int
	MinGapSize float64
	MaxGaps    int
	Budget     int
}

// AcquisitionTask is a suggested research, crawl, or verification task.
type AcquisitionTask struct {
	ID             string      `json:"id"`
	Type           string      `json:"type"`
	Priority       float64     `json:"priority"`
	Description    string      `json:"description"`
	Prompt         string      `json:"prompt"`
	RelatedNodeIDs []uuid.UUID `json:"related_node_ids,omitempty"`
	NearestTopics  []string    `json:"nearest_topics,omitempty"`
}

// AcquisitionPlan turns gaps and weak claims into concrete acquisition tasks.
type AcquisitionPlan struct {
	Namespace     string            `json:"namespace"`
	CoverageScore float64           `json:"coverage_score"`
	TotalNodes    int               `json:"total_nodes"`
	Tasks         []AcquisitionTask `json:"tasks"`
}

// AcquisitionConnector configures a source of acquisition results.
type AcquisitionConnector struct {
	ID               string            `json:"id"`
	Type             string            `json:"type"`
	Endpoint         string            `json:"endpoint,omitempty"`
	AllowedSourceIDs []string          `json:"allowed_source_ids,omitempty"`
	DefaultLabels    []string          `json:"default_labels,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
}

// AcquisitionExecutionRequest configures connector-specific acquisition execution.
type AcquisitionExecutionRequest struct {
	AcquisitionPlanRequest
	TaskIDs          []string
	Connectors       []AcquisitionConnector
	AllowedSourceIDs []string
	MaxResults       int
	MaxAttempts      int
	Execute          bool
	Now              time.Time
	Timeout          time.Duration
	HTTPClient       *http.Client
	// ReviewBeforeAdmission stores connector output as durable review candidates
	// instead of admitting it to the graph.
	ReviewBeforeAdmission bool
}

// AcquisitionPreviewItem is one item returned or previewed by a connector.
type AcquisitionPreviewItem struct {
	Title      string            `json:"title,omitempty"`
	URL        string            `json:"url,omitempty"`
	Snippet    string            `json:"snippet,omitempty"`
	Content    string            `json:"content,omitempty"`
	SourceID   string            `json:"source_id,omitempty"`
	Labels     []string          `json:"labels,omitempty"`
	Confidence float64           `json:"confidence,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// AcquisitionConnectorRun describes one planned or executed connector call.
type AcquisitionConnectorRun struct {
	TaskID             string                   `json:"task_id"`
	TaskType           string                   `json:"task_type"`
	ConnectorID        string                   `json:"connector_id"`
	ConnectorType      string                   `json:"connector_type"`
	Method             string                   `json:"method"`
	TargetURL          string                   `json:"target_url,omitempty"`
	Query              string                   `json:"query"`
	Prompt             string                   `json:"prompt"`
	AllowedSources     []string                 `json:"allowed_source_ids,omitempty"`
	DryRun             bool                     `json:"dry_run"`
	Executed           bool                     `json:"executed,omitempty"`
	Status             string                   `json:"status"`
	PayloadSHA256      string                   `json:"payload_sha256"`
	ResponseSHA256     string                   `json:"response_sha256,omitempty"`
	IdempotencyKey     string                   `json:"idempotency_key,omitempty"`
	Attempt            int                      `json:"attempt,omitempty"`
	MaxAttempts        int                      `json:"max_attempts,omitempty"`
	Retryable          bool                     `json:"retryable,omitempty"`
	NextRetryAfter     time.Duration            `json:"next_retry_after,omitempty"`
	StatusCode         int                      `json:"status_code,omitempty"`
	Headers            map[string]string        `json:"headers,omitempty"`
	PreviewItems       []AcquisitionPreviewItem `json:"preview_items,omitempty"`
	WrittenNodeIDs     []uuid.UUID              `json:"written_node_ids,omitempty"`
	ReviewCandidateIDs []uuid.UUID              `json:"review_candidate_ids,omitempty"`
	Error              string                   `json:"error,omitempty"`
}

// AcquisitionExecutionReceipt is an append-only audit record for one connector attempt.
type AcquisitionExecutionReceipt struct {
	ReceiptID        uuid.UUID     `json:"receipt_id,omitempty"`
	Namespace        string        `json:"namespace,omitempty"`
	TaskID           string        `json:"task_id"`
	TaskType         string        `json:"task_type,omitempty"`
	ConnectorID      string        `json:"connector_id"`
	ConnectorType    string        `json:"connector_type"`
	TargetURL        string        `json:"target_url,omitempty"`
	ExecutedAt       time.Time     `json:"executed_at"`
	Attempt          int           `json:"attempt"`
	MaxAttempts      int           `json:"max_attempts"`
	Success          bool          `json:"success"`
	Retryable        bool          `json:"retryable,omitempty"`
	NextRetryAfter   time.Duration `json:"next_retry_after,omitempty"`
	StatusCode       int           `json:"status_code,omitempty"`
	PayloadSHA256    string        `json:"payload_sha256"`
	ResponseSHA256   string        `json:"response_sha256,omitempty"`
	IdempotencyKey   string        `json:"idempotency_key,omitempty"`
	Error            string        `json:"error,omitempty"`
	WrittenNodeIDs   []uuid.UUID   `json:"written_node_ids,omitempty"`
	ReviewCandidates int           `json:"review_candidates,omitempty"`
	AllowedSourceIDs []string      `json:"allowed_source_ids,omitempty"`
}

// AcquisitionExecutionSummary summarizes a connector execution plan.
type AcquisitionExecutionSummary struct {
	Tasks            int `json:"tasks"`
	ConnectorRuns    int `json:"connector_runs"`
	PreviewItems     int `json:"preview_items"`
	WrittenNodes     int `json:"written_nodes"`
	ReviewCandidates int `json:"review_candidates"`
	Errors           int `json:"errors"`
}

// AcquisitionExecutionPlan is a dry-run or executed acquisition connector workflow.
type AcquisitionExecutionPlan struct {
	Namespace  string                      `json:"namespace"`
	DryRun     bool                        `json:"dry_run"`
	Executed   bool                        `json:"executed"`
	PlannedAt  time.Time                   `json:"planned_at"`
	Plan       *AcquisitionPlan            `json:"plan"`
	Connectors []AcquisitionConnector      `json:"connectors"`
	Runs       []AcquisitionConnectorRun   `json:"runs"`
	Summary    AcquisitionExecutionSummary `json:"summary"`
}

// AcquisitionPlan turns detected knowledge gaps and weak claims into acquisition tasks.
func (h *NamespaceHandle) AcquisitionPlan(ctx context.Context, req AcquisitionPlanRequest) (*AcquisitionPlan, error) {
	budget := req.Budget
	if budget <= 0 {
		budget = 10
	}
	gapReport, err := h.KnowledgeGaps(ctx, GapRequest{
		TopK:       req.TopK,
		MinGapSize: req.MinGapSize,
		MaxGaps:    req.MaxGaps,
	})
	if err != nil {
		return nil, err
	}
	plan := &AcquisitionPlan{
		Namespace:     h.cfg.ID,
		CoverageScore: gapReport.CoverageScore,
		TotalNodes:    gapReport.TotalNodes,
		Tasks:         make([]AcquisitionTask, 0, budget),
	}
	for _, gap := range gapReport.Gaps {
		plan.Tasks = append(plan.Tasks, AcquisitionTask{
			ID:            "gap:" + gap.ID.String(),
			Type:          "research_gap",
			Priority:      clampPriority(1.0 - gap.DensityScore + (1.0-gap.ConfidenceGap)*0.25),
			Description:   "Sparse knowledge region near: " + strings.Join(gap.NearestTopics, "; "),
			Prompt:        acquisitionPrompt("research_gap", gap.NearestTopics),
			NearestTopics: gap.NearestTopics,
		})
	}

	learner := retrieval.NewActiveLearner(h.db.graph)
	suggestions, err := learner.Suggest(ctx, h.cfg.ID, budget)
	if err != nil {
		return nil, err
	}
	for _, suggestion := range suggestions {
		plan.Tasks = append(plan.Tasks, AcquisitionTask{
			ID:             fmt.Sprintf("%s:%s", suggestion.Type, reviewIDsKey(suggestion.RelatedNodeIDs)),
			Type:           string(suggestion.Type),
			Priority:       clampPriority(suggestion.Priority),
			Description:    suggestion.Description,
			Prompt:         acquisitionPrompt(string(suggestion.Type), nil),
			RelatedNodeIDs: suggestion.RelatedNodeIDs,
		})
	}
	sort.SliceStable(plan.Tasks, func(i, j int) bool {
		return plan.Tasks[i].Priority > plan.Tasks[j].Priority
	})
	if len(plan.Tasks) > budget {
		plan.Tasks = plan.Tasks[:budget]
	}
	return plan, nil
}

// AcquisitionExecutionPreview returns connector-specific acquisition calls without mutating stored knowledge.
func (h *NamespaceHandle) AcquisitionExecutionPreview(ctx context.Context, req AcquisitionExecutionRequest) (*AcquisitionExecutionPlan, error) {
	req.Execute = false
	return h.acquisitionExecution(ctx, req, false)
}

// AcquisitionExecutionExecute executes configured acquisition connectors and writes returned items.
func (h *NamespaceHandle) AcquisitionExecutionExecute(ctx context.Context, req AcquisitionExecutionRequest) (*AcquisitionExecutionPlan, error) {
	if !req.Execute {
		return nil, fmt.Errorf("acquisition execution: execute must be true")
	}
	return h.acquisitionExecution(ctx, req, true)
}

func (h *NamespaceHandle) acquisitionExecution(ctx context.Context, req AcquisitionExecutionRequest, execute bool) (*AcquisitionExecutionPlan, error) {
	if len(req.Connectors) == 0 {
		return nil, fmt.Errorf("acquisition execution: at least one connector is required")
	}
	connectors, err := normalizeAcquisitionConnectors(req.Connectors)
	if err != nil {
		return nil, err
	}
	plan, err := h.AcquisitionPlan(ctx, req.AcquisitionPlanRequest)
	if err != nil {
		return nil, err
	}
	tasks := filterAcquisitionTasks(plan.Tasks, req.TaskIDs)
	if len(tasks) == 0 {
		return nil, fmt.Errorf("acquisition execution: no matching tasks")
	}
	plannedAt := req.Now
	if plannedAt.IsZero() {
		plannedAt = time.Now().UTC()
	}
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	out := &AcquisitionExecutionPlan{
		Namespace:  h.cfg.ID,
		DryRun:     !execute,
		Executed:   execute,
		PlannedAt:  plannedAt,
		Plan:       plan,
		Connectors: connectors,
		Runs:       make([]AcquisitionConnectorRun, 0, len(tasks)*len(connectors)),
	}
	out.Summary.Tasks = len(tasks)
	for _, task := range tasks {
		for _, connector := range connectors {
			run, err := h.buildAcquisitionConnectorRun(ctx, task, connector, req, maxResults, plannedAt, execute)
			if err != nil {
				if run.TaskID == "" {
					run = baseAcquisitionConnectorRun(task, connector, req.AllowedSourceIDs, maxResults, plannedAt, !execute)
				}
				run.Status = "error"
				run.Error = err.Error()
			}
			out.Summary.ConnectorRuns++
			out.Summary.PreviewItems += len(run.PreviewItems)
			out.Summary.WrittenNodes += len(run.WrittenNodeIDs)
			out.Summary.ReviewCandidates += len(run.ReviewCandidateIDs)
			if run.Error != "" {
				out.Summary.Errors++
			}
			out.Runs = append(out.Runs, run)
		}
	}
	return out, nil
}

func (h *NamespaceHandle) buildAcquisitionConnectorRun(ctx context.Context, task AcquisitionTask, connector AcquisitionConnector, req AcquisitionExecutionRequest, maxResults int, plannedAt time.Time, execute bool) (AcquisitionConnectorRun, error) {
	if len(req.AllowedSourceIDs) > 0 && len(connector.AllowedSourceIDs) > 0 && len(allowedAcquisitionSources(req.AllowedSourceIDs, connector.AllowedSourceIDs)) == 0 {
		return AcquisitionConnectorRun{}, fmt.Errorf("acquisition execution: connector %s has no allowed source intersection", connector.ID)
	}
	run := baseAcquisitionConnectorRun(task, connector, req.AllowedSourceIDs, maxResults, plannedAt, !execute)
	maxAttempts := req.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	run.MaxAttempts = maxAttempts
	if !execute {
		run.Status = "planned"
		return run, nil
	}
	if connector.Endpoint == "" {
		return run, fmt.Errorf("acquisition execution: connector %s endpoint is required for execute", connector.ID)
	}
	var result acquisitionConnectorResult
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		run.Attempt = attempt
		result, err = executeAcquisitionConnector(ctx, connector, run, req, maxResults)
		run.StatusCode = result.StatusCode
		run.ResponseSHA256 = result.ResponseSHA256
		run.Retryable = result.Retryable
		run.NextRetryAfter = acquisitionRetryBackoff(attempt)
		if err == nil {
			break
		}
		run.Error = err.Error()
		if receiptErr := h.recordAcquisitionExecutionReceipt(ctx, run, false); receiptErr != nil {
			return run, receiptErr
		}
		if !result.Retryable || attempt >= maxAttempts {
			return run, err
		}
	}
	items := result.Items
	items = filterAcquisitionPreviewItems(items, allowedAcquisitionSources(req.AllowedSourceIDs, connector.AllowedSourceIDs), connector.ID, maxResults)
	run.PreviewItems = items
	for _, item := range items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			content = strings.TrimSpace(item.Snippet)
		}
		if content == "" {
			content = strings.TrimSpace(item.Title)
		}
		if content == "" {
			continue
		}
		sourceID := strings.TrimSpace(item.SourceID)
		if sourceID == "" {
			sourceID = connector.ID
		}
		labels := append([]string{}, connector.DefaultLabels...)
		labels = append(labels, item.Labels...)
		if req.ReviewBeforeAdmission {
			candidate, candidateErr := h.recordAcquisitionReviewCandidate(ctx, task, connector, run, item, content, sourceID, dedupeStrings(labels), plannedAt)
			if candidateErr != nil {
				run.Error = candidateErr.Error()
				if receiptErr := h.recordAcquisitionExecutionReceipt(ctx, run, false); receiptErr != nil {
					return run, receiptErr
				}
				return run, candidateErr
			}
			run.ReviewCandidateIDs = append(run.ReviewCandidateIDs, candidate.CandidateID)
			continue
		}
		written, err := h.Write(ctx, WriteRequest{
			Content:    content,
			SourceID:   sourceID,
			Labels:     dedupeStrings(labels),
			Confidence: item.Confidence,
		})
		if err != nil {
			run.Error = err.Error()
			run.Retryable = false
			run.NextRetryAfter = 0
			if receiptErr := h.recordAcquisitionExecutionReceipt(ctx, run, false); receiptErr != nil {
				return run, receiptErr
			}
			return run, err
		}
		if written.Admitted {
			run.WrittenNodeIDs = append(run.WrittenNodeIDs, written.NodeID)
		}
	}
	run.Executed = true
	run.DryRun = false
	run.Status = "executed"
	run.Error = ""
	run.Retryable = false
	run.NextRetryAfter = 0
	if err := h.recordAcquisitionExecutionReceipt(ctx, run, true); err != nil {
		return run, err
	}
	return run, nil
}

func baseAcquisitionConnectorRun(task AcquisitionTask, connector AcquisitionConnector, requestAllowedSources []string, maxResults int, plannedAt time.Time, dryRun bool) AcquisitionConnectorRun {
	payload, _ := json.Marshal(map[string]any{
		"task_id":            task.ID,
		"task_type":          task.Type,
		"description":        task.Description,
		"prompt":             task.Prompt,
		"query":              acquisitionTaskQuery(task),
		"nearest_topics":     task.NearestTopics,
		"related_node_ids":   task.RelatedNodeIDs,
		"allowed_source_ids": allowedAcquisitionSources(requestAllowedSources, connector.AllowedSourceIDs),
		"max_results":        maxResults,
		"planned_at":         plannedAt.Format(time.RFC3339),
	})
	sum := sha256.Sum256(payload)
	headers := map[string]string{
		"Content-Type":                        "application/json",
		"X-ContextDB-Acquisition-Mode":        acquisitionMode(dryRun),
		"X-ContextDB-Acquisition-Task-ID":     task.ID,
		"X-ContextDB-Acquisition-Connector":   connector.ID,
		"X-ContextDB-Acquisition-Payload-SHA": hex.EncodeToString(sum[:]),
	}
	idempotencyKey := acquisitionIdempotencyKey(task.ID, connector.ID, hex.EncodeToString(sum[:]))
	headers["X-ContextDB-Acquisition-Idempotency-Key"] = idempotencyKey
	for key, value := range connector.Headers {
		headers[key] = value
	}
	return AcquisitionConnectorRun{
		TaskID:         task.ID,
		TaskType:       task.Type,
		ConnectorID:    connector.ID,
		ConnectorType:  connector.Type,
		Method:         "POST",
		TargetURL:      connector.Endpoint,
		Query:          acquisitionTaskQuery(task),
		Prompt:         task.Prompt,
		AllowedSources: allowedAcquisitionSources(requestAllowedSources, connector.AllowedSourceIDs),
		DryRun:         dryRun,
		Status:         "planned",
		PayloadSHA256:  hex.EncodeToString(sum[:]),
		IdempotencyKey: idempotencyKey,
		Attempt:        1,
		MaxAttempts:    1,
		Headers:        headers,
	}
}

type acquisitionConnectorResult struct {
	Items          []AcquisitionPreviewItem
	StatusCode     int
	ResponseSHA256 string
	Retryable      bool
}

func executeAcquisitionConnector(ctx context.Context, connector AcquisitionConnector, run AcquisitionConnectorRun, req AcquisitionExecutionRequest, maxResults int) (acquisitionConnectorResult, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	httpClient := req.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	payload, err := json.Marshal(map[string]any{
		"task_id":            run.TaskID,
		"task_type":          run.TaskType,
		"query":              run.Query,
		"prompt":             run.Prompt,
		"allowed_source_ids": run.AllowedSources,
		"max_results":        maxResults,
		"connector_id":       connector.ID,
		"connector_type":     connector.Type,
	})
	if err != nil {
		return acquisitionConnectorResult{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(callCtx, run.Method, connector.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return acquisitionConnectorResult{}, err
	}
	for key, value := range run.Headers {
		httpReq.Header.Set(key, value)
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return acquisitionConnectorResult{Retryable: true}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return acquisitionConnectorResult{StatusCode: resp.StatusCode, Retryable: true}, err
	}
	bodySum := sha256.Sum256(body)
	result := acquisitionConnectorResult{
		StatusCode:     resp.StatusCode,
		ResponseSHA256: hex.EncodeToString(bodySum[:]),
		Retryable:      acquisitionStatusRetryable(resp.StatusCode),
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("acquisition execution: connector %s returned status %d: %s", connector.ID, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	items, err := decodeAcquisitionPreviewItems(body)
	if err != nil {
		result.Retryable = false
		return result, err
	}
	result.Items = items
	return result, nil
}

func decodeAcquisitionPreviewItems(body []byte) ([]AcquisitionPreviewItem, error) {
	var wrapped struct {
		Items []AcquisitionPreviewItem `json:"items"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Items != nil {
		return wrapped.Items, nil
	}
	var items []AcquisitionPreviewItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("acquisition execution: decode connector response: %w", err)
	}
	return items, nil
}

func (h *NamespaceHandle) recordAcquisitionExecutionReceipt(ctx context.Context, run AcquisitionConnectorRun, success bool) error {
	receipt := AcquisitionExecutionReceipt{
		ReceiptID:        uuid.New(),
		Namespace:        h.cfg.ID,
		TaskID:           run.TaskID,
		TaskType:         run.TaskType,
		ConnectorID:      run.ConnectorID,
		ConnectorType:    run.ConnectorType,
		TargetURL:        run.TargetURL,
		ExecutedAt:       time.Now().UTC(),
		Attempt:          run.Attempt,
		MaxAttempts:      run.MaxAttempts,
		Success:          success,
		Retryable:        run.Retryable,
		NextRetryAfter:   run.NextRetryAfter,
		StatusCode:       run.StatusCode,
		PayloadSHA256:    run.PayloadSHA256,
		ResponseSHA256:   run.ResponseSHA256,
		IdempotencyKey:   run.IdempotencyKey,
		Error:            run.Error,
		WrittenNodeIDs:   append([]uuid.UUID{}, run.WrittenNodeIDs...),
		ReviewCandidates: len(run.ReviewCandidateIDs),
		AllowedSourceIDs: append([]string{}, run.AllowedSources...),
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("acquisition execution receipt: marshal: %w", err)
	}
	event := store.Event{
		ID:        receipt.ReceiptID,
		Namespace: h.cfg.ID,
		Type:      store.EventAcquisitionReceipt,
		Payload:   payload,
		TxTime:    receipt.ExecutedAt,
	}
	if err := h.db.log.Append(ctx, event); err != nil {
		return fmt.Errorf("acquisition execution receipt: append event: %w", err)
	}
	return nil
}

// AcquisitionExecutionReceipts returns connector execution receipt audit records after the given time.
func (h *NamespaceHandle) AcquisitionExecutionReceipts(ctx context.Context, after time.Time) ([]AcquisitionExecutionReceipt, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, after)
	if err != nil {
		return nil, fmt.Errorf("acquisition execution receipts: %w", err)
	}
	out := make([]AcquisitionExecutionReceipt, 0, len(events))
	for _, event := range events {
		if event.Type != store.EventAcquisitionReceipt {
			continue
		}
		var receipt AcquisitionExecutionReceipt
		if err := json.Unmarshal(event.Payload, &receipt); err != nil {
			return nil, fmt.Errorf("acquisition execution receipts: decode %s: %w", event.ID, err)
		}
		receipt.ReceiptID = event.ID
		if receipt.Namespace == "" {
			receipt.Namespace = event.Namespace
		}
		if receipt.ExecutedAt.IsZero() {
			receipt.ExecutedAt = event.TxTime
		}
		out = append(out, receipt)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ExecutedAt.After(out[j].ExecutedAt)
	})
	return out, nil
}

func normalizeAcquisitionConnectors(connectors []AcquisitionConnector) ([]AcquisitionConnector, error) {
	out := make([]AcquisitionConnector, 0, len(connectors))
	seen := map[string]bool{}
	for _, connector := range connectors {
		connector.ID = strings.TrimSpace(connector.ID)
		connector.Type = strings.TrimSpace(strings.ToLower(connector.Type))
		connector.Endpoint = strings.TrimSpace(connector.Endpoint)
		if connector.ID == "" {
			return nil, fmt.Errorf("acquisition execution: connector id is required")
		}
		if seen[connector.ID] {
			return nil, fmt.Errorf("acquisition execution: duplicate connector id %s", connector.ID)
		}
		switch connector.Type {
		case "search", "crawler":
		default:
			return nil, fmt.Errorf("acquisition execution: unsupported connector type %q", connector.Type)
		}
		seen[connector.ID] = true
		connector.AllowedSourceIDs = dedupeStrings(connector.AllowedSourceIDs)
		connector.DefaultLabels = dedupeStrings(connector.DefaultLabels)
		out = append(out, connector)
	}
	return out, nil
}

func filterAcquisitionTasks(tasks []AcquisitionTask, taskIDs []string) []AcquisitionTask {
	if len(taskIDs) == 0 {
		return tasks
	}
	allowed := map[string]bool{}
	for _, id := range taskIDs {
		allowed[strings.TrimSpace(id)] = true
	}
	out := make([]AcquisitionTask, 0, len(tasks))
	for _, task := range tasks {
		if allowed[task.ID] {
			out = append(out, task)
		}
	}
	return out
}

func filterAcquisitionPreviewItems(items []AcquisitionPreviewItem, allowedSources []string, defaultSourceID string, maxResults int) []AcquisitionPreviewItem {
	allowed := map[string]bool{}
	for _, source := range allowedSources {
		allowed[source] = true
	}
	out := make([]AcquisitionPreviewItem, 0, len(items))
	for _, item := range items {
		item.SourceID = strings.TrimSpace(item.SourceID)
		effectiveSourceID := item.SourceID
		if effectiveSourceID == "" {
			effectiveSourceID = defaultSourceID
		}
		if len(allowed) > 0 && !allowed[effectiveSourceID] {
			continue
		}
		item.Labels = dedupeStrings(item.Labels)
		out = append(out, item)
		if maxResults > 0 && len(out) >= maxResults {
			break
		}
	}
	return out
}

func allowedAcquisitionSources(requestSources, connectorSources []string) []string {
	if len(requestSources) == 0 {
		return dedupeStrings(connectorSources)
	}
	if len(connectorSources) == 0 {
		return dedupeStrings(requestSources)
	}
	allowed := map[string]bool{}
	for _, source := range requestSources {
		allowed[strings.TrimSpace(source)] = true
	}
	var out []string
	for _, source := range connectorSources {
		source = strings.TrimSpace(source)
		if allowed[source] {
			out = append(out, source)
		}
	}
	return dedupeStrings(out)
}

func acquisitionTaskQuery(task AcquisitionTask) string {
	if len(task.NearestTopics) > 0 {
		return strings.Join(task.NearestTopics, " ")
	}
	if task.Description != "" {
		return task.Description
	}
	return task.Prompt
}

func acquisitionMode(dryRun bool) string {
	if dryRun {
		return "dry-run"
	}
	return "execute"
}

func acquisitionIdempotencyKey(taskID, connectorID, payloadSHA string) string {
	sum := sha256.Sum256([]byte(taskID + "\x00" + connectorID + "\x00" + payloadSHA))
	return hex.EncodeToString(sum[:])
}

func acquisitionPrompt(taskType string, topics []string) string {
	switch taskType {
	case "research_gap":
		if len(topics) == 0 {
			return "Find high-credibility sources that fill this sparse knowledge region, then ingest concise claims with source IDs."
		}
		return "Research the gap around: " + strings.Join(topics, "; ") + ". Prefer primary or high-credibility sources and ingest concise supporting claims."
	case "verify_claim", "low_confidence":
		return "Find independent evidence that validates or refutes the related claim, then apply feedback or ingest counter-evidence."
	case "refresh_stale":
		return "Find current information for the related stale claim, then update or supersede it with fresh source-backed evidence."
	case "high_utility":
		return "Find stronger sources for this frequently used claim and ingest corroborating evidence."
	default:
		return "Acquire source-backed evidence and ingest it into this namespace."
	}
}
