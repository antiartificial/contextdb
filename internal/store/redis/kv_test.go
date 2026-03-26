//go:build integration
// +build integration

package redis

import (
	"context"
	"os"
	"testing"

	"github.com/matryer/is"
)

func redisAddr() string {
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		return addr
	}
	return "localhost:6379"
}

func TestRedisKVStore(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	kv, err := NewKVStore(redisAddr(), "test:")
	is.NoErr(err)
	defer kv.Close()

	// Set
	err = kv.Set(ctx, "hello", []byte("world"), 0)
	is.NoErr(err)

	// Get
	val, err := kv.Get(ctx, "hello")
	is.NoErr(err)
	is.Equal(string(val), "world")

	// Delete
	err = kv.Delete(ctx, "hello")
	is.NoErr(err)

	val, err = kv.Get(ctx, "hello")
	is.NoErr(err)
	is.True(val == nil)

	// TTL
	err = kv.Set(ctx, "ttl-key", []byte("expires"), 1)
	is.NoErr(err)

	val, err = kv.Get(ctx, "ttl-key")
	is.NoErr(err)
	is.Equal(string(val), "expires")
}
