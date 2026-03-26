package remote

import (
	"context"

	"google.golang.org/grpc"
)

// KVStore implements store.KVStore via gRPC.
type KVStore struct {
	conn *grpc.ClientConn
}

func (k *KVStore) Get(ctx context.Context, key string) ([]byte, error) {
	var resp struct {
		Value []byte `json:"value"`
	}
	err := invoke(ctx, k.conn, "KV", &struct {
		Action string `json:"action"`
		Key    string `json:"key"`
	}{Action: "get", Key: key}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Value, nil
}

func (k *KVStore) Set(ctx context.Context, key string, val []byte, ttlSeconds int) error {
	var resp struct {
		Value []byte `json:"value"`
	}
	return invoke(ctx, k.conn, "KV", &struct {
		Action string `json:"action"`
		Key    string `json:"key"`
		Value  []byte `json:"value"`
		TTL    int    `json:"ttl"`
	}{Action: "set", Key: key, Value: val, TTL: ttlSeconds}, &resp)
}

func (k *KVStore) Delete(ctx context.Context, key string) error {
	var resp struct {
		Value []byte `json:"value"`
	}
	return invoke(ctx, k.conn, "KV", &struct {
		Action string `json:"action"`
		Key    string `json:"key"`
	}{Action: "delete", Key: key}, &resp)
}
