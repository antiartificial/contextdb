package compact

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/extract"
	"github.com/antiartificial/contextdb/internal/store"
)

// ConsolidationConfig configures the memory consolidation worker.
type ConsolidationConfig struct {
	// AgeThreshold is the minimum age for episodic memories to be
	// considered for consolidation. Default: 24h.
	AgeThreshold time.Duration

	// FrequencyThreshold is the minimum number of times a concept must
	// appear across episodic memories to warrant a semantic fact.
	// Default: 3.
	FrequencyThreshold int

	// Interval is the polling interval for the consolidation loop.
	// Default: 30m.
	Interval time.Duration

	// Namespaces restricts consolidation to these namespaces.
	// Empty means no namespaces are processed (must be explicitly configured).
	Namespaces []string
}

func (c ConsolidationConfig) withDefaults() ConsolidationConfig {
	if c.AgeThreshold == 0 {
		c.AgeThreshold = 24 * time.Hour
	}
	if c.FrequencyThreshold == 0 {
		c.FrequencyThreshold = 3
	}
	if c.Interval == 0 {
		c.Interval = 30 * time.Minute
	}
	return c
}

// Consolidator is a background worker that converts episodic memories
// into semantic facts. It scans for old episodic memories, uses an LLM
// to extract generalized facts, writes semantic nodes with derived_from
// edges, and marks the originals as consolidated.
type Consolidator struct {
	graph  store.GraphStore
	vecs   store.VectorIndex
	log    store.EventLog
	llm    extract.Provider
	config ConsolidationConfig
	logger *slog.Logger

	mu      sync.Mutex
	stop    chan struct{}
	stopped chan struct{}
	running bool
}

// NewConsolidator creates a memory consolidation worker.
func NewConsolidator(
	graph store.GraphStore,
	vecs store.VectorIndex,
	log store.EventLog,
	llm extract.Provider,
	cfg ConsolidationConfig,
	logger *slog.Logger,
) *Consolidator {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Consolidator{
		graph:  graph,
		vecs:   vecs,
		log:    log,
		llm:    llm,
		config: cfg,
		logger: logger,
	}
}

// Start begins the background consolidation loop.
func (c *Consolidator) Start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.stop = make(chan struct{})
	c.stopped = make(chan struct{})
	c.running = true
	c.mu.Unlock()

	go c.loop(ctx)
}

// Stop signals the worker to shut down and waits for completion.
func (c *Consolidator) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	close(c.stop)
	c.mu.Unlock()
	<-c.stopped
}

func (c *Consolidator) loop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		close(c.stopped)
	}()

	ticker := time.NewTicker(c.config.Interval)
	defer ticker.Stop()

	// Run once immediately
	c.runAll(ctx)

	for {
		select {
		case <-c.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runAll(ctx)
		}
	}
}

func (c *Consolidator) runAll(ctx context.Context) {
	for _, ns := range c.config.Namespaces {
		if err := c.processNamespace(ctx, ns); err != nil {
			c.logger.Error("consolidation failed", "namespace", ns, "error", err)
		}
	}
}

