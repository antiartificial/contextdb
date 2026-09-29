package retrieval_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/store"
	badgerstore "github.com/antiartificial/contextdb/internal/store/badger"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

// This corpus is deliberately separate from the legacy representative corpus.
// Its stable IDs and clock make before/after feature comparisons meaningful.
const p0Namespace = "eval:p0"

var p0Anchor = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

type p0Case struct {
	id, category, text string
	vector             []float32
	asOf               time.Time
	want               uuid.UUID
	knownAt            time.Time
	maxRank            int // zero keeps the case observational until its feature lands
	forbidden          []uuid.UUID
}

func p0ID(s string) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceURL, []byte("contextdb/p0/"+s)) }

func p0Cases() []p0Case {
	return []p0Case{
		{id: "exact_identifier", category: "exact_term", text: "Find invoice INV-8Q7A", vector: []float32{1, 0}, want: p0ID("invoice")},
		{id: "valid_time_before_change", category: "temporal", text: "What was the deployment target before the migration?", vector: []float32{0, 1}, asOf: p0Anchor.Add(-24 * time.Hour), want: p0ID("target_old"), maxRank: 5, forbidden: []uuid.UUID{p0ID("target_new")}},
		{id: "valid_time_after_change", category: "temporal", text: "What is the deployment target after the migration?", vector: []float32{0, 1}, asOf: p0Anchor.Add(24 * time.Hour), want: p0ID("target_new"), maxRank: 1, forbidden: []uuid.UUID{p0ID("target_old")}},
		{id: "late_arrival_known_at", category: "temporal", text: "What did we know about the rollout before the correction arrived?", vector: []float32{.7, .7}, asOf: p0Anchor.Add(24 * time.Hour), knownAt: p0Anchor.Add(-12 * time.Hour), want: p0ID("late_old"), maxRank: 1, forbidden: []uuid.UUID{p0ID("late_correction")}},
		{id: "late_arrival_after_correction", category: "temporal", text: "What did we learn about the rollout after the correction arrived?", vector: []float32{.7, .7}, asOf: p0Anchor.Add(24 * time.Hour), knownAt: p0Anchor.Add(24 * time.Hour), want: p0ID("late_correction"), maxRank: 1},
		{id: "contradiction", category: "contradiction", text: "Does the service require TLS?", vector: []float32{1, 0}, want: p0ID("tls_trusted"), maxRank: 1},
		{id: "source_dispute", category: "source_dispute", text: "Which source states the correct failover region?", vector: []float32{0, 1}, want: p0ID("region_trusted"), maxRank: 1},
	}
}

type p0Backend struct {
	name     string
	graph    store.GraphStore
	vecs     store.VectorIndex
	register func(core.Node)
	close    func() error
}

func p0Backends(t testing.TB) []p0Backend {
	t.Helper()
	memGraph, memVecs := memstore.NewGraphStore(), memstore.NewVectorIndex()
	badgerDB, err := badgerstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	badgerGraph, badgerVecs := badgerstore.NewGraphStore(badgerDB.Inner()), badgerstore.NewVectorIndex(badgerDB.Inner(), badgerstore.HNSWConfig{})
	return []p0Backend{
		{name: "memory", graph: memGraph, vecs: memVecs, register: memVecs.RegisterNode},
		{name: "badger", graph: badgerGraph, vecs: badgerVecs, register: badgerVecs.RegisterNode, close: badgerDB.Close},
	}
}

func p0Populate(ctx context.Context, b p0Backend) error {
	type item struct {
		name, text, source string
		vector             []float32
		confidence         float64
		from               time.Time
		until              *time.Time
		tx                 time.Time
	}
	oldEnd := p0Anchor
	items := []item{
		{"invoice", "Invoice INV-8Q7A was approved", "ledger", []float32{0, 1}, .9, p0Anchor.Add(-72 * time.Hour), nil, p0Anchor.Add(-48 * time.Hour)},
		{"invoice_decoy", "An unrelated expense was approved", "ledger", []float32{1, 0}, .9, p0Anchor.Add(-72 * time.Hour), nil, p0Anchor.Add(-48 * time.Hour)},
		{"target_old", "Deployment target was east", "ops", []float32{0, 1}, .9, p0Anchor.Add(-72 * time.Hour), &oldEnd, p0Anchor.Add(-48 * time.Hour)},
		{"target_new", "Deployment target is west", "ops", []float32{0, 1}, .9, p0Anchor, nil, p0Anchor.Add(12 * time.Hour)},
		{"late_old", "Rollout completed without errors", "early report", []float32{.7, .7}, .8, p0Anchor.Add(-72 * time.Hour), nil, p0Anchor.Add(-48 * time.Hour)},
		{"late_correction", "Rollout had errors", "incident review", []float32{.7, .7}, .98, p0Anchor.Add(-72 * time.Hour), nil, p0Anchor.Add(12 * time.Hour)},
		{"tls_trusted", "Service requires TLS", "security", []float32{1, 0}, .95, p0Anchor.Add(-72 * time.Hour), nil, p0Anchor.Add(-48 * time.Hour)},
		{"tls_disputed", "Service never requires TLS", "anonymous", []float32{1, 0}, .05, p0Anchor.Add(-72 * time.Hour), nil, p0Anchor.Add(-48 * time.Hour)},
		{"region_trusted", "Failover region is west", "runbook", []float32{0, 1}, .95, p0Anchor.Add(-72 * time.Hour), nil, p0Anchor.Add(-48 * time.Hour)},
		{"region_disputed", "Failover region is north", "anonymous", []float32{0, 1}, .05, p0Anchor.Add(-72 * time.Hour), nil, p0Anchor.Add(-48 * time.Hour)},
	}
	for _, it := range items {
		n := core.Node{ID: p0ID(it.name), Namespace: p0Namespace, Labels: []string{"Claim"}, Properties: map[string]any{"text": it.text, "source_id": it.source}, Vector: it.vector, ModelID: "synthetic-2d", Confidence: it.confidence, ValidFrom: it.from, ValidUntil: it.until, TxTime: it.tx}
		if err := b.graph.UpsertNode(ctx, n); err != nil {
			return err
		}
		b.register(n)
		id := n.ID
		if err := b.vecs.Index(ctx, core.VectorEntry{ID: p0ID("vec_" + it.name), Namespace: p0Namespace, NodeID: &id, Vector: it.vector, Text: it.text, ModelID: "synthetic-2d", CreatedAt: it.tx}); err != nil {
			return err
		}
	}
	return nil
}

