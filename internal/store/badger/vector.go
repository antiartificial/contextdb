package badger

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

const prefixVec = "vec/"

// HNSWConfig configures the HNSW vector index.
type HNSWConfig struct {
	M              int // max connections per node per layer (default: 16)
	EfConstruction int // beam width during insert (default: 200)
	EfSearch       int // beam width during search (default: 50)
}

func (c HNSWConfig) withDefaults() HNSWConfig {
	if c.M == 0 {
		c.M = 16
	}
	if c.EfConstruction == 0 {
		c.EfConstruction = 200
	}
	if c.EfSearch == 0 {
		c.EfSearch = 50
	}
	return c
}

// VectorIndex implements store.VectorIndex using an in-process HNSW graph
// with BadgerDB for vector entry persistence.
type VectorIndex struct {
	mu     sync.RWMutex
	db     *badgerdb.DB
	config HNSWConfig
	// per-namespace HNSW graphs
	graphs map[string]*hnswGraph
	// node cache for score assembly
	nodes map[uuid.UUID]core.Node
}

// NewVectorIndex returns a VectorIndex backed by BadgerDB with HNSW search.
func NewVectorIndex(db *badgerdb.DB, cfg HNSWConfig) *VectorIndex {
	cfg = cfg.withDefaults()
	return &VectorIndex{
		db:     db,
		config: cfg,
		graphs: make(map[string]*hnswGraph),
		nodes:  make(map[uuid.UUID]core.Node),
	}
}

// RegisterNode caches a node for score assembly during Search.
func (v *VectorIndex) RegisterNode(n core.Node) {
	v.mu.Lock()
	v.nodes[n.ID] = n
	v.mu.Unlock()
}

func vecKey(ns string, id uuid.UUID) []byte {
	return []byte(fmt.Sprintf("%s%s/%s", prefixVec, ns, id))
}

func vecNSPrefix(ns string) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixVec, ns))
}

func (v *VectorIndex) Index(_ context.Context, entry core.VectorEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal vector entry: %w", err)
	}

	if err := v.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(vecKey(entry.Namespace, entry.ID), data)
	}); err != nil {
		return err
	}

	// insert into HNSW graph
	v.mu.Lock()
	defer v.mu.Unlock()
	g := v.getOrCreateGraph(entry.Namespace)
	g.insert(entry.ID, entry.NodeID, entry.Vector, v.config)
	return nil
}

func (v *VectorIndex) Delete(_ context.Context, ns string, id uuid.UUID) error {
	if err := v.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Delete(vecKey(ns, id))
	}); err != nil {
		return err
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if g, ok := v.graphs[ns]; ok {
		g.remove(id)
	}
	return nil
}

func (v *VectorIndex) Search(_ context.Context, q store.VectorQuery) ([]core.ScoredNode, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
	topK := q.TopK
	if topK <= 0 {
		topK = 20
	}
	labelSet := sliceToSet(q.Labels)

	g, ok := v.graphs[q.Namespace]
	if !ok || len(g.nodes) == 0 {
		return nil, nil
	}

	// HNSW search
	candidates := g.search(q.Vector, topK*2, v.config.EfSearch)

	var out []core.ScoredNode
	for _, c := range candidates {
		if len(out) >= topK {
			break
		}
		if c.similarity <= 0 {
			continue
		}

		var node core.Node
		if c.nodeID != nil {
			n, ok := v.nodes[*c.nodeID]
			if !ok {
				continue
			}
			if !n.IsValidAt(asOf) {
				continue
			}
			if len(labelSet) > 0 {
				allMatch := true
				for lbl := range labelSet {
					if !n.HasLabel(lbl) {
						allMatch = false
						break
					}
				}
				if !allMatch {
					continue
				}
			}
			node = n
		} else {
			// fetch from BadgerDB for standalone entries
			node = core.Node{
				Namespace:  q.Namespace,
				Properties: map[string]any{},
			}
		}

		out = append(out, core.ScoredNode{
			Node:            node,
			Score:           c.similarity,
			SimilarityScore: c.similarity,
			RetrievalSource: "vector",
		})
	}
	return out, nil
}

