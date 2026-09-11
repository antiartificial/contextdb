package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/ingest"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/google/uuid"
)

// WriteRequest describes a single write operation.
type WriteRequest struct {
	// NodeID optionally supplies a stable graph identity. It is useful when a
	// caller persists its own approval decision and may retry this write.
	NodeID uuid.UUID

	// IdempotencyKey identifies one logical write across retries. When set, it
	// also makes the recovery operation ID stable within this namespace.
	IdempotencyKey string

	// Content is the raw text of the claim, memory, or fact.
	Content string

	// SourceID is the external identifier of the asserting source.
	// Used to look up or create a Source record and apply its credibility.
	SourceID string

	// Labels are caller-defined node labels (e.g. "Claim", "Skill", "Episode").
	Labels []string

	// Properties are arbitrary key-value metadata stored on the node.
	Properties map[string]any

	// Vector is the pre-computed embedding. If nil, the node is stored
	// without a vector and will not appear in ANN search results.
	Vector []float32

	// ModelID identifies the embedding model that produced Vector.
	ModelID string

	// Confidence is the initial confidence score [0,1].
	// If 0, defaults to the source's effective credibility.
	Confidence float64

	// ValidFrom is when the fact became true in the world.
	// Defaults to time.Now() if zero.
	ValidFrom time.Time

	// MemType sets the memory type for decay rate selection.
	// Only meaningful for agent-memory namespaces.
	MemType core.MemoryType

	// DependsOn lists node IDs that must be written before this request.
	// Used by WriteBatchOrdered to determine write order. Each UUID should
	// match a "node_id" property on another request in the same batch.
	// Dependencies on IDs outside the batch are silently ignored.
	DependsOn []uuid.UUID

	// SkipDedup bypasses content fingerprint deduplication. Use this when
	// intentionally re-ingesting the same text as a distinct claim.
	SkipDedup bool

	// Dedup opts this write into content fingerprint deduplication even when
	// Options.DedupWrites is false.
	Dedup bool
}

// WriteResult describes the outcome of a write operation.
type WriteResult struct {
	// NodeID is the ID of the written node, or the existing node ID if
	// the write was rejected as a near-duplicate.
	NodeID uuid.UUID

	// Admitted indicates whether the node was written to the graph.
	// False means the admission gate rejected it (see Reason).
	Admitted bool

	// Reason explains a rejection.
	Reason string

	// ConflictIDs are IDs of nodes this write contradicts.
	ConflictIDs []uuid.UUID
}

// RecoverPendingWrites reconciles durable write intents that were interrupted
// between stores. It is safe to call after restarting a process. A returned
// error means at least one intent remains pending and must not be reported as
// a successful write.
func (h *NamespaceHandle) RecoverPendingWrites(ctx context.Context) error {
	if err := ingest.Recover(ctx, h.db.graph, h.db.vecs, h.db.log, h.cfg.ID); err != nil {
		return fmt.Errorf("recover pending writes: %w", err)
	}
	if err := ingest.RecoverFeedback(ctx, h.db.graph, h.db.log, h.cfg.ID); err != nil {
		return fmt.Errorf("recover pending feedback: %w", err)
	}
	return nil
}

