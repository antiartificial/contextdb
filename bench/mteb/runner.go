// Package mteb provides an MTEB-style retrieval quality benchmark runner
// for contextdb. MTEB (Massive Text Embedding Benchmark) measures retrieval
// quality by computing NDCG@K, recall@K, and MRR across a set of queries
// with known-relevant documents.
//
// This implementation uses contextdb's retrieval engine with synthetic
// embeddings. The goal is not to compare against external embedding models
// but to validate that contextdb's hybrid retrieval (vector + graph + scoring)
// improves over pure vector baselines on the same embedding space.
//
// Usage:
//
//	runner := mteb.NewRunner(db, mteb.Config{TopK: 10})
//	report, err := runner.Run(ctx, mteb.BuildRetrievalSuite())
//	runner.PrintReport(report)
package mteb

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/pkg/client"
)

// Config configures the MTEB runner.
type Config struct {
	TopK      int    // default: 10
	Namespace string // default: "mteb"
	VectorDim int    // default: 8
}

func (c Config) withDefaults() Config {
	if c.TopK == 0 {
		c.TopK = 10
	}
	if c.Namespace == "" {
		c.Namespace = "mteb"
	}
	if c.VectorDim == 0 {
		c.VectorDim = 8
	}
	return c
}

// Document is a corpus document with a known ID and relevance judgements.
type Document struct {
	ID      string
	Text    string
	Vector  []float32
	Labels  []string
	GroupID string // cluster/topic grouping
}

// RetrievalQuery is a query with relevance judgements.
type RetrievalQuery struct {
	ID       string
	Text     string
	Vector   []float32
	Relevant []string // document IDs that are relevant (ordered by relevance)
}

// Suite holds a retrieval evaluation suite: corpus + queries.
type Suite struct {
	Name      string
	Documents []Document
	Queries   []RetrievalQuery
}

// QueryResult holds evaluation metrics for a single query.
type QueryResult struct {
	QueryID      string
	NDCG10       float64
	RecallAt1    float64
	RecallAt5    float64
	RecallAt10   float64
	MRR          float64 // Mean Reciprocal Rank
	Latency      time.Duration
	RetrievedIDs []string
}

// Report is the aggregate benchmark report.
type Report struct {
	SuiteName    string
	TotalQueries int
	MeanNDCG10   float64
	MeanRecall1  float64
	MeanRecall5  float64
	MeanRecall10 float64
	MeanMRR      float64
	MeanLatency  time.Duration
	Results      []QueryResult
}

// Runner executes the MTEB-style benchmark.
type Runner struct {
	DB     *client.DB
	Config Config
}

// NewRunner creates a new MTEB benchmark runner.
func NewRunner(db *client.DB, cfg Config) *Runner {
	return &Runner{
		DB:     db,
		Config: cfg.withDefaults(),
	}
}

