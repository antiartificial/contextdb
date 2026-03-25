package bench_test

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
)

// ─── ASCII chart helpers ──────────────────────────────────────────────────────

// bar renders a horizontal bar of width proportional to v (0.0–1.0).
func bar(v float64, width int) string {
	filled := int(math.Round(v * float64(width)))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// printTable renders a labelled ASCII bar chart to t.Log.
func printTable(t *testing.T, title string, rows [][]string) {
	t.Helper()
	t.Logf("\n%s", title)
	t.Log(strings.Repeat("─", 72))
	for _, row := range rows {
		t.Log(row[0])
	}
	t.Log(strings.Repeat("─", 72))
}

// ─── Score landscape helpers ──────────────────────────────────────────────────

type scorePoint struct {
	label      string
	similarity float64
	confidence float64
	recency    float64 // 0–1 pre-computed
	utility    float64
	score      float64
}

func evalPoints(params core.ScoreParams, points []scorePoint) []scorePoint {
	params.AsOf = time.Now()
	for i, p := range points {
		// Build a node that will produce the desired recency score:
		// recency = exp(-alpha * age_hours), so age = -ln(recency)/alpha
		alpha := params.DecayAlpha
		if alpha == 0 {
			alpha = 0.05
		}
		var ageHours float64
		if p.recency > 0 && p.recency < 1 {
			ageHours = -math.Log(p.recency) / alpha
		}
		created := params.AsOf.Add(-time.Duration(ageHours*float64(time.Hour)))
		n := core.Node{
			Namespace:  "bench",
			Confidence: p.confidence,
			ValidFrom:  created,
		}
		sn := core.ScoreNode(n, p.similarity, p.utility, params)
		points[i].score = sn.Score
	}
	return points
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestBench_ScoreLandscape_BeliefVsAgent prints a side-by-side comparison
// of how the same candidates score under BeliefSystem vs AgentMemory presets.
func TestBench_ScoreLandscape_BeliefVsAgent(t *testing.T) {
	candidates := []scorePoint{
		{label: "trusted, fresh, relevant", similarity: 0.85, confidence: 0.95, recency: 0.95, utility: 0.90},
		{label: "trusted, stale, relevant", similarity: 0.85, confidence: 0.95, recency: 0.20, utility: 0.90},
		{label: "unknown, fresh, relevant", similarity: 0.85, confidence: 0.50, recency: 0.95, utility: 0.50},
		{label: "troll, fresh, high sim  ", similarity: 0.95, confidence: 0.05, recency: 0.95, utility: 0.10},
		{label: "trusted, fresh, low sim ", similarity: 0.30, confidence: 0.95, recency: 0.95, utility: 0.90},
		{label: "high utility, low sim   ", similarity: 0.30, confidence: 0.50, recency: 0.80, utility: 0.95},
		{label: "stale procedural skill  ", similarity: 0.75, confidence: 0.90, recency: 0.10, utility: 0.85},
	}

	bsParams := core.BeliefSystemParams()
	amParams := core.AgentMemoryParams()

	bsScored := evalPoints(bsParams, append([]scorePoint(nil), candidates...))
	amScored := evalPoints(amParams, append([]scorePoint(nil), candidates...))

	barW := 30

	t.Log("\n╔══════════════════════════════════════════════════════════════════════╗")
	t.Log("║           SCORE LANDSCAPE — BeliefSystem vs AgentMemory             ║")
	t.Log("╚══════════════════════════════════════════════════════════════════════╝")
	t.Logf("%-28s  %-s %-6s  %-s %-6s", "candidate", "BeliefSystem", "score", "AgentMemory", "score")
	t.Log(strings.Repeat("─", 90))

	for i, c := range candidates {
		bs := bsScored[i].score
		am := amScored[i].score
		t.Logf("%-28s  %s %.4f  %s %.4f",
			c.label,
			bar(bs, barW), bs,
			bar(am, barW), am,
		)
	}
	t.Log(strings.Repeat("─", 90))
}

// TestBench_RecencyDecayCurves plots the decay curves for all memory types
// over a 120-hour window.
func TestBench_RecencyDecayCurves(t *testing.T) {
	types := []struct {
		name      string
		memType   core.MemoryType
	}{
		{"episodic  ", core.MemoryEpisodic},
		{"semantic  ", core.MemorySemantic},
		{"procedural", core.MemoryProcedural},
		{"working   ", core.MemoryWorking},
	}

	hours := []float64{0, 6, 12, 24, 48, 72, 120}
	barW := 30

	t.Log("\n╔══════════════════════════════════════════════════════════════════╗")
	t.Log("║              RECENCY DECAY CURVES BY MEMORY TYPE                ║")
	t.Log("╚══════════════════════════════════════════════════════════════════╝")
	t.Logf("%-12s  %s", "type\\hour→", formatHourHeader(hours))
	t.Log(strings.Repeat("─", 70))

	for _, mt := range types {
		alpha := core.DecayAlpha(mt.memType)
		var cells []string
		for _, h := range hours {
			v := math.Exp(-alpha * h)
			cells = append(cells, fmt.Sprintf("%.2f", v))
		}
		t.Logf("%-12s  %s", mt.name, strings.Join(cells, "  "))
	}

	// Visual bar for a fixed query point (48h old)
	t.Log("\n48-hour recency score per type:")
	t.Log(strings.Repeat("─", 50))
	for _, mt := range types {
		alpha := core.DecayAlpha(mt.memType)
		v := math.Exp(-alpha * 48)
		t.Logf("  %-12s %s %.4f", mt.name, bar(v, barW), v)
	}
}

// TestBench_CredibilitySweep sweeps source credibility [0→1] and shows
// how the BeliefSystem preset scores a claim with fixed similarity.
func TestBench_CredibilitySweep(t *testing.T) {
	params := core.BeliefSystemParams()
	params.AsOf = time.Now()
	barW := 40

	sim := 0.75 // fixed similarity for all points

	t.Log("\n╔══════════════════════════════════════════════════════════════════╗")
	t.Log("║       CREDIBILITY SWEEP — BeliefSystem preset, sim=0.75         ║")
	t.Log("╚══════════════════════════════════════════════════════════════════╝")
	t.Log(strings.Repeat("─", 65))

	for cred := 0.0; cred <= 1.01; cred += 0.1 {
		if cred > 1.0 {
			cred = 1.0
		}
		n := core.Node{
			Namespace:  "bench",
			Confidence: cred,
			ValidFrom:  time.Now(),
		}
		sn := core.ScoreNode(n, sim, 1.0, params)
		t.Logf("  cred=%.1f  %s  %.4f", cred, bar(sn.Score, barW), sn.Score)
	}
}

// TestBench_WeightSensitivity shows how varying a single weight while
// holding others constant shifts the score distribution.
func TestBench_WeightSensitivity(t *testing.T) {
	asOf := time.Now()
	barW := 35

	// Fixed candidate: moderate on all dimensions
	n := core.Node{
		Namespace:  "bench",
		Confidence: 0.7,
		ValidFrom:  asOf.Add(-24 * time.Hour),
	}
	sim := 0.7
	util := 0.7

	t.Log("\n╔══════════════════════════════════════════════════════════════════════╗")
	t.Log("║          WEIGHT SENSITIVITY — varying SimilarityWeight              ║")
	t.Log("╚══════════════════════════════════════════════════════════════════════╝")
	t.Log("  (confidence=0.7, similarity=0.7, recency≈24h, utility=0.7)")
	t.Log(strings.Repeat("─", 65))

	for sw := 0.0; sw <= 1.01; sw += 0.1 {
		if sw > 1.0 {
			sw = 1.0
		}
		remaining := 1.0 - sw
		p := core.ScoreParams{
			SimilarityWeight: sw,
			ConfidenceWeight: remaining * 0.5,
			RecencyWeight:    remaining * 0.3,
			UtilityWeight:    remaining * 0.2,
			DecayAlpha:       0.05,
			AsOf:             asOf,
		}
		sn := core.ScoreNode(n, sim, util, p)
		t.Logf("  simW=%.1f  %s  %.4f", sw, bar(sn.Score, barW), sn.Score)
	}
}

// TestBench_WriteHTMLReport writes an HTML visualisation of the score
// landscape to /tmp/contextdb_bench.html. Open in a browser for a
// richer view than the ASCII output.
func TestBench_WriteHTMLReport(t *testing.T) {
	candidates := []struct {
		label      string
		similarity float64
		confidence float64
		ageHours   float64
		utility    float64
	}{
		{"trusted, fresh, high-sim", 0.85, 0.95, 1, 0.90},
		{"trusted, stale, high-sim", 0.85, 0.95, 72, 0.90},
		{"unknown, fresh, high-sim", 0.85, 0.50, 1, 0.50},
		{"troll, fresh, highest-sim", 0.97, 0.05, 1, 0.10},
		{"trusted, fresh, low-sim", 0.30, 0.95, 1, 0.90},
		{"high-utility, low-sim", 0.30, 0.50, 3, 0.95},
		{"stale skill, good-sim", 0.75, 0.90, 96, 0.85},
	}

	presets := []struct {
		name   string
		params core.ScoreParams
	}{
		{"BeliefSystem", core.BeliefSystemParams()},
		{"AgentMemory", core.AgentMemoryParams()},
		{"General", core.GeneralParams()},
		{"Procedural", core.ProceduralParams()},
	}

	asOf := time.Now()

	// Build score matrix
	type row struct {
		label  string
		scores [4]float64
	}
	rows := make([]row, len(candidates))
	for i, c := range candidates {
		rows[i].label = c.label
		for j, preset := range presets {
			p := preset.params
			p.AsOf = asOf
			n := core.Node{
				Namespace:  "bench",
				Confidence: c.confidence,
				ValidFrom:  asOf.Add(-time.Duration(c.ageHours * float64(time.Hour))),
			}
			sn := core.ScoreNode(n, c.similarity, c.utility, p)
			rows[i].scores[j] = sn.Score
		}
	}

	// Write HTML
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>ContextDB Score Landscape</title>
<style>
  body{font-family:system-ui,sans-serif;margin:2rem;background:#fafafa;color:#1a1a1a}
  h1{font-size:1.4rem;font-weight:600;margin-bottom:1.5rem}
  table{border-collapse:collapse;width:100%;max-width:900px}
  th,td{padding:.5rem .75rem;text-align:left;border:1px solid #ddd;font-size:.85rem}
  th{background:#f0f0f0;font-weight:600}
  .bar-cell{min-width:140px}
  .bar-wrap{display:flex;align-items:center;gap:.5rem}
  .bar{height:14px;border-radius:3px;min-width:2px}
  .score{font-variant-numeric:tabular-nums;min-width:4rem;text-align:right;font-size:.8rem;color:#555}
  .preset-0{background:#4e79a7}
  .preset-1{background:#f28e2b}
  .preset-2{background:#59a14f}
  .preset-3{background:#e15759}
  .legend{display:flex;gap:1rem;margin-bottom:1rem;font-size:.85rem}
  .dot{width:12px;height:12px;border-radius:50%;display:inline-block;margin-right:.3rem}
  .note{font-size:.8rem;color:#666;margin-top:1.5rem}
</style>
</head>
<body>
<h1>ContextDB — Score Landscape by Preset</h1>
<div class="legend">
`)
	for i, p := range presets {
		sb.WriteString(fmt.Sprintf(`  <span><span class="dot preset-%d"></span>%s</span>`, i, p.name))
	}
	sb.WriteString(`</div>
<table>
<thead><tr><th>Candidate</th>`)
	for _, p := range presets {
		sb.WriteString("<th class=\"bar-cell\">" + p.name + "</th>")
	}
	sb.WriteString("</tr></thead>\n<tbody>\n")

	for _, r := range rows {
		sb.WriteString("<tr><td>" + r.label + "</td>")
		for j, score := range r.scores {
			pct := int(score * 200) // max bar width 200px
			sb.WriteString(fmt.Sprintf(
				`<td class="bar-cell"><div class="bar-wrap"><div class="bar preset-%d" style="width:%dpx"></div><span class="score">%.4f</span></div></td>`,
				j, pct, score,
			))
		}
		sb.WriteString("</tr>\n")
	}

	sb.WriteString(`</tbody></table>
<p class="note">
  Each bar shows the composite score for a candidate under the named preset.<br>
  Longer bar = more likely to be retrieved. All scores are in [0, 1].
</p>
</body></html>
`)

	path := "/tmp/contextdb_bench.html"
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Logf("could not write HTML report: %v", err)
	} else {
		t.Logf("HTML report written to %s", path)
	}
}

func formatHourHeader(hours []float64) string {
	parts := make([]string, len(hours))
	for i, h := range hours {
		parts[i] = fmt.Sprintf("%5.0fh", h)
	}
	return strings.Join(parts, "  ")
}
