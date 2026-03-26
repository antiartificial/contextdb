// Package redis implements KVStore and EventLog using Redis.
//
//go:build integration
// +build integration

package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// KVStore implements store.KVStore using Redis.
// Native TTL via SET EX.
type KVStore struct {
	client *redis.Client
	prefix string
}

// NewKVStore connects to Redis and returns a KVStore.
func NewKVStore(addr, prefix string) (*KVStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	if prefix == "" {
		prefix = "contextdb:kv:"
	}
	return &KVStore{client: client, prefix: prefix}, nil
}

// Close closes the Redis connection.
func (k *KVStore) Close() error {
	return k.client.Close()
}

func (k *KVStore) key(key string) string {
	return k.prefix + key
}

func (k *KVStore) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := k.client.Get(ctx, k.key(key)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (k *KVStore) Set(ctx context.Context, key string, val []byte, ttlSeconds int) error {
	var ttl time.Duration
	if ttlSeconds > 0 {
		ttl = time.Duration(ttlSeconds) * time.Second
	}
	return k.client.Set(ctx, k.key(key), val, ttl).Err()
}

func (k *KVStore) Delete(ctx context.Context, key string) error {
	return k.client.Del(ctx, k.key(key)).Err()
}
