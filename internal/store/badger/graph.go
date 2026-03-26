package badger

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// Key prefixes for graph data in BadgerDB.
const (
	prefixNode       = "n/"
	prefixNodeLatest = "n-latest/"
	prefixEdge       = "e/"
	prefixEdgeSrc    = "ei-src/"
	prefixEdgeDst    = "ei-dst/"
	prefixSource     = "s/"
)

// GraphStore implements store.GraphStore backed by BadgerDB.
type GraphStore struct {
	db *badgerdb.DB
}

// NewGraphStore returns a GraphStore backed by the given BadgerDB instance.
func NewGraphStore(db *badgerdb.DB) *GraphStore {
	return &GraphStore{db: db}
}

func nodeKey(ns string, id uuid.UUID, version uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%s/%08d", prefixNode, ns, id, version))
}

func nodeLatestKey(ns string, id uuid.UUID) []byte {
	return []byte(fmt.Sprintf("%s%s/%s", prefixNodeLatest, ns, id))
}

func nodePrefix(ns string, id uuid.UUID) []byte {
	return []byte(fmt.Sprintf("%s%s/%s/", prefixNode, ns, id))
}

func edgeKey(ns string, id uuid.UUID) []byte {
	return []byte(fmt.Sprintf("%s%s/%s", prefixEdge, ns, id))
}

func edgeSrcKey(ns string, srcID, edgeID uuid.UUID) []byte {
	return []byte(fmt.Sprintf("%s%s/%s/%s", prefixEdgeSrc, ns, srcID, edgeID))
}

func edgeDstKey(ns string, dstID, edgeID uuid.UUID) []byte {
	return []byte(fmt.Sprintf("%s%s/%s/%s", prefixEdgeDst, ns, dstID, edgeID))
}

func edgeSrcPrefix(ns string, nodeID uuid.UUID) []byte {
	return []byte(fmt.Sprintf("%s%s/%s/", prefixEdgeSrc, ns, nodeID))
}

func edgeDstPrefix(ns string, nodeID uuid.UUID) []byte {
	return []byte(fmt.Sprintf("%s%s/%s/", prefixEdgeDst, ns, nodeID))
}

func sourceKey(ns, externalID string) []byte {
	return []byte(fmt.Sprintf("%s%s/%s", prefixSource, ns, externalID))
}

func (g *GraphStore) UpsertNode(_ context.Context, n core.Node) error {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	if n.TxTime.IsZero() {
		n.TxTime = time.Now()
	}
	if n.ValidFrom.IsZero() {
		n.ValidFrom = n.TxTime
	}

	return g.db.Update(func(txn *badgerdb.Txn) error {
		// determine version
		var version uint64 = 1
		latestKey := nodeLatestKey(n.Namespace, n.ID)
		item, err := txn.Get(latestKey)
		if err == nil {
			var existing core.Node
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &existing)
			}); err == nil {
				version = existing.Version + 1
			}
		}
		n.Version = version

		data, err := json.Marshal(n)
		if err != nil {
			return fmt.Errorf("marshal node: %w", err)
		}

		// write versioned key
		if err := txn.Set(nodeKey(n.Namespace, n.ID, version), data); err != nil {
			return err
		}
		// write latest key
		return txn.Set(latestKey, data)
	})
}

func (g *GraphStore) GetNode(_ context.Context, ns string, id uuid.UUID) (*core.Node, error) {
	var n core.Node
	err := g.db.View(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(nodeLatestKey(ns, id))
		if err == badgerdb.ErrKeyNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &n)
		})
	})
	if err != nil {
		return nil, err
	}
	if n.ID == uuid.Nil {
		return nil, nil
	}
	return &n, nil
}

func (g *GraphStore) AsOf(_ context.Context, ns string, id uuid.UUID, t time.Time) (*core.Node, error) {
	var result *core.Node
	err := g.db.View(func(txn *badgerdb.Txn) error {
		prefix := nodePrefix(ns, id)
		opts := badgerdb.DefaultIteratorOptions
		opts.Reverse = true
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		// seek to end of prefix range
		seekKey := append(prefix, 0xFF)
		for it.Seek(seekKey); it.ValidForPrefix(prefix); it.Next() {
			var n core.Node
			if err := it.Item().Value(func(val []byte) error {
				return json.Unmarshal(val, &n)
			}); err != nil {
				return err
			}
			if n.IsValidAt(t) && !n.TxTime.After(t) {
				result = &n
				return nil
			}
		}
		return nil
	})
	return result, err
}

func (g *GraphStore) History(_ context.Context, ns string, id uuid.UUID) ([]core.Node, error) {
	var nodes []core.Node
	err := g.db.View(func(txn *badgerdb.Txn) error {
		prefix := nodePrefix(ns, id)
		opts := badgerdb.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			var n core.Node
			if err := it.Item().Value(func(val []byte) error {
				return json.Unmarshal(val, &n)
			}); err != nil {
				return err
			}
			nodes = append(nodes, n)
		}
		return nil
	})
	return nodes, err
}