// Write ingests a new claim, memory, or fact into the namespace.
// It runs through the admission gate (credibility floor, near-duplicate
// check, novelty threshold) before writing.
func (h *NamespaceHandle) Write(ctx context.Context, req WriteRequest) (WriteResult, error) {
	start := time.Now()
	h.db.metrics.IngestTotal.Inc()
	if key := strings.TrimSpace(req.IdempotencyKey); key != "" {
		unlock := h.lockIdempotency(key)
		defer unlock()
		release, err := h.acquireCoordinationLease(ctx, "write", key)
		if err != nil {
			return WriteResult{}, fmt.Errorf("write: %w", err)
		}
		defer release()
	}
	fingerprint := ""
	if (h.db.opts.DedupWrites || req.Dedup) && !req.SkipDedup && req.Content != "" {
		fingerprint = core.ContentFingerprint(req.Content)
		if fingerprint != "" {
			existing, err := h.db.graph.GetNodeByFingerprint(ctx, h.cfg.ID, fingerprint)
			if err != nil {
				return WriteResult{}, fmt.Errorf("write: fingerprint lookup: %w", err)
			}
			if existing != nil {
				if err := h.db.graph.TouchNode(ctx, h.cfg.ID, existing.ID, time.Now()); err != nil {
					return WriteResult{}, fmt.Errorf("write: touch deduplicated node: %w", err)
				}
				h.db.metrics.AdmissionDuplicateSkipped.Inc()
				h.db.metrics.IngestAdmitted.Inc()
				h.db.metrics.IngestLatency.ObserveDuration(time.Since(start))
				h.db.logger.Debug("write deduplicated",
					"namespace", h.cfg.ID,
					"node_id", existing.ID,
					"source", req.SourceID)
				return WriteResult{
					NodeID:   existing.ID,
					Admitted: true,
					Reason:   "deduplicated",
				}, nil
			}
		}
	}

	// Auto-embed if no vector provided and embedder is configured
	if len(req.Vector) == 0 && req.Content != "" && h.db.opts.Embedder != nil {
		vecs, err := h.db.opts.Embedder.Embed(ctx, []string{req.Content})
		if err != nil {
			return WriteResult{}, fmt.Errorf("write: auto-embed: %w", err)
		}
		if len(vecs) > 0 {
			req.Vector = vecs[0]
			if req.ModelID == "" {
				req.ModelID = h.db.opts.EmbedModel
			}
		}
	}

	// Resolve or create source
	src, err := h.resolveSource(ctx, req.SourceID)
	if err != nil {
		return WriteResult{}, fmt.Errorf("write: resolve source: %w", err)
	}

	// Set ValidFrom
	validFrom := req.ValidFrom
	if validFrom.IsZero() {
		validFrom = time.Now()
	}

	// Set confidence
	confidence := req.Confidence
	if confidence == 0 {
		confidence = src.EffectiveCredibility()
	}

	// Build candidate node
	props := req.Properties
	if props == nil {
		props = make(map[string]any)
	}
	if req.Content != "" {
		props["text"] = req.Content
	}
	if req.SourceID != "" {
		props["source_id"] = req.SourceID
	}

	nodeID := req.NodeID
	if nodeID == uuid.Nil {
		if req.IdempotencyKey != "" {
			nodeID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("contextdb/node/"+h.cfg.ID+"/"+req.IdempotencyKey))
		} else {
			nodeID = uuid.New()
		}
	}
	candidate := core.Node{
		ID:          nodeID,
		Namespace:   h.cfg.ID,
		Labels:      req.Labels,
		Properties:  props,
		Vector:      req.Vector,
		ModelID:     req.ModelID,
		Fingerprint: fingerprint,
		Confidence:  confidence,
		ValidFrom:   validFrom,
		TxTime:      time.Now(),
	}

	// Quick ANN scan for near-duplicate detection
	var nearest []core.ScoredNode
	if len(req.Vector) > 0 {
		nearest, err = h.db.vecs.Search(ctx, store.VectorQuery{
			Namespace: h.cfg.ID,
			Vector:    req.Vector,
			TopK:      5,
			AsOf:      time.Now(),
		})
		if err != nil {
			return WriteResult{}, fmt.Errorf("write: near-duplicate scan: %w", err)
		}
	}

	// Admission gate
	decision := ingest.Admit(ingest.AdmitRequest{
		Candidate:         candidate,
		Source:            src,
		NearestNeighbours: nearest,
		Threshold:         h.cfg.AdmitThreshold,
	})

	if !decision.Admit {
		h.db.metrics.IngestRejected.Inc()
		switch {
		case containsStr(decision.Reason, "credibility below floor"):
			h.db.metrics.AdmissionTrollRejected.Inc()
		case containsStr(decision.Reason, "near-duplicate"):
			h.db.metrics.AdmissionDuplicateSkipped.Inc()
		default:
			h.db.metrics.AdmissionThresholdFailed.Inc()
		}
		h.db.logger.Debug("write rejected",
			"namespace", h.cfg.ID,
			"source", req.SourceID,
			"reason", decision.Reason)
		return WriteResult{Admitted: false, Reason: decision.Reason}, nil
	}

	// Apply confidence multiplier
	candidate.Confidence = confidence * decision.ConfidenceMultiplier

	// Persist a durable intent before changing either store. The fixed vector
	// entry ID makes replay idempotent for indexes that replace by entry ID.
	var vectorEntry *core.VectorEntry
	if len(req.Vector) > 0 {
		nID := candidate.ID
		vectorEntry = &core.VectorEntry{
			ID:        uuid.New(),
			Namespace: h.cfg.ID,
			NodeID:    &nID,
			Vector:    req.Vector,
			Text:      req.Content,
			ModelID:   req.ModelID,
			CreatedAt: time.Now(),
		}
	}

	// The helper is recoverable rather than atomic. A returned error may mean
	// the graph has the node while the vector is still pending reconciliation.
	t0 := time.Now()
	operationID := uuid.New()
	if req.IdempotencyKey != "" {
		operationID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("contextdb/write/"+h.cfg.ID+"/"+req.IdempotencyKey))
	}
	plan := ingest.WritePlan{
		ID:          operationID,
		RequestHash: writeRequestHash(req, nodeID),
		Node:        &candidate,
		Vector:      vectorEntry,
	}
	var persistErr error
	if req.IdempotencyKey == "" {
		persistErr = ingest.PersistNew(ctx, h.db.graph, h.db.vecs, h.db.log, plan)
	} else {
		persistErr = ingest.Persist(ctx, h.db.graph, h.db.vecs, h.db.log, plan)
	}
	if persistErr != nil {
		return WriteResult{}, fmt.Errorf("write: persist recoverable write: %w", persistErr)
	}
	h.db.metrics.GraphUpsertLatency.ObserveDuration(time.Since(t0))
	h.db.metrics.GraphUpsertTotal.Inc()
	h.db.metrics.NodeCount.Add(1)

	if vectorEntry != nil {
		h.db.metrics.VectorIndexLatency.ObserveDuration(time.Since(t0))
		h.db.metrics.VectorIndexTotal.Inc()
	}

	h.db.metrics.IngestAdmitted.Inc()
	h.db.metrics.IngestLatency.ObserveDuration(time.Since(start))

	// Conflict detection (if we have nearest neighbours to check against)
	var conflictIDs []uuid.UUID
	if len(nearest) > 0 {
		detector := ingest.NewConflictDetector(h.db.graph, h.db.opts.LLMProvider)
		cResult, cErr := detector.Detect(ctx, candidate, nearest)
		if cErr != nil {
			h.db.logger.Warn("conflict detection failed", "error", cErr)
		} else {
			conflictIDs = cResult.ConflictIDs
		}
	}

	h.db.logger.Debug("write admitted",
		"namespace", h.cfg.ID,
		"node_id", candidate.ID,
		"source", req.SourceID,
		"confidence", candidate.Confidence,
		"conflicts", len(conflictIDs))

	return WriteResult{
		NodeID:      candidate.ID,
		Admitted:    true,
		ConflictIDs: conflictIDs,
	}, nil
}

