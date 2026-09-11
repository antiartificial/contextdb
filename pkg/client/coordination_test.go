package client

import (
	"testing"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/matryer/is"
)

func TestIdempotencyLockRegistryReleasesUnusedKeys(t *testing.T) {
	is := is.New(t)
	db := MustOpen(Options{Mode: ModeEmbedded})
	defer db.Close()
	h := db.Namespace("idempotency-locks", namespace.ModeGeneral)
	for i := 0; i < 1000; i++ {
		unlock := h.lockIdempotency(string(rune(i)))
		unlock()
	}
	h.idempotencyMu.Lock()
	defer h.idempotencyMu.Unlock()
	is.Equal(len(h.idempotencyLocks), 0)
}
