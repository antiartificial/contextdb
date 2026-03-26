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

// RaptorConfig configures the RAPTOR compaction worker.
type RaptorConfig struct {
	Interval         time.Duration // polling interval (default: 5m)
	ClusterThreshold float64       // cosine sim threshold for clustering (default: 0.7)
	MinClusterSize   int           // minimum nodes to form a cluster (default: 3)
	MaxClusterSize   int           // maximum nodes per cluster (default: 50)
	SummaryMaxTokens int           // max tokens for summary (default: 256)
	Namespaces       []string      // which namespaces to compact (empty = all)
}

func (c RaptorConfig) withDefaults() RaptorConfig {
	if c.Interval == 0 {
		c.Interval = 5 * time.Minute
	}
	if c.ClusterThreshold == 0 {
		c.ClusterThreshold = 0.7
	}
	if c.MinClusterSize == 0 {
		c.MinClusterSize = 3
	}
	if c.MaxClusterSize == 0 {
		c.MaxClusterSize = 50
	}
	if c.SummaryMaxTokens == 0 {
		c.SummaryMaxTokens = 256
	}
	return c
}

// Worker is the RAPTOR compaction background worker.
type Worker struct {
	graph  store.GraphStore
	vecs   store.VectorIndex
	log    store.EventLog
	llm    extract.Provider
	config RaptorConfig
	logger *slog.Logger

	mu       sync.Mutex
	stop     chan struct{}
	stopped  chan struct{}
	running  bool
	lastRun  map[string]time.Time // per-namespace last compaction time
}

// NewWorker creates a RAPTOR compaction worker.
func NewWorker(graph store.GraphStore, vecs store.VectorIndex, log store.EventLog, llm extract.Provider, cfg RaptorConfig, logger *slog.Logger) *Worker {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		graph:   graph,
		vecs:    vecs,
		log:     log,
		llm:     llm,
		config:  cfg,
		logger:  logger,
		lastRun: make(map[string]time.Time),
	}
}

// Start begins the background compaction loop.
func (w *Worker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.stop = make(chan struct{})
	w.stopped = make(chan struct{})
	w.running = true
	w.mu.Unlock()

	go w.loop(ctx)
}

// Stop signals the worker to shut down and waits for completion.
func (w *Worker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	close(w.stop)
	w.mu.Unlock()
	<-w.stopped
}

func (w *Worker) loop(ctx context.Context) {
	defer close(w.stopped)

	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()

	// run once immediately
	w.runAll(ctx)

	for {
		select {
		case <-w.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runAll(ctx)
		}
	}
}

func (w *Worker) runAll(ctx context.Context) {
	for _, ns := range w.config.Namespaces {
		if err := w.processNamespace(ctx, ns); err != nil {
			w.logger.Error("compaction failed", "namespace", ns, "error", err)
		}
	}
}

func (w *Worker) processNamespace(ctx context.Context, ns string) error {
	lastRun := w.lastRun[ns]
	if lastRun.IsZero() {
		lastRun = time.Now().Add(-24 * time.Hour)
	}

	// Fetch unprocessed events
	events, err := w.log.Since(ctx, ns, lastRun)
	if err != nil {
		return fmt.Errorf("fetch events: %w", err)
	}
	if len(events) == 0 {
		return nil
	}

	// Collect node IDs from events
	var nodeIDs []uuid.UUID
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
		if payload.ID != uuid.Nil {
			nodeIDs = append(nodeIDs, payload.ID)
		}
	}

	if len(nodeIDs) == 0 {
		w.lastRun[ns] = time.Now()
		return nil
	}

	// Fetch full nodes
	var nodes []core.Node
	for _, id := range nodeIDs {
		n, err := w.graph.GetNode(ctx, ns, id)
		if err != nil || n == nil {
			continue
		}
		nodes = append(nodes, *n)
	}

	if len(nodes) < w.config.MinClusterSize {
		w.lastRun[ns] = time.Now()
		return nil
	}

	// Cluster similar nodes
	clusters := clusterNodes(nodes, w.config.ClusterThreshold, w.config.MinClusterSize, w.config.MaxClusterSize)

	w.logger.Info("compaction", "namespace", ns, "events", len(events), "nodes", len(nodes), "clusters", len(clusters))

	// Summarize each cluster
	for _, cluster := range clusters {
		if err := w.compactCluster(ctx, ns, cluster); err != nil {
			w.logger.Error("cluster compaction failed", "namespace", ns, "cluster_size", len(cluster), "error", err)
		}
	}

	// Mark events as processed
	for _, e := range events {
		if err := w.log.MarkProcessed(ctx, e.ID); err != nil {
			w.logger.Error("mark processed failed", "event_id", e.ID, "error", err)
		}
	}

	w.lastRun[ns] = time.Now()
	return nil
}

