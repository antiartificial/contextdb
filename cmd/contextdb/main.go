// Command contextdb starts the ContextDB server in embedded mode.
// Observability endpoints are served on :7702 (separate from the data plane).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/observe"
	"github.com/antiartificial/contextdb/internal/retrieval"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(log)

	ctx := context.Background()

	// ── Observability ────────────────────────────────────────────────────
	reg := observe.Default
	metrics := observe.NewMetrics(reg)

	// Start metrics + pprof server on :7702
	go func() {
		addr := ":7702"
		slog.Info("observability server starting",
			"addr", addr,
			"endpoints", []string{"/metrics", "/debug/vars", "/debug/pprof/", "/health"})
		srv := &http.Server{
			Addr:         addr,
			Handler:      observe.Handler(reg),
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("observability server failed", "err", err)
		}
	}()

	// ── Storage ──────────────────────────────────────────────────────────
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	kv := memstore.NewKVStore()
	_ = memstore.NewEventLog()

	bsConfig := namespace.Defaults("channel:general", namespace.ModeBeliefSystem)
	metrics.ActiveNamespaces.Set(2)

	slog.Info("ContextDB starting",
		"mode", "embedded",
		"namespaces", []string{bsConfig.ID, "agent:primary"},
	)

	// ── Seed belief-system namespace ─────────────────────────────────────
	trustedSrc := core.DefaultSource(bsConfig.ID, "moderator:alice")
	trustedSrc.Labels = []string{"moderator"}
	_ = graph.UpsertSource(ctx, trustedSrc)
	metrics.GraphUpsertTotal.Inc()

	trollSrc := core.DefaultSource(bsConfig.ID, "user:troll99")
	trollSrc.Labels = []string{"troll"}
	_ = graph.UpsertSource(ctx, trollSrc)

	// Trusted claim
	trustedNode := core.Node{
		ID:         uuid.New(),
		Namespace:  bsConfig.ID,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "Go is garbage collected"},
		Confidence: trustedSrc.EffectiveCredibility(),
		ValidFrom:  time.Now(),
	}
	t0 := time.Now()
	_ = graph.UpsertNode(ctx, trustedNode)
	metrics.GraphUpsertLatency.ObserveDuration(time.Since(t0))
	metrics.GraphUpsertTotal.Inc()
	vecs.RegisterNode(trustedNode)
	t1 := time.Now()
	_ = vecs.Index(ctx, core.VectorEntry{
		ID: uuid.New(), Namespace: bsConfig.ID, NodeID: &trustedNode.ID,
		Vector:    []float32{0.9, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
		Text:      "Go is garbage collected",
		ModelID:   "synthetic", CreatedAt: time.Now(),
	})
	metrics.VectorIndexLatency.ObserveDuration(time.Since(t1))
	metrics.VectorIndexTotal.Inc()
	metrics.NodeCount.Add(1)

	// 5 troll claims
	for i := 0; i < 5; i++ {
		trollNode := core.Node{
			ID: uuid.New(), Namespace: bsConfig.ID,
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": fmt.Sprintf("Go has no GC (troll %d)", i+1)},
			Confidence: trollSrc.EffectiveCredibility(),
			ValidFrom:  time.Now(),
		}
		_ = graph.UpsertNode(ctx, trollNode)
		metrics.GraphUpsertTotal.Inc()
		metrics.AdmissionTrollRejected.Inc()
		vecs.RegisterNode(trollNode)
		nID := trollNode.ID
		_ = vecs.Index(ctx, core.VectorEntry{
			ID: uuid.New(), Namespace: bsConfig.ID, NodeID: &nID,
			Vector: []float32{
				0.85 + float32(i)*0.01,
				0.15, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1,
			},
			Text: fmt.Sprintf("Go has no GC (troll %d)", i+1),
			ModelID: "synthetic", CreatedAt: time.Now(),
		})
		metrics.VectorIndexTotal.Inc()
		metrics.NodeCount.Add(1)
	}

	// ── Retrieval demo ───────────────────────────────────────────────────
	baseEngine := &retrieval.Engine{Graph: graph, Vectors: vecs, KV: kv}
	engine := observe.NewInstrumentedEngine(baseEngine, metrics, log)

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

	report := observe.ReportScores(results, 0)
	observe.LogScoreReport(log, bsConfig.ID, report)

	fmt.Println()
	fmt.Printf("Observability endpoints running on :7702\n")
	fmt.Printf("  curl http://localhost:7702/metrics\n")
	fmt.Printf("  curl http://localhost:7702/debug/vars\n")
	fmt.Printf("  curl http://localhost:7702/health\n")
	fmt.Printf("  go tool pprof http://localhost:7702/debug/pprof/heap\n")
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop.")

	// Block so the observability server stays up
	select {}
}
