package badger

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/google/uuid"
)

// UpsertNodeAndEdgeIfVersion writes both records in one Badger transaction.
func (g *GraphStore) UpsertNodeAndEdgeIfVersion(_ context.Context, n core.Node, expectedVersion uint64, edge core.Edge) (bool, error) {
	if n.ID == uuid.Nil || edge.ID == uuid.Nil || n.Namespace != edge.Namespace {
		return false, fmt.Errorf("atomic node/edge update requires IDs and one namespace")
	}
	written := false
	err := g.db.Update(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(nodeLatestKey(n.Namespace, n.ID))
		if err == badgerdb.ErrKeyNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		var latest core.Node
		if err := item.Value(func(value []byte) error { return json.Unmarshal(value, &latest) }); err != nil {
			return err
		}
		if latest.Version != expectedVersion {
			return nil
		}
		if _, err := txn.Get(edgeKey(edge.Namespace, edge.ID)); err == nil {
			return fmt.Errorf("audit edge %s already exists", edge.ID)
		} else if err != badgerdb.ErrKeyNotFound {
			return err
		}
		if n.TxTime.IsZero() {
			n.TxTime = time.Now()
		}
		if !n.TxTime.After(latest.TxTime) {
			n.TxTime = latest.TxTime.Add(time.Microsecond)
		}
		if n.ValidFrom.IsZero() {
			n.ValidFrom = n.TxTime
		}
		edge.TxTime = n.TxTime
		if edge.ValidFrom.IsZero() {
			edge.ValidFrom = edge.TxTime
		}
		n.Version = expectedVersion + 1
		nodeData, err := json.Marshal(n)
		if err != nil {
			return err
		}
		edgeData, err := json.Marshal(edge)
		if err != nil {
			return err
		}
		if err := txn.Set(nodeKey(n.Namespace, n.ID, n.Version), nodeData); err != nil {
			return err
		}
		if err := txn.Set(nodeLatestKey(n.Namespace, n.ID), nodeData); err != nil {
			return err
		}
		if n.Fingerprint != "" {
			if err := txn.Set(fingerprintKey(n.Namespace, n.Fingerprint), []byte(n.ID.String())); err != nil {
				return err
			}
		}
		if err := appendEdgeVersion(txn, edge); err != nil {
			return err
		}
		if err := txn.Set(edgeKey(edge.Namespace, edge.ID), edgeData); err != nil {
			return err
		}
		if err := txn.Set(edgeSrcKey(edge.Namespace, edge.Src, edge.ID), nil); err != nil {
			return err
		}
		if err := txn.Set(edgeDstKey(edge.Namespace, edge.Dst, edge.ID), nil); err != nil {
			return err
		}
		written = true
		return nil
	})
	if err == badgerdb.ErrConflict {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return written, nil
}
