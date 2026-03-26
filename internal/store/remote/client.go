// Package remote implements store interfaces backed by gRPC calls
// to a contextdb server. Used by ModeRemote.
package remote

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const serviceName = "contextdb.v1.ContextDB"

// Client holds a gRPC connection to a contextdb server.
type Client struct {
	conn *grpc.ClientConn
}

// NewClient connects to a contextdb gRPC server at addr.
func NewClient(ctx context.Context, addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(jsonCodec{})),
	)
	if err != nil {
		return nil, fmt.Errorf("remote: dial %s: %w", addr, err)
	}
	return &Client{conn: conn}, nil
}

// Close closes the gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Graph returns a remote GraphStore.
func (c *Client) Graph() *GraphStore {
	return &GraphStore{conn: c.conn}
}

// Vectors returns a remote VectorIndex.
func (c *Client) Vectors() *VectorIndex {
	return &VectorIndex{conn: c.conn}
}

// KV returns a remote KVStore.
func (c *Client) KV() *KVStore {
	return &KVStore{conn: c.conn}
}

// EventLog returns a remote EventLog.
func (c *Client) EventLog() *EventLogStore {
	return &EventLogStore{conn: c.conn}
}

// invoke calls a gRPC method with JSON encoding.
func invoke(ctx context.Context, conn *grpc.ClientConn, method string, req, resp interface{}) error {
	fullMethod := "/" + serviceName + "/" + method
	return conn.Invoke(ctx, fullMethod, req, resp)
}

// jsonCodec is the client-side JSON codec matching the server's GRPCCodec.
type jsonCodec struct{}

func (jsonCodec) Name() string                         { return "json" }
func (jsonCodec) Marshal(v interface{}) ([]byte, error) { return json.Marshal(v) }
func (jsonCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
