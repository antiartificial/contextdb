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
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/pkg/client"
)

// contextDBService is the interface required by gRPC's RegisterService.
type contextDBService interface{}

// GRPCService implements the contextdb gRPC service.
// Since we don't rely on protoc-generated code, this uses a manual
// service registration approach with JSON-encoded messages over gRPC.
type GRPCService struct {
	db    *client.DB
	graph store.GraphStore
	vecs  store.VectorIndex
	kv    store.KVStore
	log   store.EventLog
}

// NewGRPCService returns a gRPC service backed by the given DB.
func NewGRPCService(db *client.DB) *GRPCService {
	graph, vecs, kv, log := db.Stores()
	return &GRPCService{db: db, graph: graph, vecs: vecs, kv: kv, log: log}
}

// Register registers all RPC methods with the gRPC server.
func (s *GRPCService) Register(srv *grpc.Server) {
	// Register service descriptor for reflection.
	// Since we don't have generated code, we use unary handlers directly.
	desc := grpc.ServiceDesc{
		ServiceName: "contextdb.v1.ContextDB",
		HandlerType: (*contextDBService)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "Write", Handler: s.handleWrite},
			{MethodName: "Retrieve", Handler: s.handleRetrieve},
			{MethodName: "IngestText", Handler: s.handleIngestText},
			{MethodName: "LabelSource", Handler: s.handleLabelSource},
			{MethodName: "Ping", Handler: s.handlePing},
			// Low-level store methods for ModeRemote
			{MethodName: "GetNode", Handler: s.handleGetNode},
			{MethodName: "UpsertNode", Handler: s.handleUpsertNode},
			{MethodName: "NodeHistory", Handler: s.handleNodeHistory},
			{MethodName: "UpsertEdge", Handler: s.handleUpsertEdge},
			{MethodName: "InvalidateEdge", Handler: s.handleInvalidateEdge},
			{MethodName: "Edges", Handler: s.handleEdges},
			{MethodName: "WalkGraph", Handler: s.handleWalkGraph},
			{MethodName: "ManageSource", Handler: s.handleManageSource},
			{MethodName: "VectorIndex", Handler: s.handleVectorIndex},
			{MethodName: "VectorSearch", Handler: s.handleVectorSearch},
			{MethodName: "KV", Handler: s.handleKV},
			{MethodName: "EventAppend", Handler: s.handleEventAppend},
			{MethodName: "EventSince", Handler: s.handleEventSince},
			{MethodName: "EventMarkProcessed", Handler: s.handleEventMarkProcessed},
		},
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "StreamRetrieve",
				Handler:       s.handleStreamRetrieve,
				ServerStreams:  true,
				ClientStreams:  false,
			},
		},
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
	Text        string       `json:"text"`
	SeedIDs     []string     `json:"seed_ids"`
	TopK        int          `json:"top_k"`
	Labels      []string     `json:"labels"`
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
		Text:        req.Text,
		SeedIDs:     seedIDs,
		TopK:        req.TopK,
		Labels:      req.Labels,
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

// ─── Streaming handlers ───────────────────────────────────────────────────────

func (s *GRPCService) handleStreamRetrieve(srv interface{}, stream grpc.ServerStream) error {
	var req GRPCRetrieveRequest
	if err := stream.RecvMsg(&req); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	ctx := stream.Context()
	tenant := TenantFromContext(ctx)
	ns := req.Namespace
	if tenant != "" {
		ns = tenant + "/" + ns
	}

	h := s.db.Namespace(ns, namespace.ModeGeneral)

	var seedIDs []uuid.UUID
	for _, sid := range req.SeedIDs {
		id, err := uuid.Parse(sid)
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
		Text:        req.Text,
		SeedIDs:     seedIDs,
		TopK:        req.TopK,
		Labels:      req.Labels,
		ScoreParams: sp,
		AsOf:        asOf,
	})
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}

	// Stream each result individually
	for _, r := range results {
		resp := scoredNodeResponse{
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
		if err := stream.SendMsg(&resp); err != nil {
			return err
		}
	}
	return nil
}