func (g *GraphStore) UpsertEdge(_ context.Context, e core.Edge) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.TxTime.IsZero() {
		e.TxTime = time.Now()
	}
	if e.ValidFrom.IsZero() {
		e.ValidFrom = e.TxTime
	}

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal edge: %w", err)
	}

	return g.db.Update(func(txn *badgerdb.Txn) error {
		if err := txn.Set(edgeKey(e.Namespace, e.ID), data); err != nil {
			return err
		}
		if err := txn.Set(edgeSrcKey(e.Namespace, e.Src, e.ID), nil); err != nil {
			return err
		}
		return txn.Set(edgeDstKey(e.Namespace, e.Dst, e.ID), nil)
	})
}

func (g *GraphStore) InvalidateEdge(_ context.Context, ns string, id uuid.UUID, at time.Time) error {
	return g.db.Update(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(edgeKey(ns, id))
		if err == badgerdb.ErrKeyNotFound {
			return fmt.Errorf("edge %s not found in namespace %s", id, ns)
		}
		if err != nil {
			return err
		}

		var e core.Edge
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &e)
		}); err != nil {
			return err
		}

		e.InvalidatedAt = &at
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		return txn.Set(edgeKey(ns, id), data)
	})
}

func (g *GraphStore) GetEdges(_ context.Context, ns string, nodeID uuid.UUID) ([]core.Edge, error) {
	return g.EdgesFrom(context.Background(), ns, nodeID, nil)
}

func (g *GraphStore) GetEdgesTo(_ context.Context, ns string, nodeID uuid.UUID) ([]core.Edge, error) {
	return g.EdgesTo(context.Background(), ns, nodeID, nil)
}

func (g *GraphStore) EdgesFrom(_ context.Context, ns string, nodeID uuid.UUID, edgeTypes []string) ([]core.Edge, error) {
	return g.edgesByIndex(edgeSrcPrefix(ns, nodeID), ns, edgeTypes)
}

func (g *GraphStore) EdgesTo(_ context.Context, ns string, nodeID uuid.UUID, edgeTypes []string) ([]core.Edge, error) {
	return g.edgesByIndex(edgeDstPrefix(ns, nodeID), ns, edgeTypes)
}

func (g *GraphStore) edgesByIndex(prefix []byte, ns string, edgeTypes []string) ([]core.Edge, error) {
	typeSet := sliceToSet(edgeTypes)
	now := time.Now()
	var edges []core.Edge

	err := g.db.View(func(txn *badgerdb.Txn) error {
		opts := badgerdb.DefaultIteratorOptions
		opts.Prefix = prefix
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			// extract edge ID from index key (last 36 chars)
			key := it.Item().Key()
			keyStr := string(key)
			if len(keyStr) < 36 {
				continue
			}
			edgeIDStr := keyStr[len(keyStr)-36:]
			edgeID, err := uuid.Parse(edgeIDStr)
			if err != nil {
				continue
			}

			// fetch the actual edge
			eItem, err := txn.Get(edgeKey(ns, edgeID))
			if err != nil {
				continue
			}
			var e core.Edge
			if err := eItem.Value(func(val []byte) error {
				return json.Unmarshal(val, &e)
			}); err != nil {
				continue
			}

			if !e.IsActiveAt(now) {
				continue
			}
			if len(typeSet) > 0 && !typeSet[e.Type] {
				continue
			}
			edges = append(edges, e)
		}
		return nil
	})
	return edges, err
}

func (g *GraphStore) Walk(ctx context.Context, q store.WalkQuery) ([]core.Node, error) {
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
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
			n, err := g.AsOf(ctx, q.Namespace, id, asOf)
			if err != nil {
				return nil, err
			}
			if n != nil {
				result = append(result, *n)
			}

			// expand edges
			edges, err := g.EdgesFrom(ctx, q.Namespace, id, q.EdgeTypes)
			if err != nil {
				return nil, err
			}
			for _, e := range edges {
				if !e.IsActiveAt(asOf) {
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
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	s.UpdatedAt = time.Now()

	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal source: %w", err)
	}
	return g.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(sourceKey(s.Namespace, s.ExternalID), data)
	})
}

func (g *GraphStore) GetSourceByExternalID(_ context.Context, ns, externalID string) (*core.Source, error) {
	var s core.Source
	err := g.db.View(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(sourceKey(ns, externalID))
		if err == badgerdb.ErrKeyNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &s)
		})
	})
	if err != nil {
		return nil, err
	}
	if s.ID == uuid.Nil {
		return nil, nil
	}
	return &s, nil
}

func (g *GraphStore) UpdateCredibility(_ context.Context, ns string, id uuid.UUID, delta float64) error {
	return g.db.Update(func(txn *badgerdb.Txn) error {
		// scan sources to find by UUID
		prefix := []byte(prefixSource + ns + "/")
		opts := badgerdb.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			var s core.Source
			if err := it.Item().Value(func(val []byte) error {
				return json.Unmarshal(val, &s)
			}); err != nil {
				continue
			}
			if s.ID == id && s.Namespace == ns {
				// Update Beta distribution: shift mean toward positive/negative
			// Positive delta increases Alpha, negative increases Beta
			if delta > 0 {
				s.Alpha += delta * 10
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
				data, err := json.Marshal(s)
				if err != nil {
					return err
				}
				return txn.Set(it.Item().KeyCopy(nil), data)
			}
		}
		return fmt.Errorf("source %s not found", id)
	})
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