func (c *Consolidator) processNamespace(ctx context.Context, ns string) error {
	// The event log's processed flag is the durable cursor. A timestamp cursor
	// loses young episodes that mature after a later run or process restart.
	events, err := c.log.Since(ctx, ns, time.Unix(0, 0))
	if err != nil {
		return fmt.Errorf("fetch events: %w", err)
	}
	if len(events) == 0 {
		return nil
	}

	// Collect episodic node IDs from events
	cutoff := time.Now().Add(-c.config.AgeThreshold)
	var candidates []core.Node
	eligibleEvents := make(map[uuid.UUID]uuid.UUID)
	seenCandidates := make(map[uuid.UUID]bool)

	for _, e := range events {
		if e.Type != store.EventNodeUpsert {
			continue
		}
		var payload struct {
			ID uuid.UUID `json:"id"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			continue
		}
		if payload.ID == uuid.Nil {
			continue
		}

		node, err := c.graph.GetNode(ctx, ns, payload.ID)
		if err != nil || node == nil {
			continue
		}

		// Only consider episodic memories
		if !node.HasLabel("episodic") && !isEpisodicMemory(node) {
			continue
		}

		// Must be older than the age threshold
		if node.ValidFrom.After(cutoff) {
			continue
		}

		// Skip already consolidated nodes
		if consolidated, ok := node.Properties["consolidated"].(bool); ok && consolidated {
			continue
		}

		eligibleEvents[e.ID] = node.ID
		if !seenCandidates[node.ID] {
			candidates = append(candidates, *node)
			seenCandidates[node.ID] = true
		}
	}

	if len(candidates) < c.config.FrequencyThreshold {
		return nil
	}

	c.logger.Info("consolidation",
		"namespace", ns,
		"candidates", len(candidates),
	)

	// Extract semantic facts from episodic memories via LLM
	facts, err := c.extractFacts(ctx, candidates)
	if err != nil {
		return fmt.Errorf("extract facts: %w", err)
	}
	if len(facts) == 0 {
		return nil
	}
	supported := make(map[uuid.UUID]bool)

	// Write semantic nodes
	for _, fact := range facts {
		if len(fact.SourceNodeIDs) < 2 {
			continue
		}
		sort.Slice(fact.SourceNodeIDs, func(i, j int) bool { return fact.SourceNodeIDs[i].String() < fact.SourceNodeIDs[j].String() })
		var supportKeys []string
		for _, id := range fact.SourceNodeIDs {
			supportKeys = append(supportKeys, id.String())
		}
		factID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("contextdb/consolidated/"+ns+"/"+fact.Text+"/"+strings.Join(supportKeys, ",")))
		for _, id := range fact.SourceNodeIDs {
			supported[id] = true
		}
		var supportVersions []map[string]any
		for _, srcID := range fact.SourceNodeIDs {
			for _, candidate := range candidates {
				if candidate.ID == srcID {
					supportVersions = append(supportVersions, map[string]any{"node_id": srcID.String(), "version": candidate.Version})
					break
				}
			}
		}
		semanticNode := core.Node{
			ID:        factID,
			Namespace: ns,
			Labels:    []string{"semantic", "Consolidated"},
			Properties: map[string]any{
				"text":             fact.Text,
				"source_count":     fact.SourceCount,
				"consolidated_at":  time.Now().Format(time.RFC3339),
				"support_versions": supportVersions,
			},
			Confidence: fact.Confidence,
			ValidFrom:  time.Now(),
			TxTime:     time.Now(),
		}

		// Compute average vector from source nodes
		var sourceNodes []core.Node
		for _, srcID := range fact.SourceNodeIDs {
			for _, c := range candidates {
				if c.ID == srcID {
					sourceNodes = append(sourceNodes, c)
				}
			}
		}
		if len(sourceNodes) > 0 {
			semanticNode.Vector = averageVector(sourceNodes)
		}

		if err := c.graph.UpsertNode(ctx, semanticNode); err != nil {
			return fmt.Errorf("upsert semantic node: %w", err)
		}

		// Index vector if present
		if len(semanticNode.Vector) > 0 {
			nID := semanticNode.ID
			if err := c.vecs.Index(ctx, core.VectorEntry{
				ID:        uuid.NewSHA1(uuid.NameSpaceURL, []byte("contextdb/consolidated-vector/"+factID.String())),
				Namespace: ns,
				NodeID:    &nID,
				Vector:    semanticNode.Vector,
				Text:      fact.Text,
				CreatedAt: time.Now(),
			}); err != nil {
				return fmt.Errorf("index semantic vector: %w", err)
			}
			if reg, ok := c.vecs.(interface{ RegisterNode(core.Node) }); ok {
				reg.RegisterNode(semanticNode)
			}
		}

		// Create derived_from edges
		for _, srcID := range fact.SourceNodeIDs {
			if err := c.graph.UpsertEdge(ctx, core.Edge{
				ID:        uuid.NewSHA1(uuid.NameSpaceURL, []byte("contextdb/consolidated-edge/"+factID.String()+"/"+srcID.String())),
				Namespace: ns,
				Src:       semanticNode.ID,
				Dst:       srcID,
				Type:      "derived_from",
				Weight:    1.0,
				ValidFrom: time.Now(),
				TxTime:    time.Now(),
			}); err != nil {
				return fmt.Errorf("upsert derived_from edge: %w", err)
			}
		}
	}

	// Mark originals as consolidated
	for _, candidate := range candidates {
		if !supported[candidate.ID] {
			continue
		}
		candidate.Properties["consolidated"] = true
		if err := c.graph.UpsertNode(ctx, candidate); err != nil {
			return fmt.Errorf("mark consolidated: %w", err)
		}
	}

	// Mark events as processed
	for _, e := range events {
		if !supported[eligibleEvents[e.ID]] {
			continue
		}
		if err := c.log.MarkProcessed(ctx, e.ID); err != nil {
			return fmt.Errorf("mark processed %s: %w", e.ID, err)
		}
	}

	return nil
}

// extractedFact represents a semantic fact extracted from episodic memories.
type extractedFact struct {
	Text          string
	Confidence    float64
	SourceNodeIDs []uuid.UUID
	SourceCount   int
}

func (c *Consolidator) extractFacts(ctx context.Context, candidates []core.Node) ([]extractedFact, error) {
	if c.llm == nil {
		return c.fallbackExtractFacts(candidates), nil
	}

	// Build the consolidation prompt
	var texts []string
	for _, n := range candidates {
		if text, ok := n.Properties["text"].(string); ok && text != "" {
			texts = append(texts, text)
		}
	}

	if len(texts) == 0 {
		return nil, nil
	}

	prompt := "Given the following numbered episodic memories, extract general semantic facts. " +
		"Return each fact as FACT: text | SOURCES: 1,2 using only numbered memories that directly support it. " +
		"Only include facts supported by at least two memories.\n\n"
	for i, t := range texts {
		prompt += fmt.Sprintf("%d. %s\n", i+1, t)
	}

	resp, err := c.llm.Chat(ctx, extract.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []extract.ChatMessage{
			{Role: "system", Content: "You extract facts with exact numbered support. Output only FACT: text | SOURCES: 1,2 lines."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
		MaxTokens:   512,
	})
	if err != nil {
		return nil, err
	}

	var facts []extractedFact
	for _, line := range strings.Split(resp.Content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "FACT: ") || !strings.Contains(line, " | SOURCES: ") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(line, "FACT: "), " | SOURCES: ", 2)
		factText := strings.TrimSpace(parts[0])
		if factText == "" {
			continue
		}
		var sources []core.Node
		seen := make(map[uuid.UUID]bool)
		for _, raw := range strings.Split(parts[1], ",") {
			index, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil || index < 1 || index > len(candidates) {
				continue
			}
			node := candidates[index-1]
			if !seen[node.ID] {
				sources = append(sources, node)
				seen[node.ID] = true
			}
		}
		if len(sources) < 2 {
			continue
		}
		ids := make([]uuid.UUID, len(sources))
		for i, source := range sources {
			ids[i] = source.ID
		}
		facts = append(facts, extractedFact{
			Text:          factText,
			Confidence:    avgConfidence(sources),
			SourceNodeIDs: ids,
			SourceCount:   len(sources),
		})
	}

	return facts, nil
}

func (c *Consolidator) fallbackExtractFacts(candidates []core.Node) []extractedFact {
	var texts []string
	var supporting []core.Node
	for _, n := range candidates {
		if text, ok := n.Properties["text"].(string); ok && text != "" {
			texts = append(texts, text)
			supporting = append(supporting, n)
		}
	}

	if len(texts) < 2 {
		return nil
	}
	allIDs := make([]uuid.UUID, len(supporting))
	for i, n := range supporting {
		allIDs[i] = n.ID
	}

	// Simple fallback: combine all texts into one summary fact
	summary := strings.Join(texts, "; ")
	if len(summary) > 500 {
		summary = summary[:500]
	}

	return []extractedFact{{
		Text:          "Consolidated: " + summary,
		Confidence:    avgConfidence(supporting),
		SourceNodeIDs: allIDs,
		SourceCount:   len(supporting),
	}}
}

func isEpisodicMemory(n *core.Node) bool {
	if n.HasLabel("Episode") || n.HasLabel("Episodic") || n.HasLabel("episodic") {
		return true
	}
	if mt, ok := n.Properties["memory_type"].(string); ok {
		return mt == string(core.MemoryEpisodic)
	}
	return false
}