// Load reconstructs all HNSW graphs from persisted vector entries.
func (v *VectorIndex) Load() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.db.View(func(txn *badgerdb.Txn) error {
		prefix := []byte(prefixVec)
		opts := badgerdb.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			var entry core.VectorEntry
			if err := it.Item().Value(func(val []byte) error {
				return json.Unmarshal(val, &entry)
			}); err != nil {
				continue
			}
			g := v.getOrCreateGraph(entry.Namespace)
			g.insert(entry.ID, entry.NodeID, entry.Vector, v.config)
		}
		return nil
	})
}

func (v *VectorIndex) getOrCreateGraph(ns string) *hnswGraph {
	g, ok := v.graphs[ns]
	if !ok {
		g = newHNSWGraph()
		v.graphs[ns] = g
	}
	return g
}

// ─── HNSW implementation ────────────────────────────────────────────────────

type hnswNode struct {
	id       uuid.UUID
	nodeID   *uuid.UUID
	vector   []float32
	level    int
	friends  [][]uuid.UUID // friends[level] = neighbor IDs at that level
}

type hnswGraph struct {
	nodes      map[uuid.UUID]*hnswNode
	entryPoint uuid.UUID
	maxLevel   int
	levelMult  float64
}

type searchCandidate struct {
	id         uuid.UUID
	nodeID     *uuid.UUID
	similarity float64
}

func newHNSWGraph() *hnswGraph {
	return &hnswGraph{
		nodes:    make(map[uuid.UUID]*hnswNode),
		levelMult: 1.0 / math.Log(16), // 1/ln(M)
	}
}

func (g *hnswGraph) randomLevel() int {
	r := rand.Float64()
	level := int(-math.Log(r) * g.levelMult)
	if level > 20 {
		level = 20
	}
	return level
}

func (g *hnswGraph) insert(id uuid.UUID, nodeID *uuid.UUID, vector []float32, cfg HNSWConfig) {
	level := g.randomLevel()
	node := &hnswNode{
		id:      id,
		nodeID:  nodeID,
		vector:  vector,
		level:   level,
		friends: make([][]uuid.UUID, level+1),
	}

	if len(g.nodes) == 0 {
		g.nodes[id] = node
		g.entryPoint = id
		g.maxLevel = level
		return
	}

	g.nodes[id] = node

	// find entry point at highest level
	ep := g.entryPoint
	for l := g.maxLevel; l > level; l-- {
		ep = g.greedyClosest(ep, vector, l)
	}

	// insert at each level from level down to 0
	for l := min(level, g.maxLevel); l >= 0; l-- {
		neighbors := g.searchLayer(ep, vector, cfg.EfConstruction, l)
		// select top M neighbors
		selected := g.selectNeighbors(neighbors, cfg.M)
		node.friends[l] = selected

		// add back-links
		for _, nID := range selected {
			if neighbor, ok := g.nodes[nID]; ok {
				if l < len(neighbor.friends) {
					neighbor.friends[l] = append(neighbor.friends[l], id)
					// prune if over capacity
					if len(neighbor.friends[l]) > cfg.M*2 {
						neighbor.friends[l] = g.pruneNeighbors(neighbor.friends[l], neighbor.vector, cfg.M*2, l)
					}
				}
			}
		}
		if len(neighbors) > 0 {
			ep = neighbors[0]
		}
	}

	if level > g.maxLevel {
		g.maxLevel = level
		g.entryPoint = id
	}
}

func (g *hnswGraph) remove(id uuid.UUID) {
	node, ok := g.nodes[id]
	if !ok {
		return
	}

	// remove back-links from neighbors
	for l := 0; l < len(node.friends); l++ {
		for _, nID := range node.friends[l] {
			if neighbor, ok := g.nodes[nID]; ok && l < len(neighbor.friends) {
				neighbor.friends[l] = removeFromSlice(neighbor.friends[l], id)
			}
		}
	}

	delete(g.nodes, id)

	// update entry point if needed
	if g.entryPoint == id && len(g.nodes) > 0 {
		for _, n := range g.nodes {
			g.entryPoint = n.id
			break
		}
	}
}

