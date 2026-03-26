package longmemeval

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/pkg/client"
)

// Config configures the benchmark runner.
type Config struct {
	TopK      int    // default: 5
	Namespace string // default: "longmemeval"
	VectorDim int    // default: 8
}

func (c Config) withDefaults() Config {
	if c.TopK == 0 {
		c.TopK = 5
	}
	if c.Namespace == "" {
		c.Namespace = "longmemeval"
	}
	if c.VectorDim == 0 {
		c.VectorDim = 8
	}
	return c
}

// Result holds the outcome of a single query evaluation.
type Result struct {
	QueryID   string
	Category  string
	Question  string
	GoldAnswer string
	Retrieved []string // top-K retrieved texts
	RecallAt1 float64  // 1.0 if gold answer substring found in top-1
	RecallAt5 float64  // 1.0 if gold answer substring found in top-5
	Latency   time.Duration
}

// Report is the aggregate benchmark report.
type Report struct {
	TotalQueries  int
	MeanRecallAt1 float64
	MeanRecallAt5 float64
	MeanLatency   time.Duration
	ByCategory    map[string]CategoryReport
	Results       []Result
}

// CategoryReport aggregates metrics by category.
type CategoryReport struct {
	Count     int
	RecallAt1 float64
	RecallAt5 float64
}

// Runner executes the LongMemEval benchmark.
type Runner struct {
	DB     *client.DB
	Config Config
}

// NewRunner creates a new benchmark runner.
func NewRunner(db *client.DB, cfg Config) *Runner {
	return &Runner{
		DB:     db,
		Config: cfg.withDefaults(),
	}
}

// Run ingests all sessions, evaluates all queries, and returns an aggregate report.
func (r *Runner) Run(ctx context.Context, dataset *Dataset) (*Report, error) {
	ns := r.DB.Namespace(r.Config.Namespace, namespace.ModeGeneral)

	// Phase 1: Ingest all session turns as nodes.
	for _, sess := range dataset.Sessions {
		for turnIdx, turn := range sess.Turns {
			vec := textToVector(turn.Content, r.Config.VectorDim)
			_, err := ns.Write(ctx, client.WriteRequest{
				Content:  turn.Content,
				SourceID: fmt.Sprintf("session:%s:turn:%d", sess.ID, turnIdx),
				Labels:   []string{"Turn", turn.Role},
				Properties: map[string]any{
					"session_id": sess.ID,
					"turn_index": turnIdx,
					"role":       turn.Role,
					"text":       turn.Content,
				},
				Vector:     vec,
				ModelID:    "fnv-hash",
				Confidence: 0.8,
			})
			if err != nil {
				return nil, fmt.Errorf("longmemeval: ingest session %s turn %d: %w", sess.ID, turnIdx, err)
			}
		}
	}

	// Phase 2: Evaluate each query.
	var results []Result
	for _, q := range dataset.Queries {
		queryVec := textToVector(q.Question, r.Config.VectorDim)

		start := time.Now()
		retrieved, err := ns.Retrieve(ctx, client.RetrieveRequest{
			Vector: queryVec,
			TopK:   r.Config.TopK,
		})
		latency := time.Since(start)

		if err != nil {
			return nil, fmt.Errorf("longmemeval: retrieve query %s: %w", q.ID, err)
		}

		// Collect retrieved texts.
		var texts []string
		for _, res := range retrieved {
			text, _ := res.Node.Properties["text"].(string)
			if text == "" {
				text = fmt.Sprintf("node:%s", res.Node.ID)
			}
			texts = append(texts, text)
		}

		// Compute recall: check if gold answer is a substring of any retrieved content.
		goldLower := strings.ToLower(q.GoldAnswer)
		recallAt1 := 0.0
		recallAt5 := 0.0

		for i, text := range texts {
			if strings.Contains(strings.ToLower(text), goldLower) {
				if i == 0 {
					recallAt1 = 1.0
				}
				recallAt5 = 1.0
				break
			}
		}

		results = append(results, Result{
			QueryID:    q.ID,
			Category:   q.Category,
			Question:   q.Question,
			GoldAnswer: q.GoldAnswer,
			Retrieved:  texts,
			RecallAt1:  recallAt1,
			RecallAt5:  recallAt5,
			Latency:    latency,
		})
	}

	// Phase 3: Aggregate report.
	report := &Report{
		TotalQueries: len(results),
		ByCategory:   make(map[string]CategoryReport),
		Results:      results,
	}

	var sumR1, sumR5 float64
	var sumLatency time.Duration
	catR1 := make(map[string]float64)
	catR5 := make(map[string]float64)
	catCount := make(map[string]int)

	for _, res := range results {
		sumR1 += res.RecallAt1
		sumR5 += res.RecallAt5
		sumLatency += res.Latency

		catR1[res.Category] += res.RecallAt1
		catR5[res.Category] += res.RecallAt5
		catCount[res.Category]++
	}

	n := float64(len(results))
	if n > 0 {
		report.MeanRecallAt1 = sumR1 / n
		report.MeanRecallAt5 = sumR5 / n
		report.MeanLatency = time.Duration(float64(sumLatency) / n)
	}

	for cat, count := range catCount {
		c := float64(count)
		report.ByCategory[cat] = CategoryReport{
			Count:     count,
			RecallAt1: catR1[cat] / c,
			RecallAt5: catR5[cat] / c,
		}
	}

	return report, nil
}

