package badger

import (
	"context"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"
)

const prefixKV = "kv/"

// KVStore implements store.KVStore backed by BadgerDB.
type KVStore struct {
	db *badgerdb.DB
}

// NewKVStore returns a KVStore backed by the given BadgerDB instance.
func NewKVStore(db *badgerdb.DB) *KVStore {
	return &KVStore{db: db}
}

func kvKey(key string) []byte {
	return []byte(prefixKV + key)
}

func (k *KVStore) Get(_ context.Context, key string) ([]byte, error) {
	var val []byte
	err := k.db.View(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(kvKey(key))
		if err == badgerdb.ErrKeyNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		val, err = item.ValueCopy(nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (k *KVStore) Set(_ context.Context, key string, val []byte, ttlSeconds int) error {
	return k.db.Update(func(txn *badgerdb.Txn) error {
		entry := badgerdb.NewEntry(kvKey(key), val)
		if ttlSeconds > 0 {
			entry = entry.WithTTL(time.Duration(ttlSeconds) * time.Second)
		}
		return txn.SetEntry(entry)
	})
}

func (k *KVStore) Delete(_ context.Context, key string) error {
	return k.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Delete(kvKey(key))
	})
}