func (g *hnswGraph) search(query []float32, topK, efSearch int) []searchCandidate {
	if len(g.nodes) == 0 {
		return nil
	}

	ep := g.entryPoint

	// traverse from top to layer 1
	for l := g.maxLevel; l > 0; l-- {
		ep = g.greedyClosest(ep, query, l)
	}

	// search at layer 0
	candidates := g.searchLayer(ep, query, efSearch, 0)

	var results []searchCandidate
	seen := make(map[uuid.UUID]bool)
	for _, cID := range candidates {
		if seen[cID] {
			continue
		}
		seen[cID] = true
		if node, ok := g.nodes[cID]; ok {
			sim := core.CosineSimilarity(query, node.vector)
			results = append(results, searchCandidate{
				id:         node.id,
				nodeID:     node.nodeID,
				similarity: sim,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].similarity > results[j].similarity
	})
	if len(results) > topK {
		results = results[:topK]
	}
	return results
}

func (g *hnswGraph) greedyClosest(ep uuid.UUID, query []float32, level int) uuid.UUID {
	if _, ok := g.nodes[ep]; !ok {
		return ep
	}
	best := ep
	bestSim := g.similarity(ep, query)

	changed := true
	for changed {
		changed = false
		node := g.nodes[best]
		if level >= len(node.friends) {
			break
		}
		for _, nID := range node.friends[level] {
			sim := g.similarity(nID, query)
			if sim > bestSim {
				bestSim = sim
				best = nID
				changed = true
			}
		}
	}
	return best
}

func (g *hnswGraph) searchLayer(ep uuid.UUID, query []float32, ef, level int) []uuid.UUID {
	visited := map[uuid.UUID]bool{ep: true}
	candidates := []uuid.UUID{ep}
	results := []uuid.UUID{ep}

	for len(candidates) > 0 {
		// pop best candidate
		bestIdx := 0
		bestSim := g.similarity(candidates[0], query)
		for i := 1; i < len(candidates); i++ {
			sim := g.similarity(candidates[i], query)
			if sim > bestSim {
				bestSim = sim
				bestIdx = i
			}
		}
		current := candidates[bestIdx]
		candidates = append(candidates[:bestIdx], candidates[bestIdx+1:]...)

		// worst in results
		worstSim := g.similarity(results[len(results)-1], query)
		for _, r := range results {
			sim := g.similarity(r, query)
			if sim < worstSim {
				worstSim = sim
			}
		}

		if bestSim < worstSim && len(results) >= ef {
			break
		}

		node := g.nodes[current]
		if node == nil || level >= len(node.friends) {
			continue
		}
		for _, nID := range node.friends[level] {
			if visited[nID] {
				continue
			}
			visited[nID] = true

			sim := g.similarity(nID, query)
			if sim > worstSim || len(results) < ef {
				candidates = append(candidates, nID)
				results = append(results, nID)
				// keep results bounded
				if len(results) > ef {
					results = g.pruneByDistance(results, query, ef)
				}
			}
		}
	}
	return results
}

func (g *hnswGraph) selectNeighbors(candidates []uuid.UUID, maxN int) []uuid.UUID {
	if len(candidates) <= maxN {
		return candidates
	}
	return candidates[:maxN]
}

func (g *hnswGraph) pruneNeighbors(neighbors []uuid.UUID, origin []float32, maxN int, _ int) []uuid.UUID {
	type scored struct {
		id  uuid.UUID
		sim float64
	}
	var items []scored
	for _, nID := range neighbors {
		if n, ok := g.nodes[nID]; ok {
			items = append(items, scored{nID, core.CosineSimilarity(origin, n.vector)})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].sim > items[j].sim })
	if len(items) > maxN {
		items = items[:maxN]
	}
	result := make([]uuid.UUID, len(items))
	for i, it := range items {
		result[i] = it.id
	}
	return result
}

func (g *hnswGraph) pruneByDistance(ids []uuid.UUID, query []float32, maxN int) []uuid.UUID {
	type scored struct {
		id  uuid.UUID
		sim float64
	}
	items := make([]scored, len(ids))
	for i, id := range ids {
		items[i] = scored{id, g.similarity(id, query)}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].sim > items[j].sim })
	if len(items) > maxN {
		items = items[:maxN]
	}
	result := make([]uuid.UUID, len(items))
	for i, it := range items {
		result[i] = it.id
	}
	return result
}

func (g *hnswGraph) similarity(id uuid.UUID, query []float32) float64 {
	n, ok := g.nodes[id]
	if !ok {
		return -1
	}
	return core.CosineSimilarity(query, n.vector)
}

func removeFromSlice(s []uuid.UUID, id uuid.UUID) []uuid.UUID {
	for i, v := range s {
		if v == id {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
