package remote

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// GraphStore implements store.GraphStore via gRPC.
type GraphStore struct {
	conn *grpc.ClientConn
}

func (g *GraphStore) UpsertNode(ctx context.Context, n core.Node) error {
	var resp struct{}
	return invoke(ctx, g.conn, "UpsertNode", &struct{ Node core.Node }{Node: n}, &resp)
}

func (g *GraphStore) GetNode(ctx context.Context, ns string, id uuid.UUID) (*core.Node, error) {
	var resp struct {
		Node  *core.Node `json:"node"`
		Found bool       `json:"found"`
	}
	err := invoke(ctx, g.conn, "GetNode", &struct {
		Namespace string `json:"namespace"`
		ID        string `json:"id"`
	}{Namespace: ns, ID: id.String()}, &resp)
	if err != nil {
		return nil, err
	}
	if !resp.Found {
		return nil, nil
	}
	return resp.Node, nil
}

func (g *GraphStore) AsOf(ctx context.Context, ns string, id uuid.UUID, t time.Time) (*core.Node, error) {
	// AsOf not exposed via gRPC yet — fetch history and filter client-side
	nodes, err := g.History(ctx, ns, id)
	if err != nil {
		return nil, err
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		if nodes[i].IsValidAt(t) {
			return &nodes[i], nil
		}
	}
	return nil, nil
}

func (g *GraphStore) History(ctx context.Context, ns string, id uuid.UUID) ([]core.Node, error) {
	var resp struct {
		Nodes []core.Node `json:"nodes"`
	}
	err := invoke(ctx, g.conn, "NodeHistory", &struct {
		Namespace string `json:"namespace"`
		ID        string `json:"id"`
	}{Namespace: ns, ID: id.String()}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Nodes, nil
}

func (g *GraphStore) UpsertEdge(ctx context.Context, e core.Edge) error {
	var resp struct{}
	return invoke(ctx, g.conn, "UpsertEdge", &struct{ Edge core.Edge }{Edge: e}, &resp)
}

func (g *GraphStore) InvalidateEdge(ctx context.Context, ns string, id uuid.UUID, at time.Time) error {
	var resp struct{}
	return invoke(ctx, g.conn, "InvalidateEdge", &struct {
		Namespace string    `json:"namespace"`
		ID        string    `json:"id"`
		At        time.Time `json:"at"`
	}{Namespace: ns, ID: id.String(), At: at}, &resp)
}

func (g *GraphStore) GetEdges(ctx context.Context, ns string, nodeID uuid.UUID) ([]core.Edge, error) {
	return g.EdgesFrom(ctx, ns, nodeID, nil)
}

func (g *GraphStore) GetEdgesTo(ctx context.Context, ns string, nodeID uuid.UUID) ([]core.Edge, error) {
	return g.EdgesTo(ctx, ns, nodeID, nil)
}

func (g *GraphStore) EdgesFrom(ctx context.Context, ns string, nodeID uuid.UUID, edgeTypes []string) ([]core.Edge, error) {
	var resp struct {
		Edges []core.Edge `json:"edges"`
	}
	err := invoke(ctx, g.conn, "Edges", &struct {
		Namespace string   `json:"namespace"`
		NodeID    string   `json:"node_id"`
		Direction string   `json:"direction"`
		EdgeTypes []string `json:"edge_types"`
	}{Namespace: ns, NodeID: nodeID.String(), Direction: "from", EdgeTypes: edgeTypes}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Edges, nil
}

func (g *GraphStore) EdgesTo(ctx context.Context, ns string, nodeID uuid.UUID, edgeTypes []string) ([]core.Edge, error) {
	var resp struct {
		Edges []core.Edge `json:"edges"`
	}
	err := invoke(ctx, g.conn, "Edges", &struct {
		Namespace string   `json:"namespace"`
		NodeID    string   `json:"node_id"`
		Direction string   `json:"direction"`
		EdgeTypes []string `json:"edge_types"`
	}{Namespace: ns, NodeID: nodeID.String(), Direction: "to", EdgeTypes: edgeTypes}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Edges, nil
}

func (g *GraphStore) Walk(ctx context.Context, q store.WalkQuery) ([]core.Node, error) {
	seedIDs := make([]string, len(q.SeedIDs))
	for i, id := range q.SeedIDs {
		seedIDs[i] = id.String()
	}
	var resp struct {
		Nodes []core.Node `json:"nodes"`
	}
	err := invoke(ctx, g.conn, "WalkGraph", &struct {
		Namespace string   `json:"namespace"`
		SeedIDs   []string `json:"seed_ids"`
		EdgeTypes []string `json:"edge_types"`
		MaxDepth  int      `json:"max_depth"`
		Strategy  string   `json:"strategy"`
		MinWeight float64  `json:"min_weight"`
	}{
		Namespace: q.Namespace,
		SeedIDs:   seedIDs,
		EdgeTypes: q.EdgeTypes,
		MaxDepth:  q.MaxDepth,
		Strategy:  string(q.Strategy),
		MinWeight: q.MinWeight,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Nodes, nil
}

func (g *GraphStore) RetractNode(ctx context.Context, ns string, id uuid.UUID, reason string, at time.Time) error {
	// Composite operation over existing RPCs: get, modify, upsert + create edge.
	n, err := g.GetNode(ctx, ns, id)
	if err != nil {
		return err
	}
	if n == nil {
		return fmt.Errorf("node %s not found in namespace %s", id, ns)
	}

	n.ValidUntil = &at
	if err := g.UpsertNode(ctx, *n); err != nil {
		return err
	}

	return g.UpsertEdge(ctx, core.Edge{
		ID:         uuid.New(),
		Namespace:  ns,
		Src:        id,
		Dst:        id,
		Type:       "retracted",
		Weight:     1.0,
		Properties: map[string]any{"reason": reason},
		ValidFrom:  at,
		TxTime:     at,
	})
}

func (g *GraphStore) UpsertSource(ctx context.Context, s core.Source) error {
	var resp struct {
		Source *core.Source `json:"source"`
	}
	return invoke(ctx, g.conn, "ManageSource", &struct {
		Action string      `json:"action"`
		Source core.Source  `json:"source"`
	}{Action: "upsert", Source: s}, &resp)
}

func (g *GraphStore) GetSourceByExternalID(ctx context.Context, ns, externalID string) (*core.Source, error) {
	var resp struct {
		Source *core.Source `json:"source"`
	}
	err := invoke(ctx, g.conn, "ManageSource", &struct {
		Action     string `json:"action"`
		Namespace  string `json:"namespace"`
		ExternalID string `json:"external_id"`
	}{Action: "get", Namespace: ns, ExternalID: externalID}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Source, nil
}

func (g *GraphStore) Diff(_ context.Context, _ string, _, _ time.Time) ([]store.NodeDiff, error) {
	return nil, fmt.Errorf("not supported on remote store")
}

func (g *GraphStore) ValidAt(_ context.Context, _ string, _ time.Time, _ []string) ([]core.Node, error) {
	return nil, fmt.Errorf("not supported on remote store")
}

func (g *GraphStore) UpdateCredibility(ctx context.Context, ns string, id uuid.UUID, delta float64) error {
	var resp struct {
		Source *core.Source `json:"source"`
	}
	return invoke(ctx, g.conn, "ManageSource", &struct {
		Action   string  `json:"action"`
		Namespace string `json:"namespace"`
		SourceID string  `json:"source_id"`
		Delta    float64 `json:"delta"`
	}{Action: "update_credibility", Namespace: ns, SourceID: id.String(), Delta: delta}, &resp)
}
