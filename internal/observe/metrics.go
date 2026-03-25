// Package observe provides zero-dependency observability for contextdb.
//
// It ships three primitives:
//
//   - Counter    monotonically increasing int64
//   - Gauge      instantaneous float64 (set, add, sub)
//   - Histogram  latency buckets with count/sum/percentile estimates
//
// Metrics are registered in a global Registry and exposed via two
// endpoints when you call [Handler]:
//
//	GET /metrics          Prometheus text format (scrape this with Prometheus/Grafana)
//	GET /debug/vars       JSON (expvar-compatible, scrape this with Datadog agent)
//
// No external dependencies — everything is stdlib.
package observe

import (
	"expvar"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/pprof"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Registry ────────────────────────────────────────────────────────────────

// Registry holds all registered metrics.
type Registry struct {
	mu         sync.RWMutex
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
}

// NewRegistry returns an initialised empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
	}
}

// Default is the global registry. Use package-level functions to register
// metrics against it.
var Default = &Registry{
	counters:   make(map[string]*Counter),
	gauges:     make(map[string]*Gauge),
	histograms: make(map[string]*Histogram),
}

// NewCounter registers and returns a named counter. Panics if already registered.
func NewCounter(name, help string) *Counter {
	return Default.NewCounter(name, help)
}

// NewGauge registers and returns a named gauge. Panics if already registered.
func NewGauge(name, help string) *Gauge {
	return Default.NewGauge(name, help)
}

// NewHistogram registers and returns a histogram with the given microsecond buckets.
// Panics if already registered.
func NewHistogram(name, help string, bucketsUs []float64) *Histogram {
	return Default.NewHistogram(name, help, bucketsUs)
}

func (r *Registry) NewCounter(name, help string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.counters[name]; ok {
		panic("observe: counter already registered: " + name)
	}
	c := &Counter{name: name, help: help}
	r.counters[name] = c
	// Mirror into expvar for Datadog agent compatibility (skip if already published)
	if expvar.Get(name) == nil {
		expvar.Publish(name, expvar.Func(func() any { return c.Value() }))
	}
	return c
}

func (r *Registry) NewGauge(name, help string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.gauges[name]; ok {
		panic("observe: gauge already registered: " + name)
	}
	g := &Gauge{name: name, help: help}
	r.gauges[name] = g
	if expvar.Get(name) == nil {
		expvar.Publish(name, expvar.Func(func() any { return g.Value() }))
	}
	return g
}

func (r *Registry) NewHistogram(name, help string, bucketsUs []float64) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.histograms[name]; ok {
		panic("observe: histogram already registered: " + name)
	}
	sorted := make([]float64, len(bucketsUs))
	copy(sorted, bucketsUs)
	sort.Float64s(sorted)
	h := &Histogram{
		name:      name,
		help:      help,
		buckets:   sorted,
		counts:    make([]int64, len(sorted)+1), // +1 for +Inf bucket
	}
	r.histograms[name] = h
	return h
}

// ─── Counter ─────────────────────────────────────────────────────────────────

// Counter is a monotonically increasing int64.
type Counter struct {
	v    int64
	name string
	help string
}

// Inc increments by 1.
func (c *Counter) Inc() { atomic.AddInt64(&c.v, 1) }

// Add increments by n.
func (c *Counter) Add(n int64) { atomic.AddInt64(&c.v, n) }

// Value returns the current count.
func (c *Counter) Value() int64 { return atomic.LoadInt64(&c.v) }

// ─── Gauge ────────────────────────────────────────────────────────────────────

// Gauge holds an instantaneous float64 reading.
type Gauge struct {
	mu   sync.Mutex
	v    float64
	name string
	help string
}

// Set sets the gauge to v.
func (g *Gauge) Set(v float64) {
	g.mu.Lock()
	g.v = v
	g.mu.Unlock()
}

// Add adds delta to the gauge.
func (g *Gauge) Add(delta float64) {
	g.mu.Lock()
	g.v += delta
	g.mu.Unlock()
}

