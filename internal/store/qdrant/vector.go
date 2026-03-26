// Package qdrant implements VectorIndex using Qdrant's gRPC API.
// Collection-per-namespace with payload filtering for labels.
//
//go:build integration
// +build integration

package qdrant

import (
	"context"
	"fmt"

	pb "github.com/qdrant/go-client/qdrant"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// VectorIndex implements store.VectorIndex using Qdrant.
type VectorIndex struct {
	conn       *grpc.ClientConn
	points     pb.PointsClient
	collections pb.CollectionsClient
	dimensions uint64
}

// New connects to a Qdrant server and returns a VectorIndex.
func New(addr string, dimensions int) (*VectorIndex, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("qdrant: dial %s: %w", addr, err)
	}

	return &VectorIndex{
		conn:        conn,
		points:      pb.NewPointsClient(conn),
		collections: pb.NewCollectionsClient(conn),
		dimensions:  uint64(dimensions),
	}, nil
}

// Close closes the gRPC connection.
func (v *VectorIndex) Close() error {
	return v.conn.Close()
}

func (v *VectorIndex) collectionName(ns string) string {
	return "contextdb_" + ns
}

func (v *VectorIndex) ensureCollection(ctx context.Context, ns string) error {
	name := v.collectionName(ns)

	// Check if collection exists
	_, err := v.collections.Get(ctx, &pb.GetCollectionInfoRequest{
		CollectionName: name,
	})
	if err == nil {
		return nil // already exists
	}

	// Create collection
	_, err = v.collections.Create(ctx, &pb.CreateCollection{
		CollectionName: name,
		VectorsConfig: &pb.VectorsConfig{
			Config: &pb.VectorsConfig_Params{
				Params: &pb.VectorParams{
					Size:     v.dimensions,
					Distance: pb.Distance_Cosine,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("create collection %s: %w", name, err)
	}
	return nil
}

func (v *VectorIndex) Index(ctx context.Context, entry core.VectorEntry) error {
	if err := v.ensureCollection(ctx, entry.Namespace); err != nil {
		return err
	}

	pointID := entry.ID.String()
	nodeIDStr := ""
	if entry.NodeID != nil {
		nodeIDStr = entry.NodeID.String()
	}

	payload := map[string]*pb.Value{
		"namespace": {Kind: &pb.Value_StringValue{StringValue: entry.Namespace}},
		"node_id":   {Kind: &pb.Value_StringValue{StringValue: nodeIDStr}},
		"text":      {Kind: &pb.Value_StringValue{StringValue: entry.Text}},
		"model_id":  {Kind: &pb.Value_StringValue{StringValue: entry.ModelID}},
	}

	vec := make([]float32, len(entry.Vector))
	copy(vec, entry.Vector)

	_, err := v.points.Upsert(ctx, &pb.UpsertPoints{
		CollectionName: v.collectionName(entry.Namespace),
		Points: []*pb.PointStruct{
			{
				Id:      &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: pointID}},
				Vectors: &pb.Vectors{VectorsOptions: &pb.Vectors_Vector{Vector: &pb.Vector{Data: vec}}},
				Payload: payload,
			},
		},
	})
	return err
}

func (v *VectorIndex) Delete(ctx context.Context, ns string, id uuid.UUID) error {
	if err := v.ensureCollection(ctx, ns); err != nil {
		return err
	}

	pointID := id.String()
	_, err := v.points.Delete(ctx, &pb.DeletePoints{
		CollectionName: v.collectionName(ns),
		Points: &pb.PointsSelector{
			PointsSelectorOneOf: &pb.PointsSelector_Points{
				Points: &pb.PointsIdsList{
					Ids: []*pb.PointId{
						{PointIdOptions: &pb.PointId_Uuid{Uuid: pointID}},
					},
				},
			},
		},
	})
	return err
}

func (v *VectorIndex) Search(ctx context.Context, q store.VectorQuery) ([]core.ScoredNode, error) {
	if err := v.ensureCollection(ctx, q.Namespace); err != nil {
		return nil, err
	}

	topK := uint64(q.TopK)
	if topK == 0 {
		topK = 20
	}

	vec := make([]float32, len(q.Vector))
	copy(vec, q.Vector)

	// Build label filter if specified
	var filter *pb.Filter
	if len(q.Labels) > 0 {
		var conditions []*pb.Condition
		for _, label := range q.Labels {
			conditions = append(conditions, &pb.Condition{
				ConditionOneOf: &pb.Condition_Field{
					Field: &pb.FieldCondition{
						Key: "labels",
						Match: &pb.Match{
							MatchValue: &pb.Match_Keyword{Keyword: label},
						},
					},
				},
			})
		}
		filter = &pb.Filter{Must: conditions}
	}

	resp, err := v.points.Search(ctx, &pb.SearchPoints{
		CollectionName: v.collectionName(q.Namespace),
		Vector:         vec,
		Limit:          topK,
		Filter:         filter,
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant search: %w", err)
	}

	results := make([]core.ScoredNode, 0, len(resp.Result))
	for _, pt := range resp.Result {
		props := make(map[string]any)
		if text, ok := pt.Payload["text"]; ok {
			if sv, ok := text.Kind.(*pb.Value_StringValue); ok {
				props["text"] = sv.StringValue
			}
		}

		nodeID := uuid.Nil
		if nid, ok := pt.Payload["node_id"]; ok {
			if sv, ok := nid.Kind.(*pb.Value_StringValue); ok {
				if parsed, err := uuid.Parse(sv.StringValue); err == nil {
					nodeID = parsed
				}
			}
		}

		results = append(results, core.ScoredNode{
			Node: core.Node{
				ID:         nodeID,
				Namespace:  q.Namespace,
				Properties: props,
			},
			Score:           float64(pt.Score),
			SimilarityScore: float64(pt.Score),
			RetrievalSource: "vector",
		})
	}

	return results, nil
}