// Run ingests documents, evaluates queries, and returns an aggregate report.
func (r *Runner) Run(ctx context.Context, suite *Suite) (*Report, error) {
	ns := r.DB.Namespace(r.Config.Namespace, namespace.ModeGeneral)

	// Phase 1: Ingest documents.
	docNodeIDs := make(map[string]string) // doc ID → node ID
	for _, doc := range suite.Documents {
		result, err := ns.Write(ctx, client.WriteRequest{
			Content:  doc.Text,
			SourceID: fmt.Sprintf("mteb:%s", doc.ID),
			Labels:   doc.Labels,
			Properties: map[string]any{
				"doc_id":   doc.ID,
				"group_id": doc.GroupID,
				"text":     doc.Text,
			},
			Vector:     doc.Vector,
			ModelID:    "mteb-synthetic",
			Confidence: 0.85,
		})
		if err != nil {
			return nil, fmt.Errorf("mteb: ingest doc %s: %w", doc.ID, err)
		}
		docNodeIDs[doc.ID] = result.NodeID.String()
	}

	// Phase 2: Evaluate queries.
	var results []QueryResult
	for _, q := range suite.Queries {
		start := time.Now()
		retrieved, err := ns.Retrieve(ctx, client.RetrieveRequest{
			Vector: q.Vector,
			TopK:   r.Config.TopK,
		})
		latency := time.Since(start)

		if err != nil {
			return nil, fmt.Errorf("mteb: retrieve query %s: %w", q.ID, err)
		}

		// Map retrieved node IDs back to document IDs.
		var retrievedDocIDs []string
		for _, res := range retrieved {
			docID, _ := res.Node.Properties["doc_id"].(string)
			if docID == "" {
				docID = res.Node.ID.String()
			}
			retrievedDocIDs = append(retrievedDocIDs, docID)
		}

		// Build relevance set.
		relevantSet := make(map[string]int) // doc ID → rank (1-based)
		for i, id := range q.Relevant {
			relevantSet[id] = i + 1
		}

		results = append(results, QueryResult{
			QueryID:      q.ID,
			NDCG10:       ndcg(retrievedDocIDs, relevantSet, 10),
			RecallAt1:    recallAtK(retrievedDocIDs, relevantSet, 1),
			RecallAt5:    recallAtK(retrievedDocIDs, relevantSet, 5),
			RecallAt10:   recallAtK(retrievedDocIDs, relevantSet, 10),
			MRR:          mrr(retrievedDocIDs, relevantSet),
			Latency:      latency,
			RetrievedIDs: retrievedDocIDs,
		})
	}

	// Phase 3: Aggregate.
	report := &Report{
		SuiteName:    suite.Name,
		TotalQueries: len(results),
		Results:      results,
	}

	var sumNDCG, sumR1, sumR5, sumR10, sumMRR float64
	var sumLatency time.Duration
	for _, res := range results {
		sumNDCG += res.NDCG10
		sumR1 += res.RecallAt1
		sumR5 += res.RecallAt5
		sumR10 += res.RecallAt10
		sumMRR += res.MRR
		sumLatency += res.Latency
	}

	n := float64(len(results))
	if n > 0 {
		report.MeanNDCG10 = sumNDCG / n
		report.MeanRecall1 = sumR1 / n
		report.MeanRecall5 = sumR5 / n
		report.MeanRecall10 = sumR10 / n
		report.MeanMRR = sumMRR / n
		report.MeanLatency = time.Duration(float64(sumLatency) / n)
	}

	return report, nil
}

// PrintReport writes a formatted benchmark report to stdout.
func (r *Runner) PrintReport(report *Report) {
	fmt.Println()
	fmt.Println("================================================================")
	fmt.Printf("  MTEB Retrieval Report — %s\n", report.SuiteName)
	fmt.Println("================================================================")
	fmt.Printf("  Total queries:     %d\n", report.TotalQueries)
	fmt.Printf("  Mean NDCG@10:      %.3f\n", report.MeanNDCG10)
	fmt.Printf("  Mean Recall@1:     %.1f%%\n", report.MeanRecall1*100)
	fmt.Printf("  Mean Recall@5:     %.1f%%\n", report.MeanRecall5*100)
	fmt.Printf("  Mean Recall@10:    %.1f%%\n", report.MeanRecall10*100)
	fmt.Printf("  Mean MRR:          %.3f\n", report.MeanMRR)
	fmt.Printf("  Mean latency:      %s\n", report.MeanLatency)
	fmt.Println()

	fmt.Println("  Per-query results:")
	fmt.Printf("  %-25s %7s %7s %7s %7s %10s\n",
		"query", "NDCG@10", "R@1", "R@5", "MRR", "latency")
	fmt.Println("  " + strings.Repeat("-", 70))
	for _, res := range report.Results {
		qid := res.QueryID
		if len(qid) > 23 {
			qid = qid[:23]
		}
		fmt.Printf("  %-25s %6.3f  %5.0f%%  %5.0f%%  %6.3f  %10s\n",
			qid, res.NDCG10,
			res.RecallAt1*100, res.RecallAt5*100,
			res.MRR,
			res.Latency.Round(time.Microsecond))
	}
	fmt.Println("================================================================")
	fmt.Println()
}

// ─── Metric functions ────────────────────────────────────────────────────────

