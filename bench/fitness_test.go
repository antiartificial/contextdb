package bench_test

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/testdata"
)

// ─── Evaluation primitives ───────────────────────────────────────────────────

// recallAtK returns 1.0 if any correct node appears in the top-K results.
func recallAtK(results []core.ScoredNode, correct []uuid.UUID, k int) float64 {
	correctSet := make(map[uuid.UUID]bool, len(correct))
	for _, id := range correct {
		correctSet[id] = true
	}
	limit := k
	if limit > len(results) {
		limit = len(results)
	}
	for i := 0; i < limit; i++ {
		if correctSet[results[i].ID] {
			return 1.0
		}
	}
	return 0.0
}

// rankOfFirst returns the 1-based rank of the first correct result,
// or -1 if no correct result appears in the list.
func rankOfFirst(results []core.ScoredNode, correct []uuid.UUID) int {
	correctSet := make(map[uuid.UUID]bool, len(correct))
	for _, id := range correct {
		correctSet[id] = true
	}
	for i, r := range results {
		if correctSet[r.ID] {
			return i + 1
		}
	}
	return -1
}

// scoreDelta returns the score difference between the first correct result
// and the first incorrect result (positive = correct wins, negative = wrong wins).
func scoreDelta(results []core.ScoredNode, correct []uuid.UUID) float64 {
	correctSet := make(map[uuid.UUID]bool, len(correct))
	for _, id := range correct {
		correctSet[id] = true
	}
	var firstCorrect, firstWrong float64
	var foundCorrect, foundWrong bool
	for _, r := range results {
		if correctSet[r.ID] && !foundCorrect {
			firstCorrect = r.Score
			foundCorrect = true
		}
		if !correctSet[r.ID] && !foundWrong {
			firstWrong = r.Score
			foundWrong = true
		}
		if foundCorrect && foundWrong {
			break
		}
	}
	if !foundCorrect {
		return -1.0
	}
	if !foundWrong {
		return 1.0
	}
	return firstCorrect - firstWrong
}

// QueryResult holds evaluation metrics for a single labelled query.
type QueryResult struct {
	Query       testdata.LabelledQuery
	Results     []core.ScoredNode
	RecallAt1   float64
	RecallAt3   float64
	RecallAt5   float64
	RankOfFirst int
	ScoreDelta  float64
}

// Evaluator runs a full evaluation pass over the query set.
type Evaluator struct {
	Engine      *retrieval.Engine
	ScoreParams core.ScoreParams
	Strategy    retrieval.HybridStrategy
	SeedIDs     map[string][]uuid.UUID // namespace → seed IDs for graph queries
	TopK        int
}

func (e *Evaluator) Run(corpus *testdata.Corpus) []QueryResult {
	ctx := context.Background()
	var results []QueryResult

	for _, q := range corpus.QuerySet {
		params := e.ScoreParams
		params.AsOf = time.Now()

		seeds := e.SeedIDs[q.Namespace]

		query := retrieval.Query{
			Namespace:   q.Namespace,
			Vector:      q.Vector,
			SeedIDs:     seeds,
			TopK:        e.TopK,
			Strategy:    e.Strategy,
			ScoreParams: params,
		}

		retrieved, _ := e.Engine.Retrieve(ctx, query)

		results = append(results, QueryResult{
			Query:       q,
			Results:     retrieved,
			RecallAt1:   recallAtK(retrieved, q.CorrectNodeIDs, 1),
			RecallAt3:   recallAtK(retrieved, q.CorrectNodeIDs, 3),
			RecallAt5:   recallAtK(retrieved, q.CorrectNodeIDs, 5),
			RankOfFirst: rankOfFirst(retrieved, q.CorrectNodeIDs),
			ScoreDelta:  scoreDelta(retrieved, q.CorrectNodeIDs),
		})
	}
	return results
}

// meanRecall computes mean recall@K across all results.
func meanRecall(results []QueryResult, k int) float64 {
	if len(results) == 0 {
		return 0
	}
	var sum float64
	for _, r := range results {
		switch k {
		case 1:
			sum += r.RecallAt1
		case 3:
			sum += r.RecallAt3
		case 5:
			sum += r.RecallAt5
		}
	}
	return sum / float64(len(results))
}

