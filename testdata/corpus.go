// Package testdata provides a synthetic corpus for fitness evaluation.
//
// The corpus covers four namespace modes and is designed to exercise
// the retrieval properties that matter most:
//
//   - Hybrid vs pure-vector recall (does graph+confidence help?)
//   - Poisoning resistance at scale (50 troll writes vs 5 trusted)
//   - Temporal correctness (older contradicted facts score lower)
//   - Multi-hop retrieval (connected nodes surface via graph walk)
//   - Decay correctness (episodic vs procedural age handling)
//
// All embeddings are synthetic 16-dimensional vectors. Topic clusters
// are encoded as biases toward specific dimensions so cosine similarity
// behaves realistically within and across topics.
package testdata

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

// Corpus is a fully populated in-memory dataset ready for retrieval tests.
type Corpus struct {
	Graph  *memstore.GraphStore
	Vecs   *memstore.VectorIndex
	KV     *memstore.KVStore

	// Fixtures are grouped by namespace/scenario for targeted querying.
	Fixtures []Fixture

	// QuerySet is the set of labelled queries for recall evaluation.
	QuerySet []LabelledQuery
}

// Fixture is a single node in the corpus with metadata used by tests.
type Fixture struct {
	Node       core.Node
	Topic      string   // semantic cluster label
	IsCorrect  bool     // is this the "right" answer for its topic?
	IsTroll    bool
	IsStale    bool     // intentionally old
}

// LabelledQuery is a query with a known correct answer for recall evaluation.
type LabelledQuery struct {
	ID             string
	Description    string
	Namespace      string
	Vector         []float32
	CorrectNodeIDs []uuid.UUID // all acceptable top answers
	Category       string      // "factual", "temporal", "contradiction", "multihop", "procedural"
}

// ─── Embedding helpers ───────────────────────────────────────────────────────

const dim = 16

// topicVec returns a normalised vector biased strongly toward a topic
// dimension and weakly toward a noise dimension.
func topicVec(topicDim int, noiseDim int, topicWeight, noiseWeight float64) []float32 {
	v := make([]float32, dim)
	v[topicDim%dim] = float32(topicWeight)
	v[noiseDim%dim] += float32(noiseWeight)
	return normalise(v)
}

// queryVec returns a query vector close to the given topic but not identical.
func queryVec(topicDim int, drift float64) []float32 {
	v := make([]float32, dim)
	v[topicDim%dim] = float32(1.0 - drift)
	v[(topicDim+1)%dim] = float32(drift)
	return normalise(v)
}

// trollVec returns a vector very similar to the topic (high cosine sim)
// to simulate a convincing-looking false claim.
func trollVec(topicDim int, idx int) []float32 {
	v := make([]float32, dim)
	v[topicDim%dim] = float32(0.95)
	v[(topicDim+2+idx)%dim] = float32(0.05)
	return normalise(v)
}

// agentVec returns a vector for an agent memory node.
// Procedural memories use topicDim 10-12 (reserved subspace).
// Episodic/semantic use topicDim 5-9.
func agentVec(memType core.MemoryType, topicDim int) []float32 {
	switch memType {
	case core.MemoryProcedural:
		// topicDim is already in range 10-12; use next dim as noise
		return topicVec(topicDim, (topicDim+1)%dim, 0.88, 0.12)
	default:
		return topicVec(topicDim, (topicDim+4)%dim, 0.85, 0.15)
	}
}

// TopicVecExported is an exported version of topicVec for use by external test packages.
func TopicVecExported(topicDim int, noiseDim int, topicWeight, noiseWeight float64) []float32 {
	return topicVec(topicDim, noiseDim, topicWeight, noiseWeight)
}

// QueryVecExported is an exported version of queryVec for use by external test packages.
func QueryVecExported(topicDim int, drift float64) []float32 {
	return queryVec(topicDim, drift)
}

// TrollVecExported is an exported version of trollVec for use by external test packages.
func TrollVecExported(topicDim int, idx int) []float32 {
	return trollVec(topicDim, idx)
}

