package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/google/uuid"
)

// UpsertNodeAndEdgeIfVersion publishes the retraction state and audit edge
// under one write lock, so readers cannot observe a partial correction.
func (g *GraphStore) UpsertNodeAndEdgeIfVersion(_ context.Context, n core.Node, expectedVersion uint64, edge core.Edge) (bool, error) {
	if n.ID == uuid.Nil || edge.ID == uuid.Nil || n.Namespace != edge.Namespace {
		return false, fmt.Errorf("atomic node/edge update requires IDs and one namespace")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	key := nodeKey(n.Namespace, n.ID)
	versions := g.nodes[key]
	if len(versions) == 0 || versions[len(versions)-1].Version != expectedVersion {
		return false, nil
	}
	if len(g.edges[edgeKey(edge.Namespace, edge.ID)]) != 0 {
		return false, fmt.Errorf("audit edge %s already exists", edge.ID)
	}
	if n.TxTime.IsZero() {
		n.TxTime = time.Now()
	}
	if !n.TxTime.After(versions[len(versions)-1].TxTime) {
		n.TxTime = versions[len(versions)-1].TxTime.Add(time.Microsecond)
	}
	if n.ValidFrom.IsZero() {
		n.ValidFrom = n.TxTime
	}
	edge.TxTime = n.TxTime
	if edge.ValidFrom.IsZero() {
		edge.ValidFrom = edge.TxTime
	}
	n.Version = expectedVersion + 1
	g.nodes[key] = append(versions, n)
	g.edges[edgeKey(edge.Namespace, edge.ID)] = []core.Edge{edge}
	return true, nil
}
