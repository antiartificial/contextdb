package remote

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// VectorIndex implements store.VectorIndex via gRPC.
type VectorIndex struct {
	conn *grpc.ClientConn
}

func (v *VectorIndex) Index(ctx context.Context, entry core.VectorEntry) error {
	var resp struct{}
	return invoke(ctx, v.conn, "VectorIndex", &struct {
		Action string           `json:"action"`
		Entry  core.VectorEntry `json:"entry"`
	}{Action: "index", Entry: entry}, &resp)
}

func (v *VectorIndex) Delete(ctx context.Context, ns string, id uuid.UUID) error {
	var resp struct{}
	return invoke(ctx, v.conn, "VectorIndex", &struct {
		Action    string `json:"action"`
		Namespace string `json:"namespace"`
		ID        string `json:"id"`
	}{Action: "delete", Namespace: ns, ID: id.String()}, &resp)
}

func (v *VectorIndex) Search(ctx context.Context, q store.VectorQuery) ([]core.ScoredNode, error) {
	var resp struct {
		Results []core.ScoredNode `json:"results"`
	}
	err := invoke(ctx, v.conn, "VectorSearch", &struct {
		Query store.VectorQuery `json:"query"`
	}{Query: q}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Results, nil
}
