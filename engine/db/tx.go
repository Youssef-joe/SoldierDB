package db

import (
	"errors"
	"fmt"
	"sort"

	"github.com/Youssef-joe/YousDB/engine/btree"
)

var (
	// ErrTxDone is returned when a transaction is used after its callback.
	ErrTxDone = errors.New("db: transaction is done")
	// ErrReadOnly is returned when writing through a read-only transaction.
	ErrReadOnly = errors.New("db: transaction is read-only")
)

type write struct {
	value   []byte
	deleted bool
}

// Tx is a database transaction. Reads see the snapshot from the start of the
// transaction plus the transaction's own buffered writes. Writes are applied
// atomically when an Update callback returns nil.
type Tx struct {
	db       *DB
	root     uint64
	writable bool
	writes   map[string]write
	done     bool
	unlock   func()
}

// Writable reports whether the transaction accepts writes.
func (tx *Tx) Writable() bool { return tx.writable }

// Get returns a copy of the value for key.
func (tx *Tx) Get(key []byte) ([]byte, error) {
	if tx.done {
		return nil, ErrTxDone
	}
	if w, ok := tx.writes[string(key)]; ok {
		if w.deleted {
			return nil, ErrNotFound
		}
		return append([]byte(nil), w.value...), nil
	}
	if tx.root == 0 {
		return nil, ErrNotFound
	}
	value, ok := tx.db.treeAt(tx.root).Get(key)
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

// Set inserts or replaces key in the transaction buffer.
func (tx *Tx) Set(key, value []byte) error {
	if tx.done {
		return ErrTxDone
	}
	if !tx.writable {
		return ErrReadOnly
	}
	if len(key) > btree.MaxKeySize {
		return fmt.Errorf("%w: %d > %d", btree.ErrKeyTooLarge, len(key), btree.MaxKeySize)
	}
	if len(value) > btree.MaxValueSize {
		return fmt.Errorf("%w: %d > %d", btree.ErrValueTooLarge, len(value), btree.MaxValueSize)
	}
	tx.writes[string(key)] = write{value: append([]byte(nil), value...)}
	return nil
}

// Delete removes key in the transaction buffer and reports whether the key
// existed in the snapshot or buffer.
func (tx *Tx) Delete(key []byte) bool {
	if tx.done || !tx.writable {
		return false
	}
	exists := tx.exists(key)
	tx.writes[string(key)] = write{deleted: true}
	return exists
}

func (tx *Tx) exists(key []byte) bool {
	if w, ok := tx.writes[string(key)]; ok {
		return !w.deleted
	}
	if tx.root == 0 {
		return false
	}
	_, ok := tx.db.treeAt(tx.root).Get(key)
	return ok
}

// Scan visits keys in [start, end) in order, merging snapshot entries with
// buffered writes. Nil bounds are unbounded. The callback returns false to
// stop.
func (tx *Tx) Scan(start, end []byte, fn func(key, value []byte) bool) error {
	if tx.done {
		return ErrTxDone
	}

	keys := make([]string, 0, len(tx.writes))
	for key := range tx.writes {
		if inRange([]byte(key), start, end) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	stopped := false
	wi := 0

	if tx.root != 0 {
		tx.db.treeAt(tx.root).Scan(start, end, func(key, value []byte) bool {
			for wi < len(keys) && keys[wi] < string(key) {
				w := tx.writes[keys[wi]]
				if !w.deleted {
					if !fn([]byte(keys[wi]), w.value) {
						stopped = true
						return false
					}
				}
				wi++
			}
			if wi < len(keys) && keys[wi] == string(key) {
				w := tx.writes[keys[wi]]
				wi++
				if w.deleted {
					return true
				}
				if !fn(key, w.value) {
					stopped = true
					return false
				}
				return true
			}
			if !fn(key, value) {
				stopped = true
				return false
			}
			return true
		})
	}

	if stopped {
		return nil
	}
	for ; wi < len(keys); wi++ {
		w := tx.writes[keys[wi]]
		if w.deleted {
			continue
		}
		if !fn([]byte(keys[wi]), w.value) {
			break
		}
	}
	return nil
}

func inRange(key, start, end []byte) bool {
	if start != nil && string(key) < string(start) {
		return false
	}
	if end != nil && string(key) >= string(end) {
		return false
	}
	return true
}

// Begin starts an explicit transaction. Writable transactions take the write
// lock; read-only transactions take the read lock. Call Commit or Rollback to
// release the lock.
func (db *DB) Begin(writable bool) (*Tx, error) {
	if writable {
		db.mu.Lock()
	} else {
		db.mu.RLock()
	}
	if db.closed {
		if writable {
			db.mu.Unlock()
		} else {
			db.mu.RUnlock()
		}
		return nil, ErrClosed
	}
	tx := &Tx{
		db:       db,
		root:     db.pager.Root(),
		writable: writable,
		writes:   make(map[string]write),
	}
	if writable {
		tx.unlock = db.mu.Unlock
	} else {
		tx.unlock = db.mu.RUnlock
	}
	return tx, nil
}

// Commit applies the transaction's writes atomically and releases its lock.
func (tx *Tx) Commit() error {
	if tx.done {
		return ErrTxDone
	}
	tx.done = true
	defer tx.release()
	if !tx.writable || len(tx.writes) == 0 {
		return nil
	}
	return tx.commit()
}

// Rollback discards the transaction's writes and releases its lock.
func (tx *Tx) Rollback() {
	if tx.done {
		return
	}
	tx.done = true
	tx.writes = nil
	tx.release()
}

func (tx *Tx) release() {
	if tx.unlock != nil {
		tx.unlock()
		tx.unlock = nil
	}
}

// View runs fn in a read-only transaction. The database is read-locked for the
// duration of fn.
func (db *DB) View(fn func(tx *Tx) error) error {
	tx, err := db.Begin(false)
	if err != nil {
		return err
	}
	err = fn(tx)
	tx.Rollback()
	return err
}

// Update runs fn in a writable transaction. If fn returns nil, all buffered
// writes are committed atomically; otherwise they are discarded. The database
// is write-locked for the duration of fn.
func (db *DB) Update(fn func(tx *Tx) error) error {
	tx, err := db.Begin(true)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (tx *Tx) commit() error {
	if len(tx.writes) == 0 {
		return nil
	}

	keys := make([]string, 0, len(tx.writes))
	for key := range tx.writes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	if err := tx.db.pager.Begin(); err != nil {
		return err
	}
	tree := tx.db.treeAt(tx.root)
	for _, key := range keys {
		w := tx.writes[key]
		if w.deleted {
			tree.Delete([]byte(key))
			continue
		}
		if err := tree.Insert([]byte(key), w.value); err != nil {
			tx.db.pager.Abort()
			return err
		}
	}
	tx.db.pager.SetRoot(tree.Root())
	if err := tx.db.pager.Commit(); err != nil {
		tx.db.pager.Abort()
		return fmt.Errorf("db: commit: %w", err)
	}
	return nil
}
