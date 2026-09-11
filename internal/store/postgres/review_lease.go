package postgres

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/antiartificial/contextdb/internal/store"
)

// AcquireReviewLease holds a PostgreSQL session advisory lock on one dedicated
// pooled connection. The connection remains checked out for the worker cycle;
// releasing it unlocks the namespace for another replica.
func (g *GraphStore) AcquireReviewLease(ctx context.Context, namespace string) (func() error, error) {
	conn, err := g.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire review lease connection: %w", err)
	}
	key := reviewLeaseKey(namespace)
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&locked); err != nil {
		conn.Release()
		return nil, fmt.Errorf("acquire review lease: %w", err)
	}
	if !locked {
		conn.Release()
		return nil, store.ErrReviewLeaseBusy
	}
	return func() error {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var unlocked bool
		err := conn.QueryRow(unlockCtx, "SELECT pg_advisory_unlock($1)", key).Scan(&unlocked)
		if err != nil || !unlocked {
			// Do not return a connection with an uncertain session lock to the pool.
			raw := conn.Hijack()
			_ = raw.Close(unlockCtx)
			if err != nil {
				return fmt.Errorf("release review lease: %w", err)
			}
			return fmt.Errorf("release review lease: advisory lock was not held")
		}
		conn.Release()
		return nil
	}, nil
}

func reviewLeaseKey(namespace string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("contextdb/review-worker/" + namespace))
	return int64(h.Sum64())
}