// Sub subtracts delta from the gauge.
func (g *Gauge) Sub(delta float64) { g.Add(-delta) }

// Value returns the current value.
func (g *Gauge) Value() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.v
}

// ─── Histogram ────────────────────────────────────────────────────────────────

// Histogram tracks latency distributions using fixed microsecond buckets.
type Histogram struct {
	mu      sync.Mutex
	sum     float64 // sum of all observed values (microseconds)
	total   int64   // total observation count
	counts  []int64 // per-bucket cumulative counts (+1 for +Inf)
	buckets []float64
	name    string
	help    string
}

// ObserveDuration records a duration as microseconds.
func (h *Histogram) ObserveDuration(d time.Duration) {
	h.Observe(float64(d.Microseconds()))
}

// Observe records a value in microseconds.
func (h *Histogram) Observe(us float64) {
	h.mu.Lock()
	h.sum += us
	h.total++
	for i, b := range h.buckets {
		if us <= b {
			h.counts[i]++
		}
	}
	// +Inf bucket always incremented
	h.counts[len(h.buckets)]++
	h.mu.Unlock()
}

// Snapshot returns a point-in-time copy of histogram data.
func (h *Histogram) Snapshot() HistogramSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	counts := make([]int64, len(h.counts))
	copy(counts, h.counts)
	return HistogramSnapshot{
		Buckets: h.buckets,
		Counts:  counts,
		Sum:     h.sum,
		Total:   h.total,
	}
}

// HistogramSnapshot is an immutable point-in-time histogram reading.
type HistogramSnapshot struct {
	Buckets []float64
	Counts  []int64
	Sum     float64
	Total   int64
}

// P returns an estimated percentile value (0–100) in microseconds.
// Uses linear interpolation between bucket boundaries.
func (s HistogramSnapshot) P(pct float64) float64 {
	if s.Total == 0 {
		return 0
	}
	target := int64(math.Ceil(pct / 100.0 * float64(s.Total)))
	for i, b := range s.Buckets {
		if s.Counts[i] >= target {
			return b
		}
	}
	return math.Inf(1)
}

// Mean returns the arithmetic mean in microseconds.
func (s HistogramSnapshot) Mean() float64 {
	if s.Total == 0 {
		return 0
	}
	return s.Sum / float64(s.Total)
}

// ─── Prometheus text exposition ──────────────────────────────────────────────

// WritePrometheusText writes all metrics in Prometheus text format to w.
// This is the format scraped by Prometheus, Datadog agent, and Grafana Agent.
func (r *Registry) WritePrometheusText(w io.Writer) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Go runtime metrics
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintf(w, "# HELP go_goroutines Number of active goroutines\n")
	fmt.Fprintf(w, "# TYPE go_goroutines gauge\n")
	fmt.Fprintf(w, "go_goroutines %d\n\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "# HELP go_heap_alloc_bytes Bytes allocated on the heap\n")
	fmt.Fprintf(w, "# TYPE go_heap_alloc_bytes gauge\n")
	fmt.Fprintf(w, "go_heap_alloc_bytes %d\n\n", ms.HeapAlloc)
	fmt.Fprintf(w, "# HELP go_gc_pause_ns_total Cumulative GC pause nanoseconds\n")
	fmt.Fprintf(w, "# TYPE go_gc_pause_ns_total counter\n")
	fmt.Fprintf(w, "go_gc_pause_ns_total %d\n\n", ms.PauseTotalNs)
	fmt.Fprintf(w, "# HELP go_gc_cycles_total Total GC cycles completed\n")
	fmt.Fprintf(w, "# TYPE go_gc_cycles_total counter\n")
	fmt.Fprintf(w, "go_gc_cycles_total %d\n\n", ms.NumGC)

	// Build info
	if bi, ok := debug.ReadBuildInfo(); ok {
		fmt.Fprintf(w, "# HELP contextdb_build_info Build information\n")
		fmt.Fprintf(w, "# TYPE contextdb_build_info gauge\n")
		fmt.Fprintf(w, "contextdb_build_info{go_version=%q,module=%q} 1\n\n",
			bi.GoVersion, bi.Main.Path)
	}

	// Counters
	names := sortedCounterKeys(r.counters)
	for _, name := range names {
		c := r.counters[name]
		fmt.Fprintf(w, "# HELP %s %s\n", name, c.help)
		fmt.Fprintf(w, "# TYPE %s counter\n", name)
		fmt.Fprintf(w, "%s %d\n\n", name, c.Value())
	}

	// Gauges
	gnames := sortedGaugeKeys(r.gauges)
	for _, name := range gnames {
		g := r.gauges[name]
		fmt.Fprintf(w, "# HELP %s %s\n", name, g.help)
		fmt.Fprintf(w, "# TYPE %s gauge\n", name)
		fmt.Fprintf(w, "%s %g\n\n", name, g.Value())
	}

	// Histograms
	hnames := sortedHistogramKeys(r.histograms)
	for _, name := range hnames {
		h := r.histograms[name]
		snap := h.Snapshot()
		fmt.Fprintf(w, "# HELP %s %s\n", name, h.help)
		fmt.Fprintf(w, "# TYPE %s histogram\n", name)
		for i, b := range snap.Buckets {
			fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", name, b, snap.Counts[i])
		}
		fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, snap.Total)
		fmt.Fprintf(w, "%s_sum %g\n", name, snap.Sum)
		fmt.Fprintf(w, "%s_count %d\n\n", name, snap.Total)
	}
}

