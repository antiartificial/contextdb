// Command contextdb starts the ContextDB server in embedded mode.
// This is Phase 0 — all storage is in-process and no network listener
// is started yet. The binary is useful for smoke-testing the core
// scoring and retrieval logic.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/ataraxy-labs/contextdb/internal/core"
	"github.com/ataraxy-labs/contextdb/internal/namespace"
	"github.com/ataraxy-labs/contextdb/internal/retrieval"
	memstore "github.com/ataraxy-labs/contextdb/internal/store/memory"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(log)

	ctx := context.Background()

	// Initialise embedded stores.
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	kv := memstore.NewKVStore()
	_ = memstore.NewEventLog()

	// Create two namespaces with different modes.
	bsConfig := namespace.Defaults("channel:general", namespace.ModeBeliefSystem)
	_ = namespace.Defaults("agent:primary", namespace.ModeAgentMemory)

	slog.Info("ContextDB starting",
		"mode", "embedded",
		"namespaces", []string{bsConfig.ID, "agent:primary"},
	)

	// ── Demo: seed a belief-system namespace ────────────────────────────

	trustedSrc := core.DefaultSource(bsConfig.ID, "moderator:alice")
	trustedSrc.Labels = []string{"moderator"}
	if err := graph.UpsertSource(ctx, trustedSrc); err != nil {
		slog.Error("upsert source", "err", err)
		os.Exit(1)
	}

	trollSrc := core.DefaultSource(bsConfig.ID, "user:troll99")
	trollSrc.Labels = []string{"troll"}
	if err := graph.UpsertSource(ctx, trollSrc); err != nil {
		slog.Error("upsert source", "err", err)
		os.Exit(1)
	}

	// Trusted claim: "Go is garbage collected"
	trustedNode := core.Node{
		ID:         uuid.New(),
		Namespace:  bsConfig.ID,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "Go is garbage collected"},
		Confidence: trustedSrc.EffectiveCredibility(), // 1.0
		ValidFrom:  time.Now(),
	}
	if err := graph.UpsertNode(ctx, trustedNode); err != nil {
		slog.Error("upsert trusted node", "err", err)
		os.Exit(1)
	}
	vecs.RegisterNode(trustedNode)
	_ = vecs.Index(ctx, core.VectorEntry{
		ID:        uuid.New(),
		Namespace: bsConfig.ID,
		NodeID:    &trustedNode.ID,
		// Synthetic vector biased toward dim 0 = "GC is real"
		Vector:    []float32{0.9, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
		Text:      "Go is garbage collected",
		ModelID:   "synthetic",
		CreatedAt: time.Now(),
	})

	// Troll claim: "Go has no GC" — 5 repetitions with low confidence
	for i := 0; i < 5; i++ {
		trollNode := core.Node{
			ID:         uuid.New(),
			Namespace:  bsConfig.ID,
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": fmt.Sprintf("Go has no GC (troll %d)", i+1)},
			Confidence: trollSrc.EffectiveCredibility(), // 0.05
			ValidFrom:  time.Now(),
		}
		_ = graph.UpsertNode(ctx, trollNode)
		vecs.RegisterNode(trollNode)
		_ = vecs.Index(ctx, core.VectorEntry{
			ID:        uuid.New(),
			Namespace: bsConfig.ID,
			NodeID:    &trollNode.ID,
			// Synthetic vector slightly different but still similar to query
			Vector: []float32{
				0.85 + float32(i)*0.01,
				0.15, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1,
			},
			Text:      fmt.Sprintf("Go has no GC (troll %d)", i+1),
			ModelID:   "synthetic",
			CreatedAt: time.Now(),
		})
	}

	// ── Retrieve ─────────────────────────────────────────────────────────

	engine := &retrieval.Engine{Graph: graph, Vectors: vecs, KV: kv}

	params := bsConfig.ScoreParams
	params.AsOf = time.Now()

	q := retrieval.Query{
		Namespace:   bsConfig.ID,
		Vector:      []float32{0.88, 0.12, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
		TopK:        6,
		ScoreParams: params,
	}

	results, err := engine.Retrieve(ctx, q)
	if err != nil {
		slog.Error("retrieve", "err", err)
		os.Exit(1)
	}

	fmt.Println("\n┌─ Retrieval results (BeliefSystem namespace) ──────────────────────┐")
	fmt.Printf("│ %-40s %6s %6s %6s │\n", "text", "score", "sim", "conf")
	fmt.Println("├────────────────────────────────────────────────────────────────────┤")
	for _, r := range results {
		text, _ := r.Properties["text"].(string)
		if len(text) > 40 {
			text = text[:40]
		}
		fmt.Printf("│ %-40s %6.4f %6.4f %6.2f │\n",
			text, r.Score, r.SimilarityScore, r.Confidence)
	}
	fmt.Println("└────────────────────────────────────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("The trusted claim should appear at the top despite the 5 troll")
	fmt.Println("repetitions having higher vector similarity.")
}
