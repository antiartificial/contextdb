package badger_test

import (
	"testing"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
	badgerstore "github.com/antiartificial/contextdb/internal/store/badger"
	"github.com/antiartificial/contextdb/internal/store/storetest"
)

func openTestDB(t *testing.T) *badgerstore.DB {
	t.Helper()
	db, err := badgerstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestBadgerGraphStore(t *testing.T) {
	storetest.RunGraphStoreTests(t, func(t *testing.T) store.GraphStore {
		return badgerstore.NewGraphStore(openTestDB(t).Inner())
	})
}

func TestBadgerVectorIndex(t *testing.T) {
	storetest.RunVectorIndexTests(t, func(t *testing.T) (store.VectorIndex, func(core.Node)) {
		vi := badgerstore.NewVectorIndex(openTestDB(t).Inner(), badgerstore.HNSWConfig{})
		return vi, vi.RegisterNode
	})
}

func TestBadgerKVStore(t *testing.T) {
	storetest.RunKVStoreTests(t, func(t *testing.T) store.KVStore {
		return badgerstore.NewKVStore(openTestDB(t).Inner())
	})
}

func TestBadgerEventLog(t *testing.T) {
	storetest.RunEventLogTests(t, func(t *testing.T) store.EventLog {
		return badgerstore.NewEventLog(openTestDB(t).Inner())
	})
}