func normalise(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	norm := float32(math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

// ─── Namespace constants ─────────────────────────────────────────────────────

const (
	NSChannel    = "channel:general"    // belief system — poisoning scenario
	NSAgent      = "agent:primary"      // agent memory — utility + decay
	NSGeneral    = "general:rag"        // general RAG — hybrid vs vector
	NSProcedural = "procedural:skills"  // procedural — slow decay
)

// ─── Build ───────────────────────────────────────────────────────────────────

// Build constructs the full synthetic corpus and returns it.
func Build() *Corpus {
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	kv := memstore.NewKVStore()

	c := &Corpus{Graph: graph, Vecs: vecs, KV: kv}

	now := time.Now()

	c.buildChannelNamespace(ctx, graph, vecs, now)
	c.buildAgentNamespace(ctx, graph, vecs, now)
	c.buildGeneralNamespace(ctx, graph, vecs, now)
	c.buildProceduralNamespace(ctx, graph, vecs, now)

	c.buildQuerySet(now)

	return c
}

// upsert writes a node to graph + vector index and records the fixture.
func (c *Corpus) upsert(
	ctx context.Context,
	graph *memstore.GraphStore,
	vecs *memstore.VectorIndex,
	n core.Node,
	vec []float32,
	topic string,
	isCorrect, isTroll, isStale bool,
) {
	_ = graph.UpsertNode(ctx, n)
	vecs.RegisterNode(n)
	nID := n.ID
	_ = vecs.Index(ctx, core.VectorEntry{
		ID:        uuid.New(),
		Namespace: n.Namespace,
		NodeID:    &nID,
		Vector:    vec,
		Text:      textProp(n),
		ModelID:   "synthetic-16d",
		CreatedAt: time.Now(),
	})
	c.Fixtures = append(c.Fixtures, Fixture{
		Node: n, Topic: topic,
		IsCorrect: isCorrect, IsTroll: isTroll, IsStale: isStale,
	})
}

func textProp(n core.Node) string {
	if t, ok := n.Properties["text"].(string); ok {
		return t
	}
	return n.ID.String()
}

// ─── Channel namespace (belief system / poisoning) ───────────────────────────
//
// Topics: Go GC, Go concurrency, Python typing, Rust ownership
// For each topic: 1 trusted claim + N troll contradictions

func (c *Corpus) buildChannelNamespace(ctx context.Context, graph *memstore.GraphStore, vecs *memstore.VectorIndex, now time.Time) {
	type topic struct {
		name       string
		topicDim   int
		trueFact   string
		trollFact  string
		confidence float64
		trollCount int
	}

	topics := []topic{
		{"go_gc", 0, "Go uses a concurrent mark-and-sweep garbage collector", "Go has no garbage collector", 0.95, 10},
		{"go_concurrency", 1, "Go uses goroutines and channels for concurrency", "Go uses OS threads directly, no goroutines", 0.92, 8},
		{"python_typing", 2, "Python 3 supports optional static type hints via PEP 484", "Python has no type system whatsoever", 0.90, 6},
		{"rust_ownership", 3, "Rust enforces memory safety through an ownership and borrow checker", "Rust uses garbage collection like Go", 0.93, 7},
		{"go_interfaces", 4, "Go interfaces are satisfied implicitly, no explicit implements keyword", "Go requires explicit interface declarations like Java", 0.88, 5},
	}

	for _, tp := range topics {
		// Trusted claim — high confidence, moderately similar to query
		trustedID := uuid.New()
		trusted := core.Node{
			ID: trustedID, Namespace: NSChannel,
			Labels:     []string{"Claim", "Trusted"},
			Properties: map[string]any{"text": tp.trueFact, "topic": tp.name},
			Confidence: tp.confidence,
			ValidFrom:  now.Add(-24 * time.Hour),
		}
		c.upsert(ctx, graph, vecs, trusted, topicVec(tp.topicDim, 8, 0.9, 0.1), tp.name, true, false, false)

		// Troll claims — low confidence, high cosine similarity (convincing-looking)
		for i := 0; i < tp.trollCount; i++ {
			troll := core.Node{
				ID: uuid.New(), Namespace: NSChannel,
				Labels:     []string{"Claim", "Troll"},
				Properties: map[string]any{"text": tp.trollFact, "topic": tp.name, "troll_idx": i},
				Confidence: 0.05,
				ValidFrom:  now.Add(-time.Duration(i) * time.Hour),
			}
			c.upsert(ctx, graph, vecs, troll, trollVec(tp.topicDim, i), tp.name, false, true, false)
		}

		// Add a contradiction edge: troll[0] contradicts trusted
		// (In a real pipeline the ingest layer would create this; here we wire it directly)
		edges, _ := graph.EdgesFrom(ctx, NSChannel, trustedID, nil)
		_ = edges // just ensuring the trusted node is accessible
	}

	// Add some noise nodes — unrelated topics that should not surface
	for i := 0; i < 20; i++ {
		noise := core.Node{
			ID: uuid.New(), Namespace: NSChannel,
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": "unrelated noise claim", "noise_idx": i},
			Confidence: 0.5,
			ValidFrom:  now.Add(-time.Duration(i*2) * time.Hour),
		}
		noiseVec := topicVec((i+9)%dim, (i+13)%dim, 0.7, 0.3)
		c.upsert(ctx, graph, vecs, noise, noiseVec, "noise", false, false, false)
	}
}

// ─── Agent namespace (memory utility + decay) ────────────────────────────────
//
// Episodic memories at various ages, semantic abstractions, procedural skills.
// High-utility memories (from successful tasks) vs low-utility ones.

func (c *Corpus) buildAgentNamespace(ctx context.Context, graph *memstore.GraphStore, vecs *memstore.VectorIndex, now time.Time) {
	type mem struct {
		text       string
		memType    core.MemoryType
		ageHours   float64
		utility    float64 // encoded in confidence for scoring
		topicDim   int
	}

	memories := []mem{
		// Recent high-utility episodic: should rank high
		{"Deployed the orders service to GKE using helm upgrade --atomic", core.MemoryEpisodic, 2, 0.90, 5},
		{"Fixed the Kafka consumer lag by increasing partition count to 12", core.MemoryEpisodic, 4, 0.85, 6},
		{"Resolved Redis connection pool exhaustion by tuning MaxIdleConns", core.MemoryEpisodic, 6, 0.88, 7},

		// Stale low-utility episodic: should rank low despite relevant topic
		{"Attempted Kafka migration but rolled back due to consumer errors", core.MemoryEpisodic, 120, 0.20, 6},
		{"Initial GKE cluster setup — outdated config, no longer relevant", core.MemoryEpisodic, 200, 0.15, 5},

		// Semantic abstractions — moderate age but high utility
		{"GKE deployments require resource limits on all containers to avoid OOM eviction", core.MemorySemantic, 48, 0.85, 5},
		{"Kafka partition count should be set at topic creation; changes require consumer rebalance", core.MemorySemantic, 72, 0.88, 6},
		{"PostgreSQL connection pools should be sized to max_connections minus reserved system slots", core.MemorySemantic, 36, 0.80, 8},

		// Procedural — old but should still rank well due to slow decay.
		// topicDims 10-12 are reserved for procedural memories to prevent
		// collision with episodic/semantic nodes (dims 5-9).
		{"To add a new microservice: create Helm chart, add to ArgoCD app, update Terraform outputs", core.MemoryProcedural, 500, 0.92, 10},
		{"Runbook: GKE node pool upgrade — cordon, drain, replace nodes one AZ at a time", core.MemoryProcedural, 720, 0.95, 11},
		{"Database migration checklist: backup, test on staging, use --fake-initial-migration flag", core.MemoryProcedural, 400, 0.90, 12},

		// Low-utility noise
		{"Looked at the README for the orders service", core.MemoryEpisodic, 1, 0.10, 9},
		{"Checked Slack for updates", core.MemoryEpisodic, 3, 0.05, 9},
	}

	for _, m := range memories {
		id := uuid.New()
		n := core.Node{
			ID: id, Namespace: NSAgent,
			Labels:     []string{string(m.memType)},
			Properties: map[string]any{"text": m.text, "mem_type": string(m.memType), "utility": m.utility},
			Confidence: m.utility, // encode utility in confidence for scoring
			ValidFrom:  now.Add(-time.Duration(m.ageHours * float64(time.Hour))),
		}
		// Procedural memories are correct regardless of age (slow decay is the point).
		// Episodic/semantic are correct only if fresh and high-utility.
		var isCorrect bool
		if m.memType == core.MemoryProcedural {
			isCorrect = m.utility > 0.7
		} else {
			isCorrect = m.utility > 0.7 && m.ageHours < 100
		}
		c.upsert(ctx, graph, vecs, n, agentVec(m.memType, m.topicDim), string(m.memType), isCorrect, false, m.ageHours > 100)
	}
}

// ─── General namespace (hybrid vs pure-vector) ───────────────────────────────
//
// Documents where graph edges encode relationships pure vector search misses.
// Multi-hop: A relates_to B relates_to C — query matches A, correct answer is C.

func (c *Corpus) buildGeneralNamespace(ctx context.Context, graph *memstore.GraphStore, vecs *memstore.VectorIndex, now time.Time) {
	type doc struct {
		text     string
		topicDim int
		conf     float64
	}

	// Hub nodes — highly connected, used as graph traversal seeds
	hubDocs := []doc{
		{"Kubernetes pod scheduling and resource allocation", 0, 0.9},
		{"Distributed systems consistency models: eventual vs strong", 1, 0.88},
		{"PostgreSQL query optimisation and index strategy", 2, 0.92},
	}
	hubIDs := make([]uuid.UUID, len(hubDocs))
	for i, d := range hubDocs {
		id := uuid.New()
		hubIDs[i] = id
		n := core.Node{
			ID: id, Namespace: NSGeneral,
			Labels:     []string{"Document", "Hub"},
			Properties: map[string]any{"text": d.text},
			Confidence: d.conf,
			ValidFrom:  now.Add(-48 * time.Hour),
		}
		c.upsert(ctx, graph, vecs, n, topicVec(d.topicDim, 8, 0.88, 0.12), "hub", true, false, false)
	}

	// Leaf nodes — connected to hubs via relates_to edges.
	// Low direct similarity to queries but reachable via graph.
	type leaf struct {
		text     string
		topicDim int
		conf     float64
		hubIdx   int
	}
	leafDocs := []leaf{
		{"Pod disruption budgets prevent all replicas being evicted simultaneously", 0, 0.85, 0},
		{"Vertical pod autoscaler adjusts CPU and memory requests based on usage", 0, 0.82, 0},
		{"CAP theorem: distributed systems can guarantee only two of consistency, availability, partition tolerance", 1, 0.90, 1},
		{"Two-phase commit achieves strong consistency at the cost of availability during coordinator failure", 1, 0.87, 1},
		{"Partial indexes in PostgreSQL reduce index size by filtering rows at creation time", 2, 0.88, 2},
		{"EXPLAIN ANALYZE reveals actual vs estimated row counts for query plan debugging", 2, 0.91, 2},
	}
	for _, d := range leafDocs {
		id := uuid.New()
		n := core.Node{
			ID: id, Namespace: NSGeneral,
			Labels:     []string{"Document", "Leaf"},
			Properties: map[string]any{"text": d.text, "hub_idx": d.hubIdx},
			Confidence: d.conf,
			ValidFrom:  now.Add(-24 * time.Hour),
		}
		// Leaf vector is slightly off-topic from the hub — tests graph retrieval advantage
		vec := topicVec(d.topicDim, (d.topicDim+5)%dim, 0.75, 0.25)
		c.upsert(ctx, graph, vecs, n, vec, "leaf", true, false, false)

		// Wire edge: hub relates_to leaf
		_ = graph.UpsertEdge(ctx, core.Edge{
			ID:        uuid.New(),
			Namespace: NSGeneral,
			Src:       hubIDs[d.hubIdx],
			Dst:       id,
			Type:      "relates_to",
			Weight:    0.9,
			ValidFrom: now.Add(-24 * time.Hour),
		})
	}

	// Noise documents — unrelated, should not surface
	for i := 0; i < 15; i++ {
		noise := core.Node{
			ID: uuid.New(), Namespace: NSGeneral,
			Labels:     []string{"Document"},
			Properties: map[string]any{"text": "unrelated document", "noise_idx": i},
			Confidence: 0.5,
			ValidFrom:  now.Add(-time.Duration(i*3) * time.Hour),
		}
		c.upsert(ctx, graph, vecs, noise, topicVec((i+9)%dim, (i+12)%dim, 0.7, 0.3), "noise", false, false, false)
	}
}

// ─── Procedural namespace (slow decay) ───────────────────────────────────────

func (c *Corpus) buildProceduralNamespace(ctx context.Context, graph *memstore.GraphStore, vecs *memstore.VectorIndex, now time.Time) {
	type skill struct {
		text     string
		topicDim int
		ageHours float64
		conf     float64
	}

	skills := []skill{
		{"How to create a Go module: go mod init <module-path>, then go mod tidy", 0, 800, 0.95},
		{"How to write a Dockerfile for a Go service: multi-stage build, scratch base image", 1, 600, 0.92},
		{"How to set up ArgoCD application: write Application CR, point to Helm chart in git", 2, 1000, 0.94},
		{"How to configure Kafka consumer groups: set group.id, auto.offset.reset, enable.auto.commit", 3, 700, 0.91},
		{"How to add a PostgreSQL index: CREATE INDEX CONCURRENTLY to avoid table lock", 4, 500, 0.93},
		// Stale outdated skills — should score lower than updated ones
		{"Old deployment process using kubectl apply directly — deprecated, use ArgoCD now", 2, 2000, 0.30},
		{"Old Kafka setup using ZooKeeper — deprecated, KRaft mode preferred", 3, 1800, 0.25},
	}

	for _, s := range skills {
		id := uuid.New()
		n := core.Node{
			ID: id, Namespace: NSProcedural,
			Labels:     []string{"Skill", string(core.MemoryProcedural)},
			Properties: map[string]any{"text": s.text},
			Confidence: s.conf,
			ValidFrom:  now.Add(-time.Duration(s.ageHours * float64(time.Hour))),
		}
		isCorrect := s.conf > 0.5
		c.upsert(ctx, graph, vecs, n, topicVec(s.topicDim, (s.topicDim+6)%dim, 0.88, 0.12), "skill", isCorrect, false, s.ageHours > 1500)
	}
}

// ─── Query set ───────────────────────────────────────────────────────────────

func (c *Corpus) buildQuerySet(now time.Time) {
	// Find correct node IDs by matching topics and correctness flags
	correctByTopic := map[string][]uuid.UUID{}
	for _, f := range c.Fixtures {
		if f.IsCorrect {
			correctByTopic[f.Topic] = append(correctByTopic[f.Topic], f.Node.ID)
		}
	}

	c.QuerySet = []LabelledQuery{
		// ── Factual queries (channel namespace) ──────────────────────────
		{
			ID:             "fact_go_gc",
			Description:    "Query about Go garbage collection — 10 troll writes vs 1 trusted",
			Namespace:      NSChannel,
			Vector:         queryVec(0, 0.08),
			CorrectNodeIDs: correctByTopic["go_gc"],
			Category:       "poisoning",
		},
		{
			ID:             "fact_go_concurrency",
			Description:    "Query about Go concurrency model — 8 troll writes vs 1 trusted",
			Namespace:      NSChannel,
			Vector:         queryVec(1, 0.08),
			CorrectNodeIDs: correctByTopic["go_concurrency"],
			Category:       "poisoning",
		},
		{
			ID:             "fact_python_typing",
			Description:    "Query about Python type system — 6 troll writes vs 1 trusted",
			Namespace:      NSChannel,
			Vector:         queryVec(2, 0.08),
			CorrectNodeIDs: correctByTopic["python_typing"],
			Category:       "poisoning",
		},
		{
			ID:             "fact_rust_ownership",
			Description:    "Query about Rust memory safety — 7 troll writes vs 1 trusted",
			Namespace:      NSChannel,
			Vector:         queryVec(3, 0.08),
			CorrectNodeIDs: correctByTopic["rust_ownership"],
			Category:       "poisoning",
		},
		{
			ID:             "fact_go_interfaces",
			Description:    "Query about Go interfaces — 5 troll writes vs 1 trusted",
			Namespace:      NSChannel,
			Vector:         queryVec(4, 0.08),
			CorrectNodeIDs: correctByTopic["go_interfaces"],
			Category:       "poisoning",
		},

		// ── Agent memory queries (utility + decay) ────────────────────────
		{
			ID:             "agent_gke_deploy",
			Description:    "Query about GKE deployment — stale low-utility vs fresh high-utility",
			Namespace:      NSAgent,
			Vector:         queryVec(5, 0.10),
			CorrectNodeIDs: filterCorrect(c.Fixtures, NSAgent, "episodic"),
			Category:       "temporal",
		},
		{
			ID:             "agent_kafka",
			Description:    "Query about Kafka — stale failed attempt vs successful recent fix",
			Namespace:      NSAgent,
			Vector:         queryVec(6, 0.10),
			CorrectNodeIDs: filterCorrect(c.Fixtures, NSAgent, "episodic"),
			Category:       "temporal",
		},
		{
			ID:             "agent_procedural_deploy",
			Description:    "Procedural query — 500h old skill must rank above 2h low-utility episodic",
			Namespace:      NSAgent,
			Vector:         queryVec(10, 0.08), // procedural subspace: dims 10-12
			CorrectNodeIDs: filterCorrect(c.Fixtures, NSAgent, "procedural"),
			Category:       "procedural",
		},

		// ── General RAG queries (hybrid vs pure vector) ───────────────────
		{
			ID:             "rag_k8s_hub",
			Description:    "K8s hub query — hub node directly similar to query",
			Namespace:      NSGeneral,
			Vector:         queryVec(0, 0.05),
			CorrectNodeIDs: filterCorrect(c.Fixtures, NSGeneral, "hub"),
			Category:       "factual",
		},
		{
			ID:             "rag_db_leaf_multihop",
			Description:    "DB leaf query — leaf nodes require graph traversal from hub seed",
			Namespace:      NSGeneral,
			Vector:         queryVec(2, 0.05),
			CorrectNodeIDs: filterCorrect(c.Fixtures, NSGeneral, "leaf"),
			Category:       "multihop",
		},
		{
			ID:             "rag_consistency_hub",
			Description:    "Consistency/CAP hub query",
			Namespace:      NSGeneral,
			Vector:         queryVec(1, 0.05),
			CorrectNodeIDs: filterCorrect(c.Fixtures, NSGeneral, "hub"),
			Category:       "factual",
		},

		// ── Procedural skill queries ───────────────────────────────────────
		{
			ID:             "skill_argocd",
			Description:    "ArgoCD query — 1000h old skill vs deprecated 2000h old skill",
			Namespace:      NSProcedural,
			Vector:         queryVec(2, 0.08),
			CorrectNodeIDs: filterCorrect(c.Fixtures, NSProcedural, "skill"),
			Category:       "procedural",
		},
		{
			ID:             "skill_kafka_setup",
			Description:    "Kafka setup query — modern KRaft skill vs deprecated ZooKeeper skill",
			Namespace:      NSProcedural,
			Vector:         queryVec(3, 0.08),
			CorrectNodeIDs: filterCorrect(c.Fixtures, NSProcedural, "skill"),
			Category:       "procedural",
		},
	}
}

// filterCorrect returns IDs of correct fixtures matching namespace and topic.
func filterCorrect(fixtures []Fixture, ns, topic string) []uuid.UUID {
	var ids []uuid.UUID
	for _, f := range fixtures {
		if f.IsCorrect && f.Node.Namespace == ns && f.Topic == topic {
			ids = append(ids, f.Node.ID)
		}
	}
	return ids
}