// ─── Low-level store message types ────────────────────────────────────────────

type grpcGetNodeReq struct {
	Namespace string `json:"namespace"`
	ID        string `json:"id"`
}

type grpcNodeResp struct {
	Node  *core.Node `json:"node"`
	Found bool       `json:"found"`
}

type grpcUpsertNodeReq struct {
	Node core.Node `json:"node"`
}

type grpcHistoryReq struct {
	Namespace string `json:"namespace"`
	ID        string `json:"id"`
}

type grpcHistoryResp struct {
	Nodes []core.Node `json:"nodes"`
}

type grpcUpsertEdgeReq struct {
	Edge core.Edge `json:"edge"`
}

type grpcInvalidateEdgeReq struct {
	Namespace string    `json:"namespace"`
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
}

type grpcEdgesReq struct {
	Namespace string   `json:"namespace"`
	NodeID    string   `json:"node_id"`
	Direction string   `json:"direction"` // "from" or "to"
	EdgeTypes []string `json:"edge_types"`
}

type grpcEdgesResp struct {
	Edges []core.Edge `json:"edges"`
}

type grpcWalkReq struct {
	Namespace string   `json:"namespace"`
	SeedIDs   []string `json:"seed_ids"`
	EdgeTypes []string `json:"edge_types"`
	MaxDepth  int      `json:"max_depth"`
	Strategy  string   `json:"strategy"`
	MinWeight float64  `json:"min_weight"`
}

type grpcWalkResp struct {
	Nodes []core.Node `json:"nodes"`
}

type grpcManageSourceReq struct {
	Action     string      `json:"action"` // "upsert", "get", "update_credibility"
	Source     core.Source `json:"source"`
	Namespace  string      `json:"namespace"`
	ExternalID string      `json:"external_id"`
	Delta      float64     `json:"delta"`
	SourceID   string      `json:"source_id"`
}

type grpcManageSourceResp struct {
	Source *core.Source `json:"source"`
}

type grpcVectorIndexReq struct {
	Action string           `json:"action"` // "index" or "delete"
	Entry  core.VectorEntry `json:"entry"`
	NS     string           `json:"namespace"`
	ID     string           `json:"id"`
}

type grpcVectorSearchReq struct {
	Query store.VectorQuery `json:"query"`
}

type grpcVectorSearchResp struct {
	Results []core.ScoredNode `json:"results"`
}

type grpcKVReq struct {
	Action string `json:"action"` // "get", "set", "delete"
	Key    string `json:"key"`
	Value  []byte `json:"value"`
	TTL    int    `json:"ttl"`
}

type grpcKVResp struct {
	Value []byte `json:"value"`
}

type grpcEventAppendReq struct {
	Event store.Event `json:"event"`
}

type grpcEventSinceReq struct {
	Namespace string    `json:"namespace"`
	After     time.Time `json:"after"`
}

type grpcEventSinceResp struct {
	Events []store.Event `json:"events"`
}

type grpcEventMarkProcessedReq struct {
	EventID string `json:"event_id"`
}

// ─── Low-level store handlers ─────────────────────────────────────────────────

func (s *GRPCService) handleGetNode(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcGetNodeReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id: "+err.Error())
	}
	node, err := s.graph.GetNode(ctx, req.Namespace, id)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &grpcNodeResp{Node: node, Found: node != nil}, nil
}

func (s *GRPCService) handleUpsertNode(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcUpsertNodeReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.graph.UpsertNode(ctx, req.Node); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &struct{}{}, nil
}

func (s *GRPCService) handleNodeHistory(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcHistoryReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id: "+err.Error())
	}
	nodes, err := s.graph.History(ctx, req.Namespace, id)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &grpcHistoryResp{Nodes: nodes}, nil
}

func (s *GRPCService) handleUpsertEdge(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcUpsertEdgeReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.graph.UpsertEdge(ctx, req.Edge); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &struct{}{}, nil
}

