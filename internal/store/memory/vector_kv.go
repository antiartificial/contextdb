package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// VectorIndex is a brute-force in-memory ANN implementation.
// O(n) per search — suitable for development and small datasets (<100k vectors).
type VectorIndex struct {
	mu      sync.RWMutex
	entries map[uuid.UUID]core.VectorEntry
	// nodeMap caches the latest node per nodeID for score assembly
	nodes map[uuid.UUID]core.Node
}

func NewVectorIndex() *VectorIndex {
	return &VectorIndex{
		entries: make(map[uuid.UUID]core.VectorEntry),
		nodes:   make(map[uuid.UUID]core.Node),
	}
}

// RegisterNode registers a node so the vector index can assemble ScoredNodes.
// In a real backend the graph store and vector index share a database.
func (v *VectorIndex) RegisterNode(n core.Node) {
	v.mu.Lock()
	v.nodes[n.ID] = n
	v.mu.Unlock()
}

func (v *VectorIndex) Index(_ context.Context, entry core.VectorEntry) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	v.entries[entry.ID] = entry
	return nil
}

func (v *VectorIndex) Delete(_ context.Context, ns string, id uuid.UUID) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.entries, id)
	return nil
}

type scoredEntry struct {
	entry core.VectorEntry
	score float64
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

	var candidates []scoredEntry
	for _, e := range v.entries {
		if e.Namespace != q.Namespace {
			continue
		}
		sim := core.CosineSimilarity(q.Vector, e.Vector)
		if sim <= 0 {
			continue
		}
		candidates = append(candidates, scoredEntry{entry: e, score: sim})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	var out []core.ScoredNode
	for _, c := range candidates {
		if len(out) >= topK {
			break
		}

		var node core.Node
		if c.entry.NodeID != nil {
			n, ok := v.nodes[*c.entry.NodeID]
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
			// standalone vector entry — synthesise a minimal node
			node = core.Node{
				Namespace: c.entry.Namespace,
				Properties: map[string]any{
					"text": c.entry.Text,
				},
			}
		}

		sn := core.ScoredNode{
			Node:            node,
			Score:           c.score,
			SimilarityScore: c.score,
			RetrievalSource: "vector",
		}
		out = append(out, sn)
	}
	return out, nil
}

// KVStore is an in-memory key-value store with TTL support.
type KVStore struct {
	mu    sync.RWMutex
	items map[string]kvItem
}

type kvItem struct {
	val       []byte
	expiresAt time.Time // zero = no expiry
}

func NewKVStore() *KVStore {
	return &KVStore{items: make(map[string]kvItem)}
}

func (k *KVStore) Get(_ context.Context, key string) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	item, ok := k.items[key]
	if !ok {
		return nil, nil
	}
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		return nil, nil
	}
	return item.val, nil
}

func (k *KVStore) Set(_ context.Context, key string, val []byte, ttlSeconds int) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	item := kvItem{val: val}
	if ttlSeconds > 0 {
		item.expiresAt = time.Now().Add(time.Duration(ttlSeconds) * time.Second)
	}
	k.items[key] = item
	return nil
}

func (k *KVStore) Delete(_ context.Context, key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.items, key)
	return nil
}