func p0Query(c p0Case) retrieval.Query {
	asOf := c.asOf
	if asOf.IsZero() {
		asOf = p0Anchor.Add(48 * time.Hour)
	}
	return retrieval.Query{Namespace: p0Namespace, Vector: c.vector, QueryText: c.text, TopK: 5, KnownAt: c.knownAt, Strategy: retrieval.HybridStrategy{VectorWeight: 1}, ScoreParams: core.ScoreParams{SimilarityWeight: .6, ConfidenceWeight: .3, RecencyWeight: .1, AsOf: asOf}}
}

func p0Rank(results []core.ScoredNode, want uuid.UUID) int {
	for i, result := range results {
		if result.Node.ID == want {
			return i + 1
		}
	}
	return 0
}

func p0Percentile(samples []time.Duration, percentile float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	index := int(float64(len(samples)-1) * percentile)
	return samples[index]
}

// The report is observational: new capability gaps stay visible as misses,
// while backend parity and fixture integrity remain test gates.
func TestP0RetrievalEvaluation(t *testing.T) {
	ctx := context.Background()
	cases := p0Cases()
	var parity map[string]int
	for _, b := range p0Backends(t) {
		if b.close != nil {
			defer b.close()
		}
		if err := p0Populate(ctx, b); err != nil {
			t.Fatal(err)
		}
		engine := retrieval.Engine{Graph: b.graph, Vectors: b.vecs}
		ranks := map[string]int{}
		var reciprocal float64
		var supported int
		var samples []time.Duration
		for _, c := range cases {
			start := time.Now()
			results, err := engine.Retrieve(ctx, p0Query(c))
			samples = append(samples, time.Since(start))
			if err != nil {
				t.Fatalf("%s/%s: %v", b.name, c.id, err)
			}
			rank := p0Rank(results, c.want)
			if c.maxRank > 0 && (rank == 0 || rank > c.maxRank) {
				t.Errorf("%s/%s: expected rank <= %d, got %d", b.name, c.id, c.maxRank, rank)
			}
			for _, forbidden := range c.forbidden {
				if got := p0Rank(results, forbidden); got != 0 {
					t.Errorf("%s/%s: forbidden node %s appeared at rank %d", b.name, c.id, forbidden, got)
				}
			}
			ranks[c.id] = rank
			supported++
			if rank > 0 {
				reciprocal += 1 / float64(rank)
			}
			t.Logf("backend=%s case=%s category=%s expected_rank=%d hit_at_5=%t", b.name, c.id, c.category, rank, rank > 0)
		}
		if parity == nil {
			parity = ranks
		} else {
			for id, rank := range parity {
				if ranks[id] != rank {
					t.Errorf("backend parity %s: memory rank=%d %s rank=%d", id, rank, b.name, ranks[id])
				}
			}
		}
		t.Logf("backend=%s cases=%d mrr_at_5=%.3f latency_p50=%s latency_p95=%s (single local run; not production latency)", b.name, supported, reciprocal/float64(supported), p0Percentile(samples, .5), p0Percentile(samples, .95))
	}
	if len(parity) != len(cases) {
		t.Fatal(fmt.Errorf("expected %d cases, got %d", len(cases), len(parity)))
	}
}

// BenchmarkP0Retrieval reports local end-to-end retrieval cost for the same
// labelled workload. Compare runs on the same host; it is not an SLO.
func BenchmarkP0Retrieval(b *testing.B) {
	ctx := context.Background()
	cases := p0Cases()
	for _, backend := range p0Backends(b) {
		if backend.close != nil {
			defer backend.close()
		}
		b.Run(backend.name, func(b *testing.B) {
			if err := p0Populate(ctx, backend); err != nil {
				b.Fatal(err)
			}
			engine := retrieval.Engine{Graph: backend.graph, Vectors: backend.vecs}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := engine.Retrieve(ctx, p0Query(cases[i%len(cases)])); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
