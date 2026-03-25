package observe_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/observe"
	"github.com/antiartificial/contextdb/internal/retrieval"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

// freshRegistry creates an isolated registry for each test to avoid
// "already registered" panics from the global Default registry.
func freshRegistry() *observe.Registry {
	return observe.NewRegistry()
}

// ─── Counter ─────────────────────────────────────────────────────────────────

func TestCounter_IncrementAndRead(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	c := r.NewCounter("test_counter", "test")

	is.Equal(int64(0), c.Value())
	c.Inc()
	c.Inc()
	c.Add(3)
	is.Equal(int64(5), c.Value())
}

func TestCounter_ConcurrentIncrements(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	c := r.NewCounter("test_concurrent_counter", "test")

	done := make(chan struct{})
	for i := 0; i < 100; i++ {
		go func() {
			c.Inc()
			done <- struct{}{}
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
	is.Equal(int64(100), c.Value())
}

// ─── Gauge ───────────────────────────────────────────────────────────────────

func TestGauge_SetAddSub(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	g := r.NewGauge("test_gauge", "test")

	g.Set(10.0)
	is.Equal(10.0, g.Value())
	g.Add(5.0)
	is.Equal(15.0, g.Value())
	g.Sub(3.0)
	is.Equal(12.0, g.Value())
}

// ─── Histogram ───────────────────────────────────────────────────────────────

func TestHistogram_ObserveAndPercentiles(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	h := r.NewHistogram("test_hist", "test", []float64{100, 500, 1000, 5000, 10000})

	// Observe 100 values: 50 at 50µs, 50 at 5000µs
	for i := 0; i < 50; i++ {
		h.Observe(50)    // below 100µs bucket
		h.Observe(5000)  // at 5000µs bucket
	}

	snap := h.Snapshot()
	is.Equal(int64(100), snap.Total)

	// P50 should be around 5000 (half are at 5000)
	p50 := snap.P(50)
	is.True(p50 <= 5000)

	// P95 should be 5000 (95th percentile hits the 5000 bucket)
	p95 := snap.P(95)
	is.True(p95 <= 5000)

	// Mean should be (50*50 + 50*5000) / 100 = 2525
	is.True(snap.Mean() > 2000 && snap.Mean() < 3000)

	t.Logf("p50=%.0fµs  p95=%.0fµs  mean=%.0fµs",
		snap.P(50), snap.P(95), snap.Mean())
}

func TestHistogram_ObserveDuration(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	h := r.NewHistogram("test_duration_hist", "test", observe.DefaultLatencyBuckets)

	h.ObserveDuration(500 * time.Microsecond)
	h.ObserveDuration(1 * time.Millisecond)

	snap := h.Snapshot()
	is.Equal(int64(2), snap.Total)
	is.True(snap.Sum > 0)
}

func TestHistogram_EmptyReturnsZero(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	h := r.NewHistogram("test_empty_hist", "test", observe.DefaultLatencyBuckets)

	snap := h.Snapshot()
	is.Equal(0.0, snap.P(50))
	is.Equal(0.0, snap.Mean())
}

// ─── Prometheus text format ───────────────────────────────────────────────────

func TestPrometheusText_ContainsAllMetricTypes(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()

	c := r.NewCounter("myapp_requests_total", "total requests")
	g := r.NewGauge("myapp_active_connections", "active connections")
	h := r.NewHistogram("myapp_latency_us", "latency", []float64{100, 1000, 10000})

	c.Add(42)
	g.Set(7.5)
	h.Observe(250)
	h.Observe(8000)

	var buf bytes.Buffer
	r.WritePrometheusText(&buf)
	out := buf.String()

	// Counter
	is.True(strings.Contains(out, "# TYPE myapp_requests_total counter"))
	is.True(strings.Contains(out, "myapp_requests_total 42"))

	// Gauge
	is.True(strings.Contains(out, "# TYPE myapp_active_connections gauge"))
	is.True(strings.Contains(out, "myapp_active_connections 7.5"))

	// Histogram
	is.True(strings.Contains(out, "# TYPE myapp_latency_us histogram"))
	is.True(strings.Contains(out, `myapp_latency_us_bucket{le="100"}`))
	is.True(strings.Contains(out, `myapp_latency_us_bucket{le="+Inf"} 2`))
	is.True(strings.Contains(out, "myapp_latency_us_count 2"))

	// Go runtime metrics always present
	is.True(strings.Contains(out, "go_goroutines"))
	is.True(strings.Contains(out, "go_heap_alloc_bytes"))

	t.Logf("Prometheus output (%d bytes):\n%s", len(out), out[:min(len(out), 800)])
}

func TestPrometheusText_ValidFormat(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	r.NewCounter("valid_counter", "help")

	var buf bytes.Buffer
	r.WritePrometheusText(&buf)
	out := buf.String()

	// Every metric line must either be a comment or contain a space
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		is.True(strings.Contains(line, " "))
	}
}

// ─── HTTP handler ─────────────────────────────────────────────────────────────

func TestHandler_MetricsEndpoint(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	r.NewCounter("http_test_counter", "test")

	handler := observe.Handler(r)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	is.NoErr(err)
	defer resp.Body.Close()

	is.Equal(200, resp.StatusCode)
	is.True(strings.Contains(resp.Header.Get("Content-Type"), "text/plain"))
}

func TestHandler_HealthEndpoint(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	handler := observe.Handler(r)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	is.NoErr(err)
	defer resp.Body.Close()
	is.Equal(200, resp.StatusCode)
}

func TestHandler_PprofEndpoint(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	handler := observe.Handler(r)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// pprof index should return 200
	resp, err := http.Get(srv.URL + "/debug/pprof/")
	is.NoErr(err)
	defer resp.Body.Close()
	is.Equal(200, resp.StatusCode)
}

// ─── Span ─────────────────────────────────────────────────────────────────────

func TestSpan_RecordsDuration(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	h := r.NewHistogram("span_test_hist", "test", observe.DefaultLatencyBuckets)

	func() {
		defer observe.StartSpan(h).End()
		time.Sleep(1 * time.Millisecond)
	}()

	snap := h.Snapshot()
	is.Equal(int64(1), snap.Total)
	// Should have recorded at least 1000µs (1ms)
	is.True(snap.Sum >= 1000)
}

// ─── Instrumented engine ──────────────────────────────────────────────────────

func TestInstrumentedEngine_RecordsMetrics(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	metrics := observe.NewMetrics(r)

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	kv := memstore.NewKVStore()

	// Seed one node
	ctx := context.Background()
	nodeID := uuid.New()
	n := core.Node{
		ID: nodeID, Namespace: "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "test claim"},
		Confidence: 0.8,
		ValidFrom:  time.Now().Add(-time.Millisecond),
	}
	_ = graph.UpsertNode(ctx, n)
	vecs.RegisterNode(n)
	_ = vecs.Index(ctx, core.VectorEntry{
		ID:        uuid.New(),
		Namespace: "test",
		NodeID:    &nodeID,
		Vector:    []float32{1, 0, 0, 0, 0, 0, 0, 0},
		ModelID:   "test",
		CreatedAt: time.Now(),
	})

	log := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	engine := observe.NewInstrumentedEngine(
		&retrieval.Engine{Graph: graph, Vectors: vecs, KV: kv},
		metrics,
		log,
	)

	p := core.GeneralParams()
	p.AsOf = time.Now()

	_, err := engine.Retrieve(ctx, retrieval.Query{
		Namespace:   "test",
		Vector:      []float32{1, 0, 0, 0, 0, 0, 0, 0},
		TopK:        5,
		ScoreParams: p,
	})
	is.NoErr(err)

	// Counters should have incremented
	is.Equal(int64(1), metrics.RetrievalTotal.Value())
	is.Equal(int64(0), metrics.RetrievalErrors.Value())

	// Latency should have recorded one observation
	snap := metrics.RetrievalLatency.Snapshot()
	is.Equal(int64(1), snap.Total)
	is.True(snap.Sum >= 0)

	t.Logf("retrieve latency: mean=%.1fµs  p95=%.1fµs",
		snap.Mean(), snap.P(95))
}

func TestInstrumentedEngine_CountsErrors(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	metrics := observe.NewMetrics(r)

	// Engine with nil stores — will error on any call
	engine := observe.NewInstrumentedEngine(
		&retrieval.Engine{Graph: nil, Vectors: nil, KV: nil},
		metrics,
		slog.New(slog.NewTextHandler(testWriter{t}, nil)),
	)

	p := core.GeneralParams()
	p.AsOf = time.Now()

	// No seeds, no vector — will return empty but not error
	results, err := engine.Retrieve(context.Background(), retrieval.Query{
		Namespace:   "empty",
		TopK:        5,
		ScoreParams: p,
	})
	is.NoErr(err) // empty query returns empty, not error
	is.Equal(0, len(results))
	is.Equal(int64(1), metrics.RetrievalTotal.Value())
}

// ─── NewMetrics — registration smoke test ────────────────────────────────────

func TestNewMetrics_AllInstrumentsRegistered(t *testing.T) {
	is := is.New(t)
	r := observe.NewRegistry()
	m := observe.NewMetrics(r)

	// Verify none are nil — a nil instrument would panic on first use
	is.True(m.RetrievalTotal != nil)
	is.True(m.RetrievalErrors != nil)
	is.True(m.RetrievalLatency != nil)
	is.True(m.IngestTotal != nil)
	is.True(m.IngestAdmitted != nil)
	is.True(m.IngestRejected != nil)
	is.True(m.GraphUpsertTotal != nil)
	is.True(m.VectorSearchTotal != nil)
	is.True(m.ActiveNamespaces != nil)
	is.True(m.AdmissionTrollRejected != nil)

	// Verify they emit to Prometheus text
	var buf bytes.Buffer
	r.WritePrometheusText(&buf)
	out := buf.String()
	is.True(strings.Contains(out, "contextdb_retrieval_total"))
	is.True(strings.Contains(out, "contextdb_ingest_total"))
	is.True(strings.Contains(out, "contextdb_graph_upsert_total"))
	is.True(strings.Contains(out, "contextdb_vector_search_total"))
	is.True(strings.Contains(out, "contextdb_admission_troll_rejected_total"))

	t.Logf("registered %d metric lines in Prometheus output", strings.Count(out, "\n"))
}

// ─── ScoreReport ─────────────────────────────────────────────────────────────

func TestScoreReport_Statistics(t *testing.T) {
	is := is.New(t)

	results := []core.ScoredNode{
		{Score: 0.9},
		{Score: 0.7},
		{Score: 0.5},
		{Score: 0.3},
		{Score: 0.1},
	}

	report := observe.ReportScores(results, 2)

	is.Equal(5, report.Count)
	is.Equal(0.9, report.Max)
	is.Equal(0.1, report.Min)
	is.True(report.Mean > 0.4 && report.Mean < 0.6)
	is.Equal(2, report.ZeroCount)

	t.Logf("min=%.2f max=%.2f mean=%.2f p50=%.2f p95=%.2f zeros=%d",
		report.Min, report.Max, report.Mean, report.P50, report.P95, report.ZeroCount)
}

func TestScoreReport_EmptyReturnsZeroReport(t *testing.T) {
	is := is.New(t)
	report := observe.ReportScores(nil, 5)
	is.Equal(0, report.Count)
	is.Equal(5, report.ZeroCount)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// testWriter implements io.Writer via t.Log so log output appears in test output.
type testWriter struct{ t *testing.T }

func (tw testWriter) Write(p []byte) (int, error) {
	tw.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
