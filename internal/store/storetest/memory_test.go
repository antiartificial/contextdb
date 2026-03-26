package storetest_test

import (
	"testing"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
	"github.com/antiartificial/contextdb/internal/store/storetest"
)

func TestMemoryGraphStore(t *testing.T) {
	storetest.RunGraphStoreTests(t, func(t *testing.T) store.GraphStore {
		return memstore.NewGraphStore()
	})
}

func TestMemoryVectorIndex(t *testing.T) {
	storetest.RunVectorIndexTests(t, func(t *testing.T) (store.VectorIndex, func(core.Node)) {
		vi := memstore.NewVectorIndex()
		return vi, vi.RegisterNode
	})
}

func TestMemoryKVStore(t *testing.T) {
	storetest.RunKVStoreTests(t, func(t *testing.T) store.KVStore {
		return memstore.NewKVStore()
	})
}

func TestMemoryEventLog(t *testing.T) {
	storetest.RunEventLogTests(t, func(t *testing.T) store.EventLog {
		return memstore.NewEventLog()
	})
}
