// Package db provides the durable key-value database built on the pager and
// B+tree. Operations are autocommitted until the transaction API is used.
package db

import (
	"errors"
	"fmt"
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

// Close flushes nothing and closes the file. Every operation is already
// committed.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return nil
	}
	db.closed = true
	return db.pager.Close()
}

func (db *DB) tree() *btree.Tree {
	p := db.pager
	return btree.New(
		p.Root(),
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
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return nil, ErrClosed
	}
	value, ok := db.tree().Get(key)
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

// Set inserts or replaces key.
func (db *DB) Set(key, value []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return ErrClosed
	}
	return db.update(func(tree *btree.Tree) error {
		return tree.Insert(key, value)
	})
}

// Delete removes key and reports whether it existed.
func (db *DB) Delete(key []byte) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return false, ErrClosed
	}
	removed := false
	err := db.update(func(tree *btree.Tree) error {
		removed = tree.Delete(key)
		return nil
	})
	return removed, err
}

// Scan visits keys in [start, end) in order. Nil bounds are unbounded. The
// callback receives slices that are only valid during the call and returns
// false to stop.
func (db *DB) Scan(start, end []byte, fn func(key, value []byte) bool) error {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return ErrClosed
	}
	db.tree().Scan(start, end, fn)
	return nil
}

// Pairs returns a copy of all pairs in [start, end). Nil bounds are unbounded.
func (db *DB) Pairs(start, end []byte) ([]kv.Pair, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return nil, ErrClosed
	}
	var pairs []kv.Pair
	db.tree().Scan(start, end, func(key, value []byte) bool {
		pairs = append(pairs, kv.Pair{
			Key:   string(key),
			Value: append([]byte(nil), value...),
		})
		return true
	})
	return pairs, nil
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

func (db *DB) update(fn func(tree *btree.Tree) error) error {
	if err := db.pager.Begin(); err != nil {
		return err
	}
	tree := db.tree()
	if err := fn(tree); err != nil {
		db.pager.Abort()
		return err
	}
	db.pager.SetRoot(tree.Root())
	if err := db.pager.Commit(); err != nil {
		db.pager.Abort()
		return fmt.Errorf("db: commit: %w", err)
	}
	return nil
}
