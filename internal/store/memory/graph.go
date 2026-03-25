// Package memory provides in-process implementations of the store interfaces.
// These are suitable for testing, local development, and the embedded
// deployment profile. They are not safe for multi-process use.
package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// GraphStore is a thread-safe in-memory implementation of store.GraphStore.
type GraphStore struct {
	mu      sync.RWMutex
	nodes   map[string][]core.Node   // key: ns+id, value: versions oldest→newest
	edges   map[uuid.UUID]core.Edge  // key: edge ID
	sources map[string]core.Source  // key: ns+externalID
}

// NewGraphStore returns an empty in-memory GraphStore.
func NewGraphStore() *GraphStore {
	return &GraphStore{
		nodes:   make(map[string][]core.Node),
		edges:   make(map[uuid.UUID]core.Edge),
		sources: make(map[string]core.Source),
	}
}

func nodeKey(ns string, id uuid.UUID) string {
	return ns + "/" + id.String()
}

func sourceKey(ns, externalID string) string {
	return ns + "/" + externalID
}

func (g *GraphStore) UpsertNode(_ context.Context, n core.Node) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	if n.TxTime.IsZero() {
		n.TxTime = time.Now()
	}
	if n.ValidFrom.IsZero() {
		n.ValidFrom = n.TxTime
	}

	key := nodeKey(n.Namespace, n.ID)
	versions := g.nodes[key]
	n.Version = uint64(len(versions) + 1)
	g.nodes[key] = append(versions, n)
	return nil
}

func (g *GraphStore) GetNode(_ context.Context, ns string, id uuid.UUID) (*core.Node, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	versions := g.nodes[nodeKey(ns, id)]
	if len(versions) == 0 {
		return nil, nil
	}
	n := versions[len(versions)-1]
	return &n, nil
}

func (g *GraphStore) AsOf(_ context.Context, ns string, id uuid.UUID, t time.Time) (*core.Node, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	versions := g.nodes[nodeKey(ns, id)]
	// Walk newest to oldest, return first version valid at t.
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		if v.IsValidAt(t) && !v.TxTime.After(t) {
			return &v, nil
		}
	}
	return nil, nil
}

func (g *GraphStore) History(_ context.Context, ns string, id uuid.UUID) ([]core.Node, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	versions := g.nodes[nodeKey(ns, id)]
	out := make([]core.Node, len(versions))
	copy(out, versions)
	return out, nil
}

func (g *GraphStore) UpsertEdge(_ context.Context, e core.Edge) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.TxTime.IsZero() {
		e.TxTime = time.Now()
	}
	if e.ValidFrom.IsZero() {
		e.ValidFrom = e.TxTime
	}
	g.edges[e.ID] = e
	return nil
}

func (g *GraphStore) InvalidateEdge(_ context.Context, ns string, id uuid.UUID, at time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	e, ok := g.edges[id]
	if !ok {
		return fmt.Errorf("edge %s not found in namespace %s", id, ns)
	}
	e.InvalidatedAt = &at
	g.edges[id] = e
	return nil
}

func (g *GraphStore) EdgesFrom(_ context.Context, ns string, nodeID uuid.UUID, edgeTypes []string) ([]core.Edge, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	now := time.Now()
	typeSet := sliceToSet(edgeTypes)
	var out []core.Edge
	for _, e := range g.edges {
		if e.Namespace != ns || e.Src != nodeID {
			continue
		}
		if !e.IsActiveAt(now) {
			continue
		}
		if len(typeSet) > 0 && !typeSet[e.Type] {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (g *GraphStore) EdgesTo(_ context.Context, ns string, nodeID uuid.UUID, edgeTypes []string) ([]core.Edge, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	now := time.Now()
	typeSet := sliceToSet(edgeTypes)
	var out []core.Edge
	for _, e := range g.edges {
		if e.Namespace != ns || e.Dst != nodeID {
			continue
		}
		if !e.IsActiveAt(now) {
			continue
		}
		if len(typeSet) > 0 && !typeSet[e.Type] {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// Walk performs a BFS from seed nodes. WaterCircle and Beam strategies
// fall back to BFS in this in-memory implementation — production backends
// provide richer traversals.
func (g *GraphStore) Walk(ctx context.Context, q store.WalkQuery) ([]core.Node, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
	typeSet := sliceToSet(q.EdgeTypes)
	maxDepth := q.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}

	visited := make(map[uuid.UUID]struct{})
	var result []core.Node

	queue := make([]uuid.UUID, len(q.SeedIDs))
	copy(queue, q.SeedIDs)

	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		var next []uuid.UUID
		for _, id := range queue {
			if _, seen := visited[id]; seen {
				continue
			}
			visited[id] = struct{}{}

			// resolve node
			versions := g.nodes[nodeKey(q.Namespace, id)]
			for i := len(versions) - 1; i >= 0; i-- {
				v := versions[i]
				if v.IsValidAt(asOf) {
					result = append(result, v)
					break
				}
			}

			// expand edges
			for _, e := range g.edges {
				if e.Namespace != q.Namespace || e.Src != id {
					continue
				}
				if !e.IsActiveAt(asOf) {
					continue
				}
				if len(typeSet) > 0 && !typeSet[e.Type] {
					continue
				}
				if q.MinWeight > 0 && e.Weight < q.MinWeight {
					continue
				}
				if _, seen := visited[e.Dst]; !seen {
					next = append(next, e.Dst)
				}
			}
		}
		queue = next
	}
	return result, nil
}

func (g *GraphStore) UpsertSource(_ context.Context, s core.Source) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	s.UpdatedAt = time.Now()
	g.sources[sourceKey(s.Namespace, s.ExternalID)] = s
	return nil
}

func (g *GraphStore) GetSourceByExternalID(_ context.Context, ns, externalID string) (*core.Source, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	s, ok := g.sources[sourceKey(ns, externalID)]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (g *GraphStore) UpdateCredibility(_ context.Context, ns string, id uuid.UUID, delta float64) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	for k, s := range g.sources {
		if s.Namespace == ns && s.ID == id {
			s.CredibilityScore = clamp01(s.CredibilityScore + delta)
			s.UpdatedAt = time.Now()
			g.sources[k] = s
			return nil
		}
	}
	return fmt.Errorf("source %s not found", id)
}

func sliceToSet(ss []string) map[string]bool {
	if len(ss) == 0 {
		return nil
	}
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