// ndcg computes NDCG@K (Normalised Discounted Cumulative Gain).
func ndcg(retrieved []string, relevant map[string]int, k int) float64 {
	if len(relevant) == 0 {
		return 0
	}

	limit := k
	if limit > len(retrieved) {
		limit = len(retrieved)
	}

	// DCG from the actual ranked list.
	dcg := 0.0
	for i := 0; i < limit; i++ {
		if _, ok := relevant[retrieved[i]]; ok {
			dcg += 1.0 / math.Log2(float64(i+2)) // position is 1-indexed
		}
	}

	// Ideal DCG: all relevant documents at the top.
	numRelevant := len(relevant)
	if numRelevant > k {
		numRelevant = k
	}
	idcg := 0.0
	for i := 0; i < numRelevant; i++ {
		idcg += 1.0 / math.Log2(float64(i+2))
	}

	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

// recallAtK returns the fraction of relevant documents found in the top-K.
func recallAtK(retrieved []string, relevant map[string]int, k int) float64 {
	if len(relevant) == 0 {
		return 0
	}

	limit := k
	if limit > len(retrieved) {
		limit = len(retrieved)
	}

	found := 0
	for i := 0; i < limit; i++ {
		if _, ok := relevant[retrieved[i]]; ok {
			found++
		}
	}
	return float64(found) / float64(len(relevant))
}

// mrr returns the Mean Reciprocal Rank: 1/rank of the first relevant document.
func mrr(retrieved []string, relevant map[string]int) float64 {
	for i, id := range retrieved {
		if _, ok := relevant[id]; ok {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// ─── Built-in suite ──────────────────────────────────────────────────────────

// BuildRetrievalSuite creates a synthetic retrieval benchmark suite with
// clustered documents and queries. This tests whether contextdb can
// retrieve relevant documents within and across topic clusters.
func BuildRetrievalSuite() *Suite {
	dim := 8

	type cluster struct {
		name     string
		baseDim  int
		numDocs  int
		labels   []string
	}

	clusters := []cluster{
		{"databases", 0, 6, []string{"Document", "Databases"}},
		{"networking", 1, 5, []string{"Document", "Networking"}},
		{"security", 2, 5, []string{"Document", "Security"}},
		{"devops", 3, 6, []string{"Document", "DevOps"}},
		{"algorithms", 4, 4, []string{"Document", "Algorithms"}},
	}

	docTexts := map[string][]string{
		"databases": {
			"PostgreSQL uses MVCC for concurrent transactions without read locks",
			"Redis provides in-memory key-value storage with persistence options",
			"MongoDB stores documents in BSON format with flexible schemas",
			"SQLite is an embedded database engine that requires no server",
			"Cassandra uses consistent hashing for distributed data partitioning",
			"CockroachDB implements serializable isolation using optimistic concurrency",
		},
		"networking": {
			"TCP provides reliable ordered delivery of a stream of bytes",
			"UDP is connectionless and does not guarantee delivery or ordering",
			"HTTP/2 multiplexes streams over a single TCP connection",
			"gRPC uses HTTP/2 and protocol buffers for efficient RPC",
			"WebSocket provides full-duplex communication over a single TCP connection",
		},
		"security": {
			"TLS 1.3 reduces handshake latency to one round trip",
			"JWT tokens encode claims in a signed JSON payload",
			"OAuth 2.0 delegates authorization via access tokens",
			"Bcrypt uses a cost factor to make password hashing intentionally slow",
			"CORS headers control cross-origin requests from web browsers",
		},
		"devops": {
			"Docker containers share the host kernel and isolate processes via namespaces",
			"Kubernetes orchestrates container workloads across a cluster of nodes",
			"Terraform manages infrastructure as code with declarative HCL configuration",
			"Prometheus scrapes metrics endpoints and stores time-series data",
			"ArgoCD implements GitOps continuous delivery for Kubernetes",
			"Helm packages Kubernetes manifests as versioned charts with templating",
		},
		"algorithms": {
			"Quicksort has average O(n log n) complexity with in-place partitioning",
			"Binary search requires a sorted array and runs in O(log n) time",
			"Dijkstra algorithm finds shortest paths in graphs with non-negative weights",
			"Bloom filters provide probabilistic set membership with no false negatives",
		},
	}

	var docs []Document
	docID := 0
	for _, cl := range clusters {
		texts := docTexts[cl.name]
		for i := 0; i < cl.numDocs && i < len(texts); i++ {
			vec := makeClusterVec(dim, cl.baseDim, i, 0.85, 0.15)
			docs = append(docs, Document{
				ID:      fmt.Sprintf("doc-%s-%d", cl.name, i),
				Text:    texts[i],
				Vector:  vec,
				Labels:  cl.labels,
				GroupID: cl.name,
			})
			docID++
		}
	}

	// Queries: each targets documents from one or two clusters.
	queries := []RetrievalQuery{
		{
			ID:       "q-db-mvcc",
			Text:     "How does PostgreSQL handle concurrent transactions?",
			Vector:   makeQueryVec(dim, 0, 0.08),
			Relevant: []string{"doc-databases-0", "doc-databases-5"},
		},
		{
			ID:       "q-db-inmemory",
			Text:     "Which databases support in-memory storage?",
			Vector:   makeQueryVec(dim, 0, 0.12),
			Relevant: []string{"doc-databases-1", "doc-databases-3"},
		},
		{
			ID:       "q-net-transport",
			Text:     "Compare TCP and UDP transport protocols",
			Vector:   makeQueryVec(dim, 1, 0.08),
			Relevant: []string{"doc-networking-0", "doc-networking-1"},
		},
		{
			ID:       "q-net-rpc",
			Text:     "What protocols are used for RPC?",
			Vector:   makeQueryVec(dim, 1, 0.15),
			Relevant: []string{"doc-networking-3", "doc-networking-2"},
		},
		{
			ID:       "q-sec-auth",
			Text:     "How does web authentication work?",
			Vector:   makeQueryVec(dim, 2, 0.10),
			Relevant: []string{"doc-security-1", "doc-security-2"},
		},
		{
			ID:       "q-sec-tls",
			Text:     "What improvements does TLS 1.3 bring?",
			Vector:   makeQueryVec(dim, 2, 0.06),
			Relevant: []string{"doc-security-0"},
		},
		{
			ID:       "q-devops-k8s",
			Text:     "How does Kubernetes manage containers?",
			Vector:   makeQueryVec(dim, 3, 0.08),
			Relevant: []string{"doc-devops-1", "doc-devops-0", "doc-devops-5"},
		},
		{
			ID:       "q-devops-gitops",
			Text:     "What tools support GitOps?",
			Vector:   makeQueryVec(dim, 3, 0.12),
			Relevant: []string{"doc-devops-4", "doc-devops-2"},
		},
		{
			ID:       "q-algo-sort",
			Text:     "What is the complexity of quicksort?",
			Vector:   makeQueryVec(dim, 4, 0.08),
			Relevant: []string{"doc-algorithms-0"},
		},
		{
			ID:       "q-algo-graph",
			Text:     "How to find shortest paths in a graph?",
			Vector:   makeQueryVec(dim, 4, 0.10),
			Relevant: []string{"doc-algorithms-2"},
		},
		// Cross-cluster query: databases + devops.
		{
			ID:       "q-cross-db-devops",
			Text:     "How to manage database infrastructure?",
			Vector:   makeCrossVec(dim, 0, 3, 0.5),
			Relevant: []string{"doc-databases-0", "doc-devops-2"},
		},
		// Cross-cluster: networking + security.
		{
			ID:       "q-cross-net-sec",
			Text:     "How is network communication secured?",
			Vector:   makeCrossVec(dim, 1, 2, 0.5),
			Relevant: []string{"doc-security-0", "doc-networking-2"},
		},
	}

	return &Suite{
		Name:      "contextdb-retrieval-v1",
		Documents: docs,
		Queries:   queries,
	}
}

// ─── Vector helpers ──────────────────────────────────────────────────────────

func makeClusterVec(dim, baseDim, idx int, topicWeight, noiseWeight float64) []float32 {
	v := make([]float32, dim)
	v[baseDim%dim] = float32(topicWeight)
	v[(baseDim+idx+1)%dim] += float32(noiseWeight)
	return l2Normalise(v)
}

func makeQueryVec(dim, baseDim int, drift float64) []float32 {
	v := make([]float32, dim)
	v[baseDim%dim] = float32(1.0 - drift)
	v[(baseDim+1)%dim] = float32(drift)
	return l2Normalise(v)
}

func makeCrossVec(dim, dim1, dim2 int, balance float64) []float32 {
	v := make([]float32, dim)
	v[dim1%dim] = float32(balance)
	v[dim2%dim] = float32(1.0 - balance)
	return l2Normalise(v)
}

func l2Normalise(v []float32) []float32 {
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
	// Sort to ensure deterministic ordering (not needed; just normalise).
	_ = sort.SliceIsSorted(out, func(i, j int) bool { return false })
	return out
}
