package ingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
	badgerstore "github.com/antiartificial/contextdb/internal/store/badger"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

type failingVector struct {
	store.VectorIndex
	fail bool
}

func (v *failingVector) Index(ctx context.Context, entry core.VectorEntry) error {
	if v.fail {
		return errors.New("injected vector failure")
	}
	return v.VectorIndex.Index(ctx, entry)
}

type failingGraph struct {
	store.GraphStore
	failOnEdge int
	edgeCalls  int
}

type failNodeGraph struct {
	store.GraphStore
	fail bool
}

func (g *failNodeGraph) UpsertNode(ctx context.Context, n core.Node) error {
	if g.fail {
		return errors.New("injected node failure")
	}
	return g.GraphStore.UpsertNode(ctx, n)
}

type failFeedbackLog struct {
	store.EventLog
	fail bool
}

func (l *failFeedbackLog) Append(ctx context.Context, e store.Event) error {
	if l.fail && e.Type == store.EventFeedback {
		return errors.New("injected feedback event failure")
	}
	return l.EventLog.Append(ctx, e)
}

func (g *failingGraph) UpsertEdge(ctx context.Context, edge core.Edge) error {
	g.edgeCalls++
	if g.failOnEdge == g.edgeCalls {
		return errors.New("injected edge failure")
	}
	return g.GraphStore.UpsertEdge(ctx, edge)
}

func TestRecoverGraphSuccessVectorFailure(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	log := memstore.NewEventLog()
	node := recoveryNode("test")
	entry := recoveryVector(node)

	err := Persist(ctx, graph, &failingVector{VectorIndex: vecs, fail: true}, log, WritePlan{ID: uuid.New(), Node: &node, Vector: &entry})
	is.True(err != nil)
	var pending *RecoveryError
	is.True(errors.As(err, &pending))

	saved, err := graph.GetNode(ctx, node.Namespace, node.ID)
	is.NoErr(err)
	is.True(saved != nil)

	// A compactor may mark the intent processed. Recovery deliberately reads
	// SinceAll and must still find it.
	events, err := log.SinceAll(ctx, node.Namespace, time.Time{})
	is.NoErr(err)
	for _, event := range events {
		if event.Type == store.EventWriteIntent {
			is.NoErr(log.MarkProcessed(ctx, event.ID))
		}
	}
	is.NoErr(Recover(ctx, graph, vecs, log, node.Namespace))

	history, err := graph.History(ctx, node.Namespace, node.ID)
	is.NoErr(err)
	is.Equal(len(history), 1) // replay did not create another version
	results, err := vecs.Search(ctx, store.VectorQuery{Namespace: node.Namespace, Vector: entry.Vector, TopK: 1})
	is.NoErr(err)
	is.Equal(len(results), 1)
}

func TestRecoverPartialEdgeFailure(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	base := memstore.NewGraphStore()
	graph := &failingGraph{GraphStore: base, failOnEdge: 2}
	vecs := memstore.NewVectorIndex()
	log := memstore.NewEventLog()
	first, second := uuid.New(), uuid.New()
	plan := WritePlan{ID: uuid.New(), Edges: []core.Edge{
		{ID: uuid.New(), Namespace: "test", Src: first, Dst: second, Type: "first", Weight: 1, ValidFrom: time.Now(), TxTime: time.Now()},
		{ID: uuid.New(), Namespace: "test", Src: first, Dst: second, Type: "second", Weight: 1, ValidFrom: time.Now(), TxTime: time.Now()},
	}}
	is.True(Persist(ctx, graph, vecs, log, plan) != nil)

	graph.failOnEdge = 0
	is.NoErr(Recover(ctx, graph, vecs, log, "test"))
	edges, err := base.EdgesFrom(ctx, "test", first, nil)
	is.NoErr(err)
	is.Equal(len(edges), 2)
}

func TestRecoverSurvivesBadgerRestart(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	path := t.TempDir()
	db, err := badgerstore.Open(path)
	is.NoErr(err)
	node := recoveryNode("restart")
	entry := recoveryVector(node)
	graph := badgerstore.NewGraphStore(db.Inner())
	vecs := badgerstore.NewVectorIndex(db.Inner(), badgerstore.HNSWConfig{})
	log := badgerstore.NewEventLog(db.Inner())
	is.True(Persist(ctx, graph, &failingVector{VectorIndex: vecs, fail: true}, log, WritePlan{ID: uuid.New(), Node: &node, Vector: &entry}) != nil)
	is.NoErr(db.Close())

	db, err = badgerstore.Open(path)
	is.NoErr(err)
	defer db.Close()
	graph = badgerstore.NewGraphStore(db.Inner())
	vecs = badgerstore.NewVectorIndex(db.Inner(), badgerstore.HNSWConfig{})
	log = badgerstore.NewEventLog(db.Inner())
	is.NoErr(Recover(ctx, graph, vecs, log, "restart"))
	saved, err := graph.GetNode(ctx, "restart", node.ID)
	is.NoErr(err)
	is.True(saved != nil)
}

func TestRecoverFeedbackSourceSuccessNodeFailure(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	base := memstore.NewGraphStore()
	log := memstore.NewEventLog()
	source := core.DefaultSource("feedback", "source")
	source.BayesianUpdate(true)
	node := recoveryNode("feedback")
	plan := FeedbackPlan{ID: uuid.New(), Source: &source, Node: node, Event: testFeedbackEvent("feedback")}
	g := &failNodeGraph{GraphStore: base, fail: true}
	is.True(PersistFeedback(ctx, g, log, plan) != nil)
	g.fail = false
	is.NoErr(RecoverFeedback(ctx, base, log, "feedback"))
	saved, err := base.GetNode(ctx, "feedback", node.ID)
	is.NoErr(err)
	is.True(saved != nil)
	savedSource, err := base.GetSourceByExternalID(ctx, "feedback", "source")
	is.NoErr(err)
	is.Equal(savedSource.Alpha, source.Alpha)
}

func TestRecoverFeedbackNodeSuccessEventFailure(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	baseLog := memstore.NewEventLog()
	log := &failFeedbackLog{EventLog: baseLog, fail: true}
	node := recoveryNode("feedback-event")
	plan := FeedbackPlan{ID: uuid.New(), Node: node, Event: testFeedbackEvent("feedback-event")}
	is.True(PersistFeedback(ctx, graph, log, plan) != nil)
	log.fail = false
	is.NoErr(RecoverFeedback(ctx, graph, baseLog, "feedback-event"))
	events, err := baseLog.SinceAll(ctx, "feedback-event", time.Time{})
	is.NoErr(err)
	count := 0
	for _, e := range events {
		if e.Type == store.EventFeedback {
			count++
		}
	}
	is.Equal(count, 1)
}

func recoveryNode(namespace string) core.Node {
	return core.Node{ID: uuid.New(), Namespace: namespace, Labels: []string{"claim"}, Properties: map[string]any{"text": "recover me"}, Vector: []float32{1, 0}, Confidence: 1, ValidFrom: time.Now(), TxTime: time.Now()}
}

func recoveryVector(node core.Node) core.VectorEntry {
	return core.VectorEntry{ID: uuid.New(), Namespace: node.Namespace, NodeID: &node.ID, Vector: node.Vector, Text: "recover me", CreatedAt: time.Now()}
}

func testFeedbackEvent(namespace string) store.Event {
	return store.Event{ID: uuid.New(), Namespace: namespace, Type: store.EventFeedback, Payload: []byte(`{"action":"validated"}`), TxTime: time.Now()}
}
