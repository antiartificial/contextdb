package client

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/antiartificial/contextdb/internal/store"
)

// lockIdempotency serializes same-process retries of one caller key. The
// optional Postgres lease below extends that serialization across replicas.
type idempotencyLock struct {
	mu   sync.Mutex
	refs int
}

func (h *NamespaceHandle) lockIdempotency(key string) func() {
	h.idempotencyMu.Lock()
	if h.idempotencyLocks == nil {
		h.idempotencyLocks = make(map[string]*idempotencyLock)
	}
	entry := h.idempotencyLocks[key]
	if entry == nil {
		entry = &idempotencyLock{}
		h.idempotencyLocks[key] = entry
	}
	entry.refs++ // includes holders and goroutines waiting for this key
	h.idempotencyMu.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		h.idempotencyMu.Lock()
		entry.refs--
		if entry.refs == 0 && h.idempotencyLocks[key] == entry {
			delete(h.idempotencyLocks, key)
		}
		h.idempotencyMu.Unlock()
	}
}

func (h *NamespaceHandle) acquireCoordinationLease(ctx context.Context, scope, key string) (func(), error) {
	leases, ok := h.db.graph.(store.ReviewLeaseStore)
	if !ok {
		return func() {}, nil
	}
	release, err := leases.AcquireReviewLease(ctx, h.cfg.ID+":"+scope+":"+strings.TrimSpace(key))
	if err != nil {
		return nil, fmt.Errorf("acquire %s lease: %w", scope, err)
	}
	return func() {
		if err := release(); err != nil {
			h.db.logger.Error("release coordination lease", "scope", scope, "error", err)
		}
	}, nil
}
