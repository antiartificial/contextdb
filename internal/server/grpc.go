package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/pkg/client"
)

// GRPCService implements the contextdb gRPC service.
// Since we don't rely on protoc-generated code, this uses a manual
// service registration approach with JSON-encoded messages over gRPC.
type GRPCService struct {
	db *client.DB
}

// NewGRPCService returns a gRPC service backed by the given DB.
func NewGRPCService(db *client.DB) *GRPCService {
	return &GRPCService{db: db}
}

// Register registers all RPC methods with the gRPC server.
func (s *GRPCService) Register(srv *grpc.Server) {
	// Register service descriptor for reflection.
	// Since we don't have generated code, we use unary handlers directly.
	desc := grpc.ServiceDesc{
		ServiceName: "contextdb.v1.ContextDB",
		HandlerType: (*GRPCService)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "Write", Handler: s.handleWrite},
			{MethodName: "Retrieve", Handler: s.handleRetrieve},
			{MethodName: "IngestText", Handler: s.handleIngestText},
			{MethodName: "LabelSource", Handler: s.handleLabelSource},
			{MethodName: "Ping", Handler: s.handlePing},
		},
		Streams: []grpc.StreamDesc{},
	}
	srv.RegisterService(&desc, s)
}

// ─── gRPC message types ──────────────────────────────────────────────────────

// GRPCWriteRequest is the gRPC-friendly write request.
type GRPCWriteRequest struct {
	Namespace     string            `json:"namespace"`
	NamespaceMode string            `json:"namespace_mode"`
	Content       string            `json:"content"`
	SourceID      string            `json:"source_id"`
	Labels        []string          `json:"labels"`
	Properties    map[string]string `json:"properties"`
	Vector        []float32         `json:"vector"`
	ModelID       string            `json:"model_id"`
	Confidence    float64           `json:"confidence"`
	ValidFrom     *time.Time        `json:"valid_from,omitempty"`
}

// GRPCWriteResponse is the gRPC write response.
type GRPCWriteResponse struct {
	NodeID      string   `json:"node_id"`
	Admitted    bool     `json:"admitted"`
	Reason      string   `json:"reason,omitempty"`
	ConflictIDs []string `json:"conflict_ids,omitempty"`
}

// GRPCRetrieveRequest is the gRPC retrieve request.
type GRPCRetrieveRequest struct {
	Namespace   string       `json:"namespace"`
	Vector      []float32    `json:"vector"`
	SeedIDs     []string     `json:"seed_ids"`
	TopK        int          `json:"top_k"`
	ScoreParams *scoreParams `json:"score_params,omitempty"`
	AsOf        *time.Time   `json:"as_of,omitempty"`
}

// GRPCRetrieveResponse is the gRPC retrieve response.
type GRPCRetrieveResponse struct {
	Results []scoredNodeResponse `json:"results"`
}

// GRPCIngestRequest is the gRPC ingest text request.
type GRPCIngestRequest struct {
	Namespace     string `json:"namespace"`
	NamespaceMode string `json:"namespace_mode"`
	Text          string `json:"text"`
	SourceID      string `json:"source_id"`
}

// GRPCIngestResponse is the gRPC ingest response.
type GRPCIngestResponse struct {
	NodesWritten int `json:"nodes_written"`
	EdgesWritten int `json:"edges_written"`
	Rejected     int `json:"rejected"`
}

