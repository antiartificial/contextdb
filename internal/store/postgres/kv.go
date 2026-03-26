package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// KVStore implements store.KVStore backed by PostgreSQL.
type KVStore struct {
	pool *pgxpool.Pool
}

// NewKVStore returns a KVStore backed by the given connection pool.
func NewKVStore(pool *pgxpool.Pool) *KVStore {
	return &KVStore{pool: pool}
}

func (k *KVStore) Get(ctx context.Context, key string) ([]byte, error) {
	var val []byte
	var expiresAt *time.Time
	err := k.pool.QueryRow(ctx, "SELECT value, expires_at FROM kv_store WHERE key = $1", key).
		Scan(&val, &expiresAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if expiresAt != nil && time.Now().After(*expiresAt) {
		// expired — clean up lazily
		_, _ = k.pool.Exec(ctx, "DELETE FROM kv_store WHERE key = $1", key)
		return nil, nil
	}
	return val, nil
}

func (k *KVStore) Set(ctx context.Context, key string, val []byte, ttlSeconds int) error {
	var expiresAt *time.Time
	if ttlSeconds > 0 {
		t := time.Now().Add(time.Duration(ttlSeconds) * time.Second)
		expiresAt = &t
	}
	_, err := k.pool.Exec(ctx, `
		INSERT INTO kv_store (key, value, expires_at) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, expires_at = EXCLUDED.expires_at
	`, key, val, expiresAt)
	return err
}

func (k *KVStore) Delete(ctx context.Context, key string) error {
	_, err := k.pool.Exec(ctx, "DELETE FROM kv_store WHERE key = $1", key)
	return err
}
