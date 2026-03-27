// Package memory provides in-process implementations of the store interfaces.
// These are suitable for testing, local development, and the embedded
// deployment profile. They are not safe for multi-process use.
package memory

import (
	"context"
	"fmt"
	"strings"
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

func (g *GraphStore) GetEdges(_ context.Context, ns string, nodeID uuid.UUID) ([]core.Edge, error) {
	return g.EdgesFrom(context.Background(), ns, nodeID, nil)
}

func (g *GraphStore) GetEdgesTo(_ context.Context, ns string, nodeID uuid.UUID) ([]core.Edge, error) {
	return g.EdgesTo(context.Background(), ns, nodeID, nil)
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

func (g *GraphStore) RetractNode(_ context.Context, ns string, id uuid.UUID, reason string, at time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	key := nodeKey(ns, id)
	versions := g.nodes[key]
	if len(versions) == 0 {
		return fmt.Errorf("node %s not found in namespace %s", id, ns)
	}

	// Set ValidUntil on the latest version.
	latest := &versions[len(versions)-1]
	latest.ValidUntil = &at
	g.nodes[key] = versions

	// Create a retraction edge.
	edgeID := uuid.New()
	g.edges[edgeID] = core.Edge{
		ID:         edgeID,
		Namespace:  ns,
		Src:        id,
		Dst:        id,
		Type:       "retracted",
		Weight:     1.0,
		Properties: map[string]any{"reason": reason},
		ValidFrom:  at,
		TxTime:     at,
	}
	return nil
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
			// Update Beta distribution: shift mean toward positive/negative
			// Positive delta increases Alpha, negative increases Beta
			if delta > 0 {
				s.Alpha += delta * 10 // scale delta to reasonable update magnitude
			} else {
				s.Beta += -delta * 10
			}
			// Ensure Alpha, Beta stay positive
			if s.Alpha < 1 {
				s.Alpha = 1
			}
			if s.Beta < 1 {
				s.Beta = 1
			}
			s.UpdatedAt = time.Now()
			g.sources[k] = s
			return nil
		}
	}
	return fmt.Errorf("source %s not found", id)
}

func (g *GraphStore) Diff(_ context.Context, ns string, t1, t2 time.Time) ([]store.NodeDiff, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var diffs []store.NodeDiff
	for key, versions := range g.nodes {
		if !strings.HasPrefix(key, ns+"/") {
			continue
		}
		for _, v := range versions {
			if v.TxTime.After(t1) && !v.TxTime.After(t2) {
				change := store.DiffAdded
				if v.Version > 1 {
					change = store.DiffModified
				}
				if v.ValidUntil != nil && !v.ValidUntil.After(t2) {
					change = store.DiffRemoved
				}
				diffs = append(diffs, store.NodeDiff{Node: v, Change: change})
			}
		}
	}
	return diffs, nil
}

func (g *GraphStore) ValidAt(_ context.Context, ns string, t time.Time, labels []string) ([]core.Node, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []core.Node
	seen := make(map[string]bool)
	for key, versions := range g.nodes {
		if !strings.HasPrefix(key, ns+"/") {
			continue
		}
		if seen[key] {
			continue
		}
		// Find the latest version valid at time t.
		for i := len(versions) - 1; i >= 0; i-- {
			v := versions[i]
			if v.IsValidAt(t) {
				if len(labels) > 0 && !hasAllLabels(v, labels) {
					break
				}
				result = append(result, v)
				seen[key] = true
				break
			}
		}
	}
	return result, nil
}

func hasAllLabels(n core.Node, labels []string) bool {
	for _, l := range labels {
		if !n.HasLabel(l) {
			return false
		}
	}
	return true
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