// GRPCLabelSourceRequest is the gRPC label source request.
type GRPCLabelSourceRequest struct {
	Namespace     string   `json:"namespace"`
	NamespaceMode string   `json:"namespace_mode"`
	ExternalID    string   `json:"external_id"`
	Labels        []string `json:"labels"`
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func (s *GRPCService) handleWrite(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req GRPCWriteRequest
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	tenant := TenantFromContext(ctx)
	ns := req.Namespace
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	mode := resolveMode(req.NamespaceMode)
	h := s.db.Namespace(ns, mode)

	props := make(map[string]any)
	for k, v := range req.Properties {
		props[k] = v
	}

	var validFrom time.Time
	if req.ValidFrom != nil {
		validFrom = *req.ValidFrom
	}

	result, err := h.Write(ctx, client.WriteRequest{
		Content:    req.Content,
		SourceID:   req.SourceID,
		Labels:     req.Labels,
		Properties: props,
		Vector:     req.Vector,
		ModelID:    req.ModelID,
		Confidence: req.Confidence,
		ValidFrom:  validFrom,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	conflictIDs := make([]string, len(result.ConflictIDs))
	for i, id := range result.ConflictIDs {
		conflictIDs[i] = id.String()
	}

	return &GRPCWriteResponse{
		NodeID:      result.NodeID.String(),
		Admitted:    result.Admitted,
		Reason:      result.Reason,
		ConflictIDs: conflictIDs,
	}, nil
}

func (s *GRPCService) handleRetrieve(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req GRPCRetrieveRequest
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	tenant := TenantFromContext(ctx)
	ns := req.Namespace
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	h := s.db.Namespace(ns, namespace.ModeGeneral)

	var seedIDs []uuid.UUID
	for _, s := range req.SeedIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			continue
		}
		seedIDs = append(seedIDs, id)
	}

	var sp core.ScoreParams
	if req.ScoreParams != nil {
		sp = core.ScoreParams{
			SimilarityWeight: req.ScoreParams.SimilarityWeight,
			ConfidenceWeight: req.ScoreParams.ConfidenceWeight,
			RecencyWeight:    req.ScoreParams.RecencyWeight,
			UtilityWeight:    req.ScoreParams.UtilityWeight,
			DecayAlpha:       req.ScoreParams.DecayAlpha,
		}
	}

	var asOf time.Time
	if req.AsOf != nil {
		asOf = *req.AsOf
	}

	results, err := h.Retrieve(ctx, client.RetrieveRequest{
		Vector:      req.Vector,
		SeedIDs:     seedIDs,
		TopK:        req.TopK,
		ScoreParams: sp,
		AsOf:        asOf,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	resp := &GRPCRetrieveResponse{Results: make([]scoredNodeResponse, len(results))}
	for i, r := range results {
		resp.Results[i] = scoredNodeResponse{
			ID:              r.Node.ID.String(),
			Namespace:       r.Node.Namespace,
			Labels:          r.Node.Labels,
			Properties:      r.Node.Properties,
			Score:           r.Score,
			SimilarityScore: r.SimilarityScore,
			ConfidenceScore: r.ConfidenceScore,
			RecencyScore:    r.RecencyScore,
			UtilityScore:    r.UtilityScore,
			RetrievalSource: r.RetrievalSource,
		}
	}

	return resp, nil
}

func (s *GRPCService) handleIngestText(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req GRPCIngestRequest
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	tenant := TenantFromContext(ctx)
	ns := req.Namespace
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	mode := resolveMode(req.NamespaceMode)
	h := s.db.Namespace(ns, mode)

	result, err := h.IngestText(ctx, req.Text, req.SourceID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &GRPCIngestResponse{
		NodesWritten: result.NodesWritten,
		EdgesWritten: result.EdgesWritten,
		Rejected:     result.Rejected,
	}, nil
}

func (s *GRPCService) handleLabelSource(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req GRPCLabelSourceRequest
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	tenant := TenantFromContext(ctx)
	ns := req.Namespace
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	mode := resolveMode(req.NamespaceMode)
	h := s.db.Namespace(ns, mode)

	if err := h.LabelSource(ctx, req.ExternalID, req.Labels); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &struct{}{}, nil
}

func (s *GRPCService) handlePing(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req struct{}
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := s.db.Ping(ctx); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	return &struct{ Status string }{Status: "ok"}, nil
}

// GRPCCodec is a JSON codec for gRPC (used without protobuf codegen).
type GRPCCodec struct{}

func (GRPCCodec) Name() string { return "json" }

func (GRPCCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (GRPCCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// FormatGRPCCodec returns a grpc.ServerOption that uses JSON encoding.
func FormatGRPCCodec() grpc.ServerOption {
	return grpc.ForceServerCodec(GRPCCodec{})
}

// suppress unused import warning
var _ = fmt.Sprintf