// IngestText runs raw text through the extraction pipeline, producing nodes
// and edges automatically. Requires Options.Extractor to be set.
func (h *NamespaceHandle) IngestText(ctx context.Context, text, sourceID string) (*ingest.IngestResult, error) {
	if h.db.opts.Extractor == nil {
		return nil, fmt.Errorf("IngestText: no extractor configured")
	}

	pipeline := ingest.NewPipeline(
		h.db.opts.Extractor,
		h.db.graph,
		h.db.vecs,
		h.db.log,
		ingest.PipelineConfig{AdmitThreshold: h.cfg.AdmitThreshold},
	)

	return pipeline.Ingest(ctx, ingest.IngestRequest{
		Text:      text,
		Namespace: h.cfg.ID,
		SourceID:  sourceID,
	})
}

// WriteBatch writes multiple items in a single call. Returns results
// in the same order as requests. Partial failures are possible — if one
// write fails the remaining writes still execute. The first error
// encountered (if any) is returned alongside the partial results.
func (h *NamespaceHandle) WriteBatch(ctx context.Context, reqs []WriteRequest) ([]WriteResult, error) {
	results := make([]WriteResult, len(reqs))
	var firstErr error
	for i, req := range reqs {
		res, err := h.Write(ctx, req)
		if err != nil {
			results[i] = WriteResult{Reason: err.Error()}
			if firstErr == nil {
				firstErr = fmt.Errorf("WriteBatch[%d]: %w", i, err)
			}
			continue
		}
		results[i] = res
	}
	return results, firstErr
}

// WriteBatchOrdered writes nodes in dependency order. Requests are
// topologically sorted by DependsOn before writing. If there is a
// cycle, an error is returned. Requests without dependencies are
// written first. Results are indexed to match the original request
// slice, not the execution order.
func (h *NamespaceHandle) WriteBatchOrdered(ctx context.Context, reqs []WriteRequest) ([]WriteResult, error) {
	if len(reqs) <= 1 {
		return h.WriteBatch(ctx, reqs)
	}

	ordered, err := topoSort(reqs)
	if err != nil {
		return nil, err
	}

	results := make([]WriteResult, len(reqs))
	for _, idx := range ordered {
		res, err := h.Write(ctx, reqs[idx])
		results[idx] = res
		if err != nil {
			return results, fmt.Errorf("write %d: %w", idx, err)
		}
	}
	return results, nil
}
