// Package badger provides persistent store implementations backed by BadgerDB.
// All four store interfaces (GraphStore, VectorIndex, KVStore, EventLog) share
// a single BadgerDB instance, using key prefixes for isolation.
package badger

import (
	"fmt"

	badgerdb "github.com/dgraph-io/badger/v4"
)

// DB wraps a shared BadgerDB instance.
type DB struct {
	inner *badgerdb.DB
	path  string
}

// Open opens or creates a BadgerDB at the given directory path.
func Open(path string) (*DB, error) {
	opts := badgerdb.DefaultOptions(path).
		WithLogger(nil) // silence BadgerDB's default logger
	db, err := badgerdb.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("badger open %q: %w", path, err)
	}
	return &DB{inner: db, path: path}, nil
}

// Inner returns the underlying BadgerDB instance.
func (d *DB) Inner() *badgerdb.DB { return d.inner }

// Close closes the BadgerDB instance.
func (d *DB) Close() error { return d.inner.Close() }