// PrintReport writes a formatted benchmark report to stdout.
func (r *Runner) PrintReport(report *Report) {
	fmt.Println()
	fmt.Println("================================================================")
	fmt.Println("       LongMemEval Benchmark Report — contextdb")
	fmt.Println("================================================================")
	fmt.Printf("  Total queries:     %d\n", report.TotalQueries)
	fmt.Printf("  Mean Recall@1:     %.1f%%\n", report.MeanRecallAt1*100)
	fmt.Printf("  Mean Recall@5:     %.1f%%\n", report.MeanRecallAt5*100)
	fmt.Printf("  Mean latency:      %s\n", report.MeanLatency)
	fmt.Println()

	fmt.Println("  By category:")
	fmt.Printf("  %-20s %6s %6s %6s\n", "category", "count", "R@1", "R@5")
	fmt.Println("  " + strings.Repeat("-", 42))
	for cat, cr := range report.ByCategory {
		fmt.Printf("  %-20s %6d %5.0f%% %5.0f%%\n",
			cat, cr.Count, cr.RecallAt1*100, cr.RecallAt5*100)
	}
	fmt.Println()

	fmt.Println("  Per-query results:")
	fmt.Printf("  %-30s %-16s %5s %5s %10s\n", "query", "category", "R@1", "R@5", "latency")
	fmt.Println("  " + strings.Repeat("-", 70))
	for _, res := range report.Results {
		qid := res.QueryID
		if len(qid) > 28 {
			qid = qid[:28]
		}
		fmt.Printf("  %-30s %-16s %4.0f%% %4.0f%% %10s\n",
			qid, res.Category,
			res.RecallAt1*100, res.RecallAt5*100,
			res.Latency.Round(time.Microsecond))
	}
	fmt.Println("================================================================")
	fmt.Println()
}

// textToVector generates a deterministic vector from text using FNV hashing.
// The text is split into overlapping chunks and each chunk hashes to one
// dimension. The resulting vector is L2-normalised to unit length.
func textToVector(text string, dim int) []float32 {
	vec := make([]float32, dim)

	// Hash the full text to set a baseline.
	h := fnv.New64a()
	h.Write([]byte(text))
	base := h.Sum64()

	// Generate one component per dimension by hashing text with a
	// dimension-specific salt.
	for i := 0; i < dim; i++ {
		h.Reset()
		// Salt with dimension index and base hash for spread.
		salt := fmt.Sprintf("%d:%d:%s", i, base, text)
		h.Write([]byte(salt))
		bits := h.Sum64()
		// Map to [-1, 1] range.
		vec[i] = float32(bits)/float32(math.MaxUint64)*2 - 1
	}

	// L2 normalise.
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / norm)
		}
	}

	return vec
}