// ─── HTTP handlers ────────────────────────────────────────────────────────────

// Handler returns an http.ServeMux with:
//
//	GET /metrics        Prometheus text exposition
//	GET /debug/vars     expvar JSON (Datadog agent compatible)
//	GET /debug/pprof/*  Go pprof endpoints
func Handler(r *Registry) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.WritePrometheusText(w)
	})

	// expvar handler (stdlib)
	mux.Handle("/debug/vars", expvar.Handler())

	// pprof endpoints
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// Health/readiness
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	return mux
}

// ─── Trace span (stdlib, no OTLP) ────────────────────────────────────────────

// Span measures the duration of a code block and records it to a histogram.
// Usage:
//
//	defer observe.StartSpan(histogram).End()
type Span struct {
	h     *Histogram
	start time.Time
	attrs map[string]string
}

// StartSpan begins a new span against the given histogram.
func StartSpan(h *Histogram) *Span {
	return &Span{h: h, start: time.Now()}
}

// End records the span duration to the histogram.
func (s *Span) End() {
	if s.h != nil {
		s.h.ObserveDuration(time.Since(s.start))
	}
}

// ─── Structured log fields for retrieval results ─────────────────────────────

// RetrievalLogFields returns slog-compatible key-value pairs for a retrieval
// operation, suitable for attaching to a slog.Info call.
func RetrievalLogFields(
	ns string,
	resultCount int,
	topScore float64,
	elapsed time.Duration,
	source string,
) []any {
	return []any{
		"namespace", ns,
		"result_count", resultCount,
		"top_score", fmt.Sprintf("%.4f", topScore),
		"elapsed_us", elapsed.Microseconds(),
		"source", source,
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func sortedCounterKeys(m map[string]*Counter) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedGaugeKeys(m map[string]*Gauge) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedHistogramKeys(m map[string]*Histogram) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// defaultLatencyBuckets returns microsecond bucket boundaries suitable for
// a low-latency in-process database. Covers 10µs → 10s.
func defaultLatencyBuckets() []float64 {
	return []float64{
		10, 25, 50, 100, 250, 500,
		1_000, 2_500, 5_000, 10_000, 25_000, 50_000,
		100_000, 250_000, 500_000, 1_000_000, 10_000_000,
	}
}

// DefaultLatencyBuckets returns the standard latency buckets used by
// all contextdb histograms.
var DefaultLatencyBuckets = defaultLatencyBuckets()

// Lowercase helper — Prometheus metric names must be lowercase with underscores.
func metricName(parts ...string) string {
	return strings.Join(parts, "_")
}

var _ = metricName // suppress unused warning
