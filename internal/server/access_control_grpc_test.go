package server_test

import (
	"context"
	"net"
	"testing"

	"github.com/antiartificial/contextdb/internal/server"
	"github.com/antiartificial/contextdb/pkg/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCRegisteredServiceEnforcesTokenRegistry(t *testing.T) {
	db := client.MustOpen(client.Options{})
	defer db.Close()
	registry, err := server.NewTokenRegistry(`["acme:read:reader","acme:write:writer","acme:admin:root"]`)
	if err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer(server.FormatGRPCCodec(), grpc.ChainUnaryInterceptor(registry.GRPCInterceptor(), server.TenantInterceptor()), grpc.ChainStreamInterceptor(registry.GRPCStreamInterceptor()))
	server.NewGRPCService(db).Register(gs)
	go gs.Serve(lis)
	defer gs.Stop()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.ForceCodec(server.GRPCCodec{})))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	invoke := func(ctx context.Context, method string) error {
		return conn.Invoke(ctx, method, &server.GRPCWriteRequest{Namespace: "n", Content: "x", SourceID: "s"}, &server.GRPCWriteResponse{})
	}
	if code := status.Code(invoke(context.Background(), "/contextdb.v1.ContextDB/Write")); code != codes.Unauthenticated {
		t.Fatalf("anonymous write = %s", code)
	}
	read := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer acme:read:reader")
	if code := status.Code(invoke(read, "/contextdb.v1.ContextDB/Write")); code != codes.PermissionDenied {
		t.Fatalf("read write = %s", code)
	}
	if code := status.Code(invoke(metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer acme:read:forged"), "/contextdb.v1.ContextDB/Write")); code != codes.PermissionDenied {
		t.Fatalf("forged = %s", code)
	}
	write := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer acme:write:writer")
	if err := invoke(write, "/contextdb.v1.ContextDB/Write"); err != nil {
		t.Fatalf("valid write: %v", err)
	}
	if code := status.Code(invoke(read, "/contextdb.v1.ContextDB/GetNode")); code != codes.PermissionDenied {
		t.Fatalf("low-level read = %s", code)
	}
	stream, err := conn.NewStream(context.Background(), &grpc.StreamDesc{ServerStreams: true}, "/contextdb.v1.ContextDB/StreamRetrieve")
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.SendMsg(&server.GRPCRetrieveRequest{}); err != nil {
		t.Fatal(err)
	}
	if code := status.Code(stream.RecvMsg(&server.GRPCRetrieveResponse{})); code != codes.Unauthenticated {
		t.Fatalf("anonymous stream=%s", code)
	}
}