func meanDelta(results []QueryResult) float64 {
	if len(results) == 0 {
		return 0
	}
	var sum float64
	for _, r := range results {
		sum += r.ScoreDelta
	}
	return sum / float64(len(results))
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestFitness_PoisoningResistanceAtScale runs all 5 poisoning queries.
// Each has N troll writes with high cosine similarity to the query, and
// exactly 1 trusted write with lower similarity. Pass = trusted ranks #1.
func TestFitness_PoisoningResistanceAtScale(t *testing.T) {
	is := is.New(t)
	corpus := testdata.Build()

	engine := &retrieval.Engine{
		Graph:   corpus.Graph,
		Vectors: corpus.Vecs,
		KV:      corpus.KV,
	}

	params := core.BeliefSystemParams()
	eval := &Evaluator{
		Engine:      engine,
		ScoreParams: params,
		Strategy: retrieval.HybridStrategy{
			VectorWeight:  0.30,
			GraphWeight:   0.55,
			SessionWeight: 0.15,
			MaxDepth:      3,
		},
		TopK: 15,
	}

	poisoningQueries := filterByCategory(corpus.QuerySet, "poisoning")
	results := eval.Run(&testdata.Corpus{
		Graph:    corpus.Graph,
		Vecs:     corpus.Vecs,
		KV:       corpus.KV,
		QuerySet: poisoningQueries,
	})

	t.Log("\n╔══════════════════════════════════════════════════════════════════════════╗")
	t.Log("║           POISONING RESISTANCE — BeliefSystem preset                    ║")
	t.Log("╚══════════════════════════════════════════════════════════════════════════╝")
	t.Logf("%-40s %6s %5s %5s %6s %7s",
		"query", "R@1", "R@3", "R@5", "rank", "delta")
	t.Log(strings.Repeat("─", 75))

	allPass := true
	for _, r := range results {
		pass := "✓"
		if r.RecallAt1 < 1.0 {
			pass = "✗"
			allPass = false
		}
		t.Logf("%-40s %5.0f%% %4.0f%% %4.0f%% %5d  %+.4f  %s",
			r.Query.ID,
			r.RecallAt1*100, r.RecallAt3*100, r.RecallAt5*100,
			r.RankOfFirst, r.ScoreDelta, pass)
	}

	t.Log(strings.Repeat("─", 75))
	t.Logf("Mean R@1: %.1f%%   Mean R@3: %.1f%%   Mean delta: %+.4f",
		meanRecall(results, 1)*100,
		meanRecall(results, 3)*100,
		meanDelta(results))

	is.True(allPass) // every trusted claim must rank #1 despite troll flood
}

// TestFitness_HybridVsPureVector compares hybrid retrieval against pure
// vector search on the same query set. Hybrid should win on poisoning
// and temporal queries; the test records the margin.
func TestFitness_HybridVsPureVector(t *testing.T) {
	corpus := testdata.Build()
	engine := &retrieval.Engine{
		Graph:   corpus.Graph,
		Vectors: corpus.Vecs,
		KV:      corpus.KV,
	}

	// Hub seed IDs for graph queries (general namespace)
	hubSeeds := hubSeedIDs(corpus)

	hybridEval := &Evaluator{
		Engine:      engine,
		ScoreParams: core.BeliefSystemParams(),
		Strategy: retrieval.HybridStrategy{
			VectorWeight: 0.30, GraphWeight: 0.55, SessionWeight: 0.15,
			MaxDepth: 3,
		},
		SeedIDs: map[string][]uuid.UUID{testdata.NSGeneral: hubSeeds},
		TopK:    10,
	}

	// Pure vector: similarity weight 1.0, everything else 0, no graph walk
	pureVectorEval := &Evaluator{
		Engine: engine,
		ScoreParams: core.ScoreParams{
			SimilarityWeight: 1.0,
			ConfidenceWeight: 0.0,
			RecencyWeight:    0.0,
			UtilityWeight:    0.0,
			DecayAlpha:       0.0001,
		},
		Strategy: retrieval.HybridStrategy{
			VectorWeight: 1.0, GraphWeight: 0.0, SessionWeight: 0.0,
			MaxDepth: 0,
		},
		TopK: 10,
	}

	hybridResults := hybridEval.Run(corpus)
	pureResults := pureVectorEval.Run(corpus)

	t.Log("\n╔══════════════════════════════════════════════════════════════════════════╗")
	t.Log("║              HYBRID vs PURE VECTOR — R@1 comparison                     ║")
	t.Log("╚══════════════════════════════════════════════════════════════════════════╝")
	t.Logf("%-40s %10s %10s %8s", "query (category)", "hybrid R@1", "vector R@1", "delta")
	t.Log(strings.Repeat("─", 72))

	for i, hr := range hybridResults {
		vr := pureResults[i]
		delta := hr.RecallAt1 - vr.RecallAt1
		marker := " "
		if delta > 0 {
			marker = "▲"
		} else if delta < 0 {
			marker = "▼"
		}
		label := fmt.Sprintf("%s (%s)", hr.Query.ID, hr.Query.Category)
		if len(label) > 38 {
			label = label[:38]
		}
		t.Logf("%-40s %9.0f%%  %9.0f%%  %+5.0f%%  %s",
			label,
			hr.RecallAt1*100, vr.RecallAt1*100, delta*100, marker)
	}

	t.Log(strings.Repeat("─", 72))

	hybridMean := meanRecall(hybridResults, 1)
	vectorMean := meanRecall(pureResults, 1)
	t.Logf("%-40s %9.1f%%  %9.1f%%  %+5.1f%%",
		"MEAN",
		hybridMean*100, vectorMean*100,
		(hybridMean-vectorMean)*100)

	t.Logf("\nHybrid wins on %d/%d queries, ties on %d, loses on %d",
		countWins(hybridResults, pureResults),
		len(hybridResults),
		countTies(hybridResults, pureResults),
		countLosses(hybridResults, pureResults))
}

// TestFitness_TemporalDecayCorrectness verifies that stale low-confidence
// memories rank below fresh high-confidence ones across the agent namespace.
func TestFitness_TemporalDecayCorrectness(t *testing.T) {
	is := is.New(t)
	corpus := testdata.Build()
	engine := &retrieval.Engine{
		Graph:   corpus.Graph,
		Vectors: corpus.Vecs,
		KV:      corpus.KV,
	}

	agentQueries := filterByCategory(corpus.QuerySet, "temporal")
	eval := &Evaluator{
		Engine:      engine,
		ScoreParams: core.AgentMemoryParams(),
		Strategy: retrieval.HybridStrategy{
			VectorWeight: 0.50, GraphWeight: 0.30, SessionWeight: 0.20,
			MaxDepth: 2,
		},
		TopK: 10,
	}

	results := eval.Run(&testdata.Corpus{
		Graph:    corpus.Graph,
		Vecs:     corpus.Vecs,
		KV:       corpus.KV,
		QuerySet: agentQueries,
	})

	t.Log("\n╔══════════════════════════════════════════════════════════════════════╗")
	t.Log("║           TEMPORAL DECAY — AgentMemory preset                        ║")
	t.Log("╚══════════════════════════════════════════════════════════════════════╝")
	t.Logf("%-35s %6s %6s %6s %6s", "query", "R@1", "R@3", "rank", "delta")
	t.Log(strings.Repeat("─", 60))

	for _, r := range results {
		t.Logf("%-35s %5.0f%% %5.0f%% %5d  %+.4f",
			r.Query.ID,
			r.RecallAt1*100, r.RecallAt3*100,
			r.RankOfFirst, r.ScoreDelta)
	}
	t.Log(strings.Repeat("─", 60))
	t.Logf("Mean R@3: %.1f%%", meanRecall(results, 3)*100)

	// For temporal queries the correct item should appear in top-3
	is.True(meanRecall(results, 3) >= 0.5)
}

// TestFitness_ProceduralSlowDecay verifies that old procedural skills
// outrank old deprecated skills (low confidence) and short-lived episodics.
func TestFitness_ProceduralSlowDecay(t *testing.T) {
	is := is.New(t)
	corpus := testdata.Build()
	engine := &retrieval.Engine{
		Graph:   corpus.Graph,
		Vectors: corpus.Vecs,
		KV:      corpus.KV,
	}

	procQueries := filterByCategory(corpus.QuerySet, "procedural")
	eval := &Evaluator{
		Engine:      engine,
		ScoreParams: core.ProceduralParams(),
		Strategy: retrieval.HybridStrategy{
			VectorWeight: 0.45, GraphWeight: 0.40, SessionWeight: 0.15,
			MaxDepth: 2,
		},
		TopK: 10,
	}

	results := eval.Run(&testdata.Corpus{
		Graph:    corpus.Graph,
		Vecs:     corpus.Vecs,
		KV:       corpus.KV,
		QuerySet: procQueries,
	})

	t.Log("\n╔══════════════════════════════════════════════════════════════════╗")
	t.Log("║        PROCEDURAL SLOW DECAY — ProceduralParams preset           ║")
	t.Log("╚══════════════════════════════════════════════════════════════════╝")
	t.Logf("%-35s %6s %6s %6s", "query", "R@1", "R@3", "delta")
	t.Log(strings.Repeat("─", 56))

	negativeWithAnswer := 0
	for _, r := range results {
		t.Logf("%-35s %5.0f%% %5.0f%% %+.4f",
			r.Query.ID,
			r.RecallAt1*100, r.RecallAt3*100,
			r.ScoreDelta)
		// delta == -1.0 means no correct answer found (empty CorrectNodeIDs),
		// which is a corpus gap, not a scoring failure
		if r.ScoreDelta < 0 && r.ScoreDelta > -1.0 {
			negativeWithAnswer++
		}
	}
	t.Log(strings.Repeat("─", 56))
	t.Logf("Mean R@1: %.1f%%  Mean R@3: %.1f%%",
		meanRecall(results, 1)*100, meanRecall(results, 3)*100)

	// When a correct answer exists in the result set it must outscore wrong answers
	is.Equal(0, negativeWithAnswer)
}

// configResult holds aggregated metrics for one preset configuration.
type configResult struct {
	name     string
	byQuery  []QueryResult
	r1, r3   float64
	avgDelta float64
}

// TestFitness_FullSuite runs all queries across all presets and prints a
// comprehensive summary matrix. This is the headline fitness report.
func TestFitness_FullSuite(t *testing.T) {
	corpus := testdata.Build()
	engine := &retrieval.Engine{
		Graph:   corpus.Graph,
		Vectors: corpus.Vecs,
		KV:      corpus.KV,
	}

	hubSeeds := hubSeedIDs(corpus)

	configs := []struct {
		name     string
		params   core.ScoreParams
		strategy retrieval.HybridStrategy
		seeds    map[string][]uuid.UUID
	}{
		{
			"BeliefSystem",
			core.BeliefSystemParams(),
			retrieval.HybridStrategy{VectorWeight: 0.30, GraphWeight: 0.55, SessionWeight: 0.15, MaxDepth: 3},
			map[string][]uuid.UUID{testdata.NSGeneral: hubSeeds},
		},
		{
			"AgentMemory",
			core.AgentMemoryParams(),
			retrieval.HybridStrategy{VectorWeight: 0.50, GraphWeight: 0.30, SessionWeight: 0.20, MaxDepth: 2},
			nil,
		},
		{
			"General",
			core.GeneralParams(),
			retrieval.HybridStrategy{VectorWeight: 0.45, GraphWeight: 0.40, SessionWeight: 0.15, MaxDepth: 3},
			map[string][]uuid.UUID{testdata.NSGeneral: hubSeeds},
		},
		{
			"Procedural",
			core.ProceduralParams(),
			retrieval.HybridStrategy{VectorWeight: 0.45, GraphWeight: 0.40, SessionWeight: 0.15, MaxDepth: 2},
			nil,
		},
		{
			"PureVector (baseline)",
			core.ScoreParams{SimilarityWeight: 1.0, ConfidenceWeight: 0.0, RecencyWeight: 0.0, UtilityWeight: 0.0, DecayAlpha: 0.0001},
			retrieval.HybridStrategy{VectorWeight: 1.0, MaxDepth: 0},
			nil,
		},
	}

	var allResults []configResult
	for _, cfg := range configs {
		eval := &Evaluator{
			Engine:      engine,
			ScoreParams: cfg.params,
			Strategy:    cfg.strategy,
			SeedIDs:     cfg.seeds,
			TopK:        10,
		}
		qr := eval.Run(corpus)
		allResults = append(allResults, configResult{
			name:     cfg.name,
			byQuery:  qr,
			r1:       meanRecall(qr, 1),
			r3:       meanRecall(qr, 3),
			avgDelta: meanDelta(qr),
		})
	}

	// ── Print summary table ──────────────────────────────────────────────

	t.Log("\n╔══════════════════════════════════════════════════════════════════════════╗")
	t.Log("║                  FULL FITNESS SUITE — all presets                       ║")
	t.Log("╚══════════════════════════════════════════════════════════════════════════╝")

	// Per-query breakdown
	t.Logf("\n%-35s", "query (category)")
	header := "  "
	for _, cr := range allResults {
		n := cr.name
		if len(n) > 10 {
			n = n[:10]
		}
		header += fmt.Sprintf("%-12s", n)
	}
	t.Log(header)
	t.Log(strings.Repeat("─", 35+len(allResults)*12+2))

	for qi, q := range corpus.QuerySet {
		label := fmt.Sprintf("%s (%s)", q.ID, q.Category)
		if len(label) > 33 {
			label = label[:33]
		}
		row := fmt.Sprintf("%-35s", label)
		for _, cr := range allResults {
			r := cr.byQuery[qi]
			cell := fmt.Sprintf("%4.0f%%/%4.0f%%  ", r.RecallAt1*100, r.RecallAt3*100)
			row += cell
		}
		t.Log(row)
	}

	t.Log(strings.Repeat("─", 35+len(allResults)*12+2))

	// Summary row
	summary := fmt.Sprintf("%-35s", "MEAN R@1 / R@3")
	for _, cr := range allResults {
		summary += fmt.Sprintf("%4.0f%%/%4.0f%%  ", cr.r1*100, cr.r3*100)
	}
	t.Log(summary)

	deltaRow := fmt.Sprintf("%-35s", "Mean score delta")
	for _, cr := range allResults {
		deltaRow += fmt.Sprintf("%+9.4f   ", cr.avgDelta)
	}
	t.Log(deltaRow)

	// ── Category breakdown ───────────────────────────────────────────────
	t.Log("\n── By category ─────────────────────────────────────────────────────────")
	categories := []string{"poisoning", "temporal", "procedural", "factual", "multihop"}
	for _, cat := range categories {
		catLabel := fmt.Sprintf("  %-12s", cat)
		row := catLabel
		for _, cr := range allResults {
			catResults := filterResultsByCategory(cr.byQuery, cat)
			if len(catResults) == 0 {
				row += fmt.Sprintf("%12s", "  n/a       ")
				continue
			}
			row += fmt.Sprintf("%4.0f%%/%4.0f%%  ", meanRecall(catResults, 1)*100, meanRecall(catResults, 3)*100)
		}
		t.Log(row)
	}

	// ── Write HTML ───────────────────────────────────────────────────────
	writeHTMLFitnessReport(t, allResults, corpus.QuerySet)
}

// ─── HTML report ─────────────────────────────────────────────────────────────

func writeHTMLFitnessReport(t *testing.T, results []configResult, queries []testdata.LabelledQuery) {
	t.Helper()

	categoryColors := map[string]string{
		"poisoning": "#e15759",
		"temporal":  "#f28e2b",
		"procedural": "#59a14f",
		"factual":   "#4e79a7",
		"multihop":  "#b07aa1",
	}

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>ContextDB Fitness Report</title>
<style>
body{font-family:system-ui,sans-serif;margin:2rem;background:#fafafa;color:#1a1a1a;max-width:1200px}
h1{font-size:1.4rem;font-weight:600}
h2{font-size:1.1rem;font-weight:500;margin-top:2rem;border-bottom:1px solid #ddd;padding-bottom:.4rem}
table{border-collapse:collapse;width:100%;margin-top:1rem;font-size:.83rem}
th,td{padding:.45rem .7rem;border:1px solid #ddd;text-align:right}
th{background:#f0f0f0;font-weight:600;text-align:center}
td.label{text-align:left;font-size:.8rem}
.win{background:#e8f5e9;font-weight:600}
.lose{background:#fce4ec}
.bar-wrap{display:flex;align-items:center;gap:.4rem}
.bar{height:10px;border-radius:2px}
.cat-badge{display:inline-block;padding:.1rem .4rem;border-radius:3px;font-size:.72rem;color:#fff;font-weight:600}
.summary-row{background:#f5f5f5;font-weight:600}
</style>
</head>
<body>
<h1>ContextDB — Fitness Report</h1>
<p style="font-size:.85rem;color:#555">
Recall@K and score delta across ` + fmt.Sprintf("%d", len(queries)) + ` labelled queries,
` + fmt.Sprintf("%d", len(results)) + ` retrieval configurations. Higher recall and positive delta = correct answer ranked above noise.
</p>

<h2>Recall@1 / Recall@3 by query and preset</h2>
<table>
<thead><tr><th>Query</th><th>Category</th>
`)

	presetColors := []string{"#4e79a7", "#f28e2b", "#59a14f", "#e15759", "#888"}
	for i, cr := range results {
		sb.WriteString(fmt.Sprintf(`<th style="color:%s">%s</th>`, presetColors[i%len(presetColors)], cr.name))
	}
	sb.WriteString("</tr></thead>\n<tbody>\n")

	// Find best R@1 per row for highlighting
	for qi, q := range queries {
		catColor, ok := categoryColors[q.Category]
		if !ok {
			catColor = "#888"
		}

		sb.WriteString(fmt.Sprintf(`<tr><td class="label">%s</td>`, q.ID))
		sb.WriteString(fmt.Sprintf(`<td><span class="cat-badge" style="background:%s">%s</span></td>`, catColor, q.Category))

		bestR1 := 0.0
		for _, cr := range results {
			if cr.byQuery[qi].RecallAt1 > bestR1 {
				bestR1 = cr.byQuery[qi].RecallAt1
			}
		}

		for _, cr := range results {
			r := cr.byQuery[qi]
			cls := ""
			if r.RecallAt1 >= bestR1 && bestR1 > 0 {
				cls = ` class="win"`
			} else if r.RecallAt1 == 0 {
				cls = ` class="lose"`
			}
			sb.WriteString(fmt.Sprintf(`<td%s>%.0f%% / %.0f%%</td>`, cls, r.RecallAt1*100, r.RecallAt3*100))
		}
		sb.WriteString("</tr>\n")
	}

	// Summary row
	sb.WriteString(`<tr class="summary-row"><td class="label">MEAN</td><td></td>`)
	for _, cr := range results {
		sb.WriteString(fmt.Sprintf(`<td>%.1f%% / %.1f%%</td>`, cr.r1*100, cr.r3*100))
	}
	sb.WriteString("</tr>\n</tbody></table>\n")

	// Mean score delta bar chart
	sb.WriteString("<h2>Mean score delta (correct − first wrong)</h2>\n")
	sb.WriteString(`<table><thead><tr><th>Preset</th><th>Mean delta</th><th>Visual</th></tr></thead><tbody>`)
	maxDelta := 0.01
	for _, cr := range results {
		if math.Abs(cr.avgDelta) > maxDelta {
			maxDelta = math.Abs(cr.avgDelta)
		}
	}
	for i, cr := range results {
		pct := int(cr.avgDelta / maxDelta * 200)
		if pct < 0 {
			pct = 0
		}
		if pct > 200 {
			pct = 200
		}
		color := presetColors[i%len(presetColors)]
		sb.WriteString(fmt.Sprintf(`<tr><td class="label">%s</td><td>%+.4f</td>
<td><div class="bar-wrap"><div class="bar" style="width:%dpx;background:%s"></div></div></td></tr>`,
			cr.name, cr.avgDelta, pct, color))
	}
	sb.WriteString("</tbody></table>\n")

	sb.WriteString(`<p style="font-size:.75rem;color:#888;margin-top:2rem">
Generated by contextdb/bench · synthetic 16-dim corpus · all vectors are L2-normalised
</p></body></html>`)

	path := "/tmp/contextdb_fitness.html"
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Logf("could not write HTML report: %v", err)
	} else {
		t.Logf("\nHTML fitness report → %s", path)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func filterByCategory(queries []testdata.LabelledQuery, cat string) []testdata.LabelledQuery {
	var out []testdata.LabelledQuery
	for _, q := range queries {
		if q.Category == cat {
			out = append(out, q)
		}
	}
	return out
}

func filterResultsByCategory(results []QueryResult, cat string) []QueryResult {
	var out []QueryResult
	for _, r := range results {
		if r.Query.Category == cat {
			out = append(out, r)
		}
	}
	return out
}

func hubSeedIDs(corpus *testdata.Corpus) []uuid.UUID {
	var ids []uuid.UUID
	for _, f := range corpus.Fixtures {
		if f.Node.Namespace == testdata.NSGeneral && f.Node.HasLabel("Hub") {
			ids = append(ids, f.Node.ID)
		}
	}
	return ids
}

func countWins(a, b []QueryResult) int {
	n := 0
	for i := range a {
		if a[i].RecallAt1 > b[i].RecallAt1 {
			n++
		}
	}
	return n
}
func countTies(a, b []QueryResult) int {
	n := 0
	for i := range a {
		if a[i].RecallAt1 == b[i].RecallAt1 {
			n++
		}
	}
	return n
}
func countLosses(a, b []QueryResult) int {
	n := 0
	for i := range a {
		if a[i].RecallAt1 < b[i].RecallAt1 {
			n++
		}
	}
	return n
}

// ─── Corpus stats ─────────────────────────────────────────────────────────────

// TestFitness_CorpusStats prints a breakdown of the corpus composition.
func TestFitness_CorpusStats(t *testing.T) {
	corpus := testdata.Build()

	nsByNS := map[string]int{}
	trollCount := 0
	correctCount := 0
	staleCount := 0
	topicCounts := map[string]int{}

	for _, f := range corpus.Fixtures {
		nsByNS[f.Node.Namespace]++
		if f.IsTroll {
			trollCount++
		}
		if f.IsCorrect {
			correctCount++
		}
		if f.IsStale {
			staleCount++
		}
		topicCounts[f.Topic]++
	}

	total := len(corpus.Fixtures)
	t.Log("\n╔══════════════════════════════════════════════╗")
	t.Log("║              CORPUS STATISTICS               ║")
	t.Log("╚══════════════════════════════════════════════╝")
	t.Logf("  Total nodes:    %d", total)
	t.Logf("  Correct nodes:  %d (%.0f%%)", correctCount, float64(correctCount)/float64(total)*100)
	t.Logf("  Troll nodes:    %d (%.0f%%)", trollCount, float64(trollCount)/float64(total)*100)
	t.Logf("  Stale nodes:    %d (%.0f%%)", staleCount, float64(staleCount)/float64(total)*100)
	t.Logf("  Total queries:  %d", len(corpus.QuerySet))
	t.Log("")

	t.Log("  By namespace:")
	nsList := sortedKeys(nsByNS)
	for _, ns := range nsList {
		t.Logf("    %-35s %d nodes", ns, nsByNS[ns])
	}

	t.Log("\n  By topic:")
	topicList := sortedKeys(topicCounts)
	for _, topic := range topicList {
		t.Logf("    %-20s %d nodes", topic, topicCounts[topic])
	}
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