func (s *GRPCService) handleInvalidateEdge(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcInvalidateEdgeReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id: "+err.Error())
	}
	if err := s.graph.InvalidateEdge(ctx, req.Namespace, id, req.At); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &struct{}{}, nil
}

func (s *GRPCService) handleEdges(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcEdgesReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	nodeID, err := uuid.Parse(req.NodeID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid node_id: "+err.Error())
	}
	var edges []core.Edge
	switch req.Direction {
	case "to":
		edges, err = s.graph.EdgesTo(ctx, req.Namespace, nodeID, req.EdgeTypes)
	default:
		edges, err = s.graph.EdgesFrom(ctx, req.Namespace, nodeID, req.EdgeTypes)
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &grpcEdgesResp{Edges: edges}, nil
}

func (s *GRPCService) handleWalkGraph(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcWalkReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	var seedIDs []uuid.UUID
	for _, sid := range req.SeedIDs {
		id, err := uuid.Parse(sid)
		if err != nil {
			continue
		}
		seedIDs = append(seedIDs, id)
	}
	nodes, err := s.graph.Walk(ctx, store.WalkQuery{
		Namespace: req.Namespace,
		SeedIDs:   seedIDs,
		EdgeTypes: req.EdgeTypes,
		MaxDepth:  req.MaxDepth,
		Strategy:  store.TraversalStrategy(req.Strategy),
		MinWeight: req.MinWeight,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &grpcWalkResp{Nodes: nodes}, nil
}

func (s *GRPCService) handleManageSource(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcManageSourceReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	switch req.Action {
	case "upsert":
		if err := s.graph.UpsertSource(ctx, req.Source); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &grpcManageSourceResp{}, nil
	case "get":
		src, err := s.graph.GetSourceByExternalID(ctx, req.Namespace, req.ExternalID)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &grpcManageSourceResp{Source: src}, nil
	case "update_credibility":
		id, err := uuid.Parse(req.SourceID)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid source_id: "+err.Error())
		}
		if err := s.graph.UpdateCredibility(ctx, req.Namespace, id, req.Delta); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &grpcManageSourceResp{}, nil
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown action: "+req.Action)
	}
}

func (s *GRPCService) handleVectorIndex(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcVectorIndexReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	switch req.Action {
	case "index":
		if err := s.vecs.Index(ctx, req.Entry); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &struct{}{}, nil
	case "delete":
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid id: "+err.Error())
		}
		if err := s.vecs.Delete(ctx, req.NS, id); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &struct{}{}, nil
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown action: "+req.Action)
	}
}

func (s *GRPCService) handleVectorSearch(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcVectorSearchReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	results, err := s.vecs.Search(ctx, req.Query)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &grpcVectorSearchResp{Results: results}, nil
}

func (s *GRPCService) handleKV(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcKVReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	switch req.Action {
	case "get":
		val, err := s.kv.Get(ctx, req.Key)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &grpcKVResp{Value: val}, nil
	case "set":
		if err := s.kv.Set(ctx, req.Key, req.Value, req.TTL); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &grpcKVResp{}, nil
	case "delete":
		if err := s.kv.Delete(ctx, req.Key); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &grpcKVResp{}, nil
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown action: "+req.Action)
	}
}

func (s *GRPCService) handleEventAppend(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcEventAppendReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.log.Append(ctx, req.Event); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &struct{}{}, nil
}

func (s *GRPCService) handleEventSince(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcEventSinceReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	events, err := s.log.Since(ctx, req.Namespace, req.After)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &grpcEventSinceResp{Events: events}, nil
}

func (s *GRPCService) handleEventMarkProcessed(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req grpcEventMarkProcessedReq
	if err := dec(&req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	id, err := uuid.Parse(req.EventID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid event_id: "+err.Error())
	}
	if err := s.log.MarkProcessed(ctx, id); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &struct{}{}, nil
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
