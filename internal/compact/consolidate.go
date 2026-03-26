package compact

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	lastRun map[string]time.Time
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
		graph:   graph,
		vecs:    vecs,
		log:     log,
		llm:     llm,
		config:  cfg,
		logger:  logger,
		lastRun: make(map[string]time.Time),
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
	lastRun := c.lastRun[ns]
	if lastRun.IsZero() {
		lastRun = time.Now().Add(-7 * 24 * time.Hour) // look back a week on first run
	}

	// Fetch events since last run
	events, err := c.log.Since(ctx, ns, lastRun)
	if err != nil {
		return fmt.Errorf("fetch events: %w", err)
	}
	if len(events) == 0 {
		c.lastRun[ns] = time.Now()
		return nil
	}

	// Collect episodic node IDs from events
	cutoff := time.Now().Add(-c.config.AgeThreshold)
	var candidates []core.Node

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

		candidates = append(candidates, *node)
	}

	if len(candidates) < c.config.FrequencyThreshold {
		c.lastRun[ns] = time.Now()
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

	// Write semantic nodes
	for _, fact := range facts {
		semanticNode := core.Node{
			ID:        uuid.New(),
			Namespace: ns,
			Labels:    []string{"semantic", "Consolidated"},
			Properties: map[string]any{
				"text":           fact.Text,
				"source_count":   fact.SourceCount,
				"consolidated_at": time.Now().Format(time.RFC3339),
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
			c.logger.Error("upsert semantic node failed", "error", err)
			continue
		}

		// Index vector if present
		if len(semanticNode.Vector) > 0 {
			nID := semanticNode.ID
			if err := c.vecs.Index(ctx, core.VectorEntry{
				ID:        uuid.New(),
				Namespace: ns,
				NodeID:    &nID,
				Vector:    semanticNode.Vector,
				Text:      fact.Text,
				CreatedAt: time.Now(),
			}); err != nil {
				c.logger.Error("index semantic vector failed", "error", err)
			}
			if reg, ok := c.vecs.(interface{ RegisterNode(core.Node) }); ok {
				reg.RegisterNode(semanticNode)
			}
		}

		// Create derived_from edges
		for _, srcID := range fact.SourceNodeIDs {
			if err := c.graph.UpsertEdge(ctx, core.Edge{
				ID:        uuid.New(),
				Namespace: ns,
				Src:       semanticNode.ID,
				Dst:       srcID,
				Type:      "derived_from",
				Weight:    1.0,
				ValidFrom: time.Now(),
				TxTime:    time.Now(),
			}); err != nil {
				c.logger.Error("upsert derived_from edge failed",
					"src", semanticNode.ID, "dst", srcID, "error", err)
			}
		}
	}

	// Mark originals as consolidated
	for _, candidate := range candidates {
		candidate.Properties["consolidated"] = true
		if err := c.graph.UpsertNode(ctx, candidate); err != nil {
			c.logger.Error("mark consolidated failed",
				"node_id", candidate.ID, "error", err)
		}
	}

	// Mark events as processed
	for _, e := range events {
		if err := c.log.MarkProcessed(ctx, e.ID); err != nil {
			c.logger.Error("mark processed failed", "event_id", e.ID, "error", err)
		}
	}

	c.lastRun[ns] = time.Now()
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

	prompt := "Given the following episodic memories, extract general semantic facts. " +
		"Return each fact on a separate line, prefixed with 'FACT: '. " +
		"Only include facts supported by multiple memories.\n\n"
	for i, t := range texts {
		prompt += fmt.Sprintf("%d. %s\n", i+1, t)
	}

	resp, err := c.llm.Chat(ctx, extract.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []extract.ChatMessage{
			{Role: "system", Content: "You extract general facts from episodic memories. Output only facts, each prefixed with 'FACT: '."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
		MaxTokens:   512,
	})
	if err != nil {
		return nil, err
	}

	// Parse facts from response
	allIDs := make([]uuid.UUID, len(candidates))
	for i, c := range candidates {
		allIDs[i] = c.ID
	}

	var facts []extractedFact
	for _, line := range strings.Split(resp.Content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "FACT: ") {
			continue
		}
		factText := strings.TrimPrefix(line, "FACT: ")
		if factText == "" {
			continue
		}
		facts = append(facts, extractedFact{
			Text:          factText,
			Confidence:    avgConfidence(candidates),
			SourceNodeIDs: allIDs,
			SourceCount:   len(candidates),
		})
	}

	return facts, nil
}

func (c *Consolidator) fallbackExtractFacts(candidates []core.Node) []extractedFact {
	var texts []string
	allIDs := make([]uuid.UUID, len(candidates))
	for i, n := range candidates {
		allIDs[i] = n.ID
		if text, ok := n.Properties["text"].(string); ok && text != "" {
			texts = append(texts, text)
		}
	}

	if len(texts) == 0 {
		return nil
	}

	// Simple fallback: combine all texts into one summary fact
	summary := strings.Join(texts, "; ")
	if len(summary) > 500 {
		summary = summary[:500]
	}

	return []extractedFact{{
		Text:          "Consolidated: " + summary,
		Confidence:    avgConfidence(candidates),
		SourceNodeIDs: allIDs,
		SourceCount:   len(candidates),
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
