// Package db provides the durable key-value database built on the pager and
// B+tree, with snapshot transactions and atomic commits.
package db

import (
	"errors"
	"sync"

	"github.com/Youssef-joe/YousDB/engine/btree"
	"github.com/Youssef-joe/YousDB/engine/kv"
	"github.com/Youssef-joe/YousDB/engine/pager"
)

var (
	// ErrNotFound is returned when a key does not exist.
	ErrNotFound = kv.ErrNotFound
	// ErrClosed is returned after the database has been closed.
	ErrClosed = errors.New("db: closed")
)

// Stats describes the current on-disk state.
type Stats struct {
	TxID      uint64
	Root      uint64
	Pages     uint64
	FreePages int
}

// DB is a durable ordered key-value store.
type DB struct {
	mu     sync.RWMutex
	pager  *pager.Pager
	closed bool
}

// Options configures a DB.
type Options struct {
	// NoSync skips fsync. It is intended for tests and benchmarks.
	NoSync bool
}

// Open opens or creates a database file.
func Open(path string) (*DB, error) {
	return OpenWithOptions(path, Options{})
}

// OpenWithOptions opens or creates a database file with explicit options.
func OpenWithOptions(path string, opts Options) (*DB, error) {
	p, err := pager.OpenWithOptions(path, pager.Options{NoSync: opts.NoSync})
	if err != nil {
		return nil, err
	}
	return &DB{pager: p}, nil
}

// Close closes the database. Every operation is already committed.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return nil
	}
	db.closed = true
	return db.pager.Close()
}

func (db *DB) treeAt(root uint64) *btree.Tree {
	p := db.pager
	return btree.New(
		root,
		p.Get,
		func(data []byte) uint64 {
			pageNo := p.Alloc()
			p.Put(pageNo, data)
			return pageNo
		},
		p.Free,
	)
}

// Get returns a copy of the value for key.
func (db *DB) Get(key []byte) ([]byte, error) {
	var out []byte
	err := db.View(func(tx *Tx) error {
		value, err := tx.Get(key)
		if err != nil {
			return err
		}
		out = value
		return nil
	})
	return out, err
}

// Set inserts or replaces key.
func (db *DB) Set(key, value []byte) error {
	return db.Update(func(tx *Tx) error {
		return tx.Set(key, value)
	})
}

// Delete removes key and reports whether it existed.
func (db *DB) Delete(key []byte) (bool, error) {
	removed := false
	err := db.Update(func(tx *Tx) error {
		removed = tx.Delete(key)
		return nil
	})
	return removed, err
}

// Scan visits keys in [start, end) in order. Nil bounds are unbounded. The
// callback receives slices that are only valid during the call and returns
// false to stop.
func (db *DB) Scan(start, end []byte, fn func(key, value []byte) bool) error {
	return db.View(func(tx *Tx) error {
		return tx.Scan(start, end, fn)
	})
}

// Pairs returns a copy of all pairs in [start, end). Nil bounds are unbounded.
func (db *DB) Pairs(start, end []byte) ([]kv.Pair, error) {
	var pairs []kv.Pair
	err := db.View(func(tx *Tx) error {
		return tx.Scan(start, end, func(key, value []byte) bool {
			pairs = append(pairs, kv.Pair{
				Key:   string(key),
				Value: append([]byte(nil), value...),
			})
			return true
		})
	})
	return pairs, err
}

// Stats returns the committed state.
func (db *DB) Stats() Stats {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return Stats{
		TxID:      db.pager.TxID(),
		Root:      db.pager.Root(),
		Pages:     db.pager.PageCount(),
		FreePages: db.pager.FreeCount(),
	}
}
