package retrieval

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store/memory"
)

const gapTestNS = "test-gaps"

// makeVecNode creates a node with a vector and optional properties.
func makeVecNode(ns string, vec []float32, labels []string, props map[string]any) core.Node {
	now := time.Now()
	n := core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     labels,
		Properties: props,
		Vector:     vec,
		ValidFrom:  now,
		TxTime:     now,
		Confidence: 0.8,
	}
	return n
}

// normalize returns a unit vector.
func normalize(v []float32) []float32 {
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return v
	}
	norm = math.Sqrt(norm)
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out
}

// TestDetectGaps_TooFewNodes verifies that fewer than 2 nodes returns nil.
func TestDetectGaps_TooFewNodes(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()
	vi := memory.NewVectorIndex()

	d := NewGapDetector(g, vi)

	// Zero nodes
	gaps, err := d.DetectGaps(ctx, gapTestNS, GapQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gaps != nil {
		t.Errorf("expected nil gaps with 0 nodes, got %v", gaps)
	}

	// One node (no vector)
	n := makeVecNode(gapTestNS, nil, []string{"topic-a"}, nil)
	if err := g.UpsertNode(ctx, n); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	gaps, err = d.DetectGaps(ctx, gapTestNS, GapQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gaps != nil {
		t.Errorf("expected nil gaps with 1 node (no vector), got %v", gaps)
	}
}

// TestDetectGaps_TwoNodesNoVectors verifies nil result when nodes lack vectors.
func TestDetectGaps_TwoNodesNoVectors(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()
	vi := memory.NewVectorIndex()
	d := NewGapDetector(g, vi)

	for i := 0; i < 2; i++ {
		n := makeVecNode(gapTestNS, nil, nil, nil)
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	gaps, err := d.DetectGaps(ctx, gapTestNS, GapQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gaps != nil {
		t.Errorf("expected nil gaps when nodes have no vectors, got %v", gaps)
	}
}

// TestVectorMidpoint verifies that the midpoint of two orthogonal unit vectors
// is correctly normalized and lies equidistant from both.
func TestVectorMidpoint(t *testing.T) {
	// Two orthogonal 2D unit vectors: [1,0] and [0,1]
	a := []float32{1, 0}
	b := []float32{0, 1}
	mid := vectorMidpoint(a, b)

	// Midpoint before normalization is [0.5, 0.5]; after normalization it becomes
	// [1/sqrt(2), 1/sqrt(2)] ≈ [0.7071, 0.7071].
	want := float32(1.0 / math.Sqrt(2))
	const tol = 1e-5
	if math.Abs(float64(mid[0])-float64(want)) > tol {
		t.Errorf("mid[0] = %v, want ≈ %v", mid[0], want)
	}
	if math.Abs(float64(mid[1])-float64(want)) > tol {
		t.Errorf("mid[1] = %v, want ≈ %v", mid[1], want)
	}

	// Norm of result should be 1.
	var norm float64
	for _, v := range mid {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if math.Abs(norm-1.0) > tol {
		t.Errorf("midpoint norm = %v, want 1.0", norm)
	}
}

// TestVectorMidpoint_MismatchedLengths verifies that mismatched slices return a.
func TestVectorMidpoint_MismatchedLengths(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2}
	result := vectorMidpoint(a, b)
	if len(result) != len(a) {
		t.Errorf("expected len %d, got %d", len(a), len(result))
	}
}

// TestSortGapsByDensity verifies insertion-sort puts sparsest (lowest score) first.
func TestSortGapsByDensity(t *testing.T) {
	gaps := []KnowledgeGap{
		{ID: uuid.New(), DensityScore: 0.4},
		{ID: uuid.New(), DensityScore: 0.1},
		{ID: uuid.New(), DensityScore: 0.3},
		{ID: uuid.New(), DensityScore: 0.2},
	}

	sortGapsByDensity(gaps)

	for i := 1; i < len(gaps); i++ {
		if gaps[i].DensityScore < gaps[i-1].DensityScore {
			t.Errorf("gaps not sorted: gaps[%d].DensityScore=%v < gaps[%d].DensityScore=%v",
				i, gaps[i].DensityScore, i-1, gaps[i-1].DensityScore)
		}
	}

	if gaps[0].DensityScore != 0.1 {
		t.Errorf("expected sparsest gap first (0.1), got %v", gaps[0].DensityScore)
	}
	if gaps[len(gaps)-1].DensityScore != 0.4 {
		t.Errorf("expected densest gap last (0.4), got %v", gaps[len(gaps)-1].DensityScore)
	}
}

// TestSortGapsByDensity_Empty verifies that sorting an empty slice does not panic.
func TestSortGapsByDensity_Empty(t *testing.T) {
	sortGapsByDensity(nil)
	sortGapsByDensity([]KnowledgeGap{})
}

// TestBuildGapReport_NoGaps verifies coverage is 1.0 when there are no gaps.
func TestBuildGapReport_NoGaps(t *testing.T) {
	report := BuildGapReport("ns", nil, 50)
	if report.CoverageScore != 1.0 {
		t.Errorf("expected coverage 1.0 with no gaps, got %v", report.CoverageScore)
	}
	if report.GapsDetected != 0 {
		t.Errorf("expected 0 gaps detected, got %d", report.GapsDetected)
	}
	if report.TotalNodes != 50 {
		t.Errorf("expected TotalNodes 50, got %d", report.TotalNodes)
	}
	if report.Namespace != "ns" {
		t.Errorf("expected namespace 'ns', got %q", report.Namespace)
	}
}

// TestBuildGapReport_SomeGaps verifies coverage < 1.0 when gaps are present.
func TestBuildGapReport_SomeGaps(t *testing.T) {
	gaps := []KnowledgeGap{
		{ID: uuid.New(), DensityScore: 0.1},
		{ID: uuid.New(), DensityScore: 0.2},
		{ID: uuid.New(), DensityScore: 0.3},
	}
	// 3 gaps / 10 nodes = 0.3 gap ratio → coverage = 0.7
	report := BuildGapReport("ns", gaps, 10)
	if report.CoverageScore >= 1.0 {
		t.Errorf("expected coverage < 1.0 with gaps, got %v", report.CoverageScore)
	}
	if report.GapsDetected != 3 {
		t.Errorf("expected 3 gaps detected, got %d", report.GapsDetected)
	}
	const want = 0.7
	const tol = 1e-9
	if math.Abs(report.CoverageScore-want) > tol {
		t.Errorf("expected coverage %v, got %v", want, report.CoverageScore)
	}
}

// TestBuildGapReport_ZeroNodes verifies coverage is 0 when there are no nodes.
func TestBuildGapReport_ZeroNodes(t *testing.T) {
	gaps := []KnowledgeGap{{ID: uuid.New()}}
	report := BuildGapReport("ns", gaps, 0)
	if report.CoverageScore != 0 {
		t.Errorf("expected coverage 0 with 0 total nodes, got %v", report.CoverageScore)
	}
}

// TestBuildGapReport_CoverageFloorAtZero verifies coverage never goes below 0.
func TestBuildGapReport_CoverageFloorAtZero(t *testing.T) {
	// 20 gaps / 5 nodes → ratio > 1 → coverage clamped to 0
	gaps := make([]KnowledgeGap, 20)
	for i := range gaps {
		gaps[i] = KnowledgeGap{ID: uuid.New()}
	}
	report := BuildGapReport("ns", gaps, 5)
	if report.CoverageScore < 0 {
		t.Errorf("coverage went below 0: %v", report.CoverageScore)
	}
}

// TestNodeTopicText verifies the priority: "text" prop > "content" prop > first label.
func TestNodeTopicText(t *testing.T) {
	cases := []struct {
		name string
		node core.Node
		want string
	}{
		{
			name: "text property",
			node: core.Node{Properties: map[string]any{"text": "hello"}, Labels: []string{"lbl"}},
			want: "hello",
		},
		{
			name: "content property",
			node: core.Node{Properties: map[string]any{"content": "world"}, Labels: []string{"lbl"}},
			want: "world",
		},
		{
			name: "first label",
			node: core.Node{Labels: []string{"topic-x", "topic-y"}},
			want: "topic-x",
		},
		{
			name: "empty",
			node: core.Node{},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nodeTopicText(tc.node)
			if got != tc.want {
				t.Errorf("nodeTopicText = %q, want %q", got, tc.want)
			}
		})
	}
}