func (w *Worker) compactCluster(ctx context.Context, ns string, cluster []core.Node) error {
	// Build summary text from cluster members
	var texts []string
	for _, n := range cluster {
		if text, ok := n.Properties["text"].(string); ok && text != "" {
			texts = append(texts, text)
		}
	}

	if len(texts) == 0 {
		return nil
	}

	summary, err := w.summarize(ctx, texts)
	if err != nil {
		return fmt.Errorf("summarize: %w", err)
	}

	// Compute average vector
	vec := averageVector(cluster)

	// Create summary node
	summaryNode := core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Summary", "RAPTOR"},
		Properties: map[string]any{"text": summary, "cluster_size": len(cluster)},
		Vector:     vec,
		Confidence: avgConfidence(cluster),
		ValidFrom:  time.Now(),
		TxTime:     time.Now(),
	}

	if err := w.graph.UpsertNode(ctx, summaryNode); err != nil {
		return fmt.Errorf("upsert summary node: %w", err)
	}

	// Index vector if present
	if len(vec) > 0 {
		nID := summaryNode.ID
		if err := w.vecs.Index(ctx, core.VectorEntry{
			ID:        uuid.New(),
			Namespace: ns,
			NodeID:    &nID,
			Vector:    vec,
			Text:      summary,
			CreatedAt: time.Now(),
		}); err != nil {
			return fmt.Errorf("index summary vector: %w", err)
		}
		if reg, ok := w.vecs.(interface{ RegisterNode(core.Node) }); ok {
			reg.RegisterNode(summaryNode)
		}
	}

	// Create derived_from edges from summary to each child
	for _, child := range cluster {
		if err := w.graph.UpsertEdge(ctx, core.Edge{
			ID:        uuid.New(),
			Namespace: ns,
			Src:       summaryNode.ID,
			Dst:       child.ID,
			Type:      "derived_from",
			Weight:    1.0,
			ValidFrom: time.Now(),
			TxTime:    time.Now(),
		}); err != nil {
			return fmt.Errorf("upsert derived_from edge: %w", err)
		}
	}

	return nil
}

func (w *Worker) summarize(ctx context.Context, texts []string) (string, error) {
	if w.llm == nil {
		// fallback: concatenate first sentences
		return fallbackSummary(texts), nil
	}

	prompt := "Summarize the following related claims into a single concise paragraph:\n\n"
	for i, t := range texts {
		prompt += fmt.Sprintf("%d. %s\n", i+1, t)
	}

	resp, err := w.llm.Chat(ctx, extract.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []extract.ChatMessage{
			{Role: "system", Content: "You are a summarization system. Output only the summary, no preamble."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
		MaxTokens:   w.config.SummaryMaxTokens,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

func fallbackSummary(texts []string) string {
	if len(texts) == 0 {
		return ""
	}
	if len(texts) <= 3 {
		return strings.Join(texts, "; ")
	}
	return strings.Join(texts[:3], "; ") + fmt.Sprintf(" (and %d more)", len(texts)-3)
}

func avgConfidence(nodes []core.Node) float64 {
	if len(nodes) == 0 {
		return 0.5
	}
	sum := 0.0
	for _, n := range nodes {
		c := n.Confidence
		if c == 0 {
			c = 0.5
		}
		sum += c
	}
	return sum / float64(len(nodes))
}
