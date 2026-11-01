// Package pager implements the on-disk page layer for YousDB.
//
// The file is an array of fixed-size pages. Pages 0 and 1 are alternating
// meta pages; the rest hold B+tree nodes and free-list pages. A commit writes
// the inactive meta page last and fsyncs it, so after a crash exactly one meta
// page describes a complete tree.
package pager

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
)

const (
	// PageSize is the fixed size of every page in the file.
	PageSize = 4096

	// MetaMagic identifies a YousDB file.
	MetaMagic = 0x594F5553 // "YOUS"
	// MetaVersion is the current on-disk format version.
	MetaVersion = 1

	metaChecksumOffset = 40
	metaSize           = 44

	freeNextOffset  = 0
	freeCountOffset = 8
	freeHeaderSize  = 12
	freeCap         = (PageSize - freeHeaderSize) / 8
)

var (
	// ErrCorrupt is returned when no valid meta page can be found.
	ErrCorrupt = errors.New("pager: corrupt or incompatible database file")
	// ErrClosed is returned after the pager has been closed.
	ErrClosed = errors.New("pager: closed")
	// ErrTxActive is returned when a transaction is already in progress.
	ErrTxActive = errors.New("pager: transaction already active")
	// ErrNoTx is returned when a transaction operation is attempted outside one.
	ErrNoTx = errors.New("pager: no active transaction")
)

// Meta is the committed state of the database.
type Meta struct {
	TxID      uint64
	Root      uint64
	FreeHead  uint64
	PageCount uint64
}

// Pager owns the database file and its page cache.
//
// A Pager is not safe for concurrent use by multiple goroutines; the db
// package serializes access.
type Pager struct {
	mu   sync.Mutex
	file *os.File
	path string

	cache map[uint64][]byte
	dirty map[uint64]struct{}

	meta     Meta
	metaSlot int
	closed   bool
	noSync   bool

	// freePrev holds pages that are free in the committed meta and may be
	// reused right now. freeNew holds pages that became free during the
	// active transaction (or are free-list pages of the committed meta) and
	// may only be reused after the next successful commit.
	freePrev []uint64
	freeNew  []uint64

	tx         bool
	txSnapshot txState
}

type txState struct {
	root      uint64
	pageCount uint64
	freePrev  []uint64
	freeNew   []uint64
}

// Options configures a Pager.
type Options struct {
	// NoSync skips fsync after writes. It is intended for tests and
	// benchmarks; a real database must sync.
	NoSync bool
}

// Open opens or creates a database file at path.
func Open(path string) (*Pager, error) {
	return OpenWithOptions(path, Options{})
}

// OpenWithOptions opens or creates a database file with explicit options.
func OpenWithOptions(path string, opts Options) (*Pager, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}

	p := &Pager{
		file:   f,
		path:   path,
		cache:  make(map[uint64][]byte),
		dirty:  make(map[uint64]struct{}),
		noSync: opts.NoSync,
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	if info.Size() == 0 {
		if err := p.initFile(); err != nil {
			f.Close()
			return nil, err
		}
		return p, nil
	}

	if info.Size() < 2*PageSize || info.Size()%PageSize != 0 {
		f.Close()
		return nil, fmt.Errorf("%w: size %d", ErrCorrupt, info.Size())
	}

	if err := p.loadMeta(); err != nil {
		f.Close()
		return nil, err
	}
	if err := p.loadFreeList(); err != nil {
		f.Close()
		return nil, err
	}
	return p, nil
}

func (p *Pager) initFile() error {
	p.meta = Meta{TxID: 0, Root: 0, FreeHead: 0, PageCount: 2}
	p.metaSlot = 0

	if err := p.writeMetaPage(0, p.meta); err != nil {
		return err
	}
	// Make the file exactly two pages long.
	if err := p.file.Truncate(2 * PageSize); err != nil {
		return err
	}
	return p.sync()
}

func (p *Pager) loadMeta() error {
	var best Meta
	bestSlot := -1

	for slot := 0; slot < 2; slot++ {
		page := make([]byte, PageSize)
		if _, err := p.file.ReadAt(page, int64(slot*PageSize)); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				continue
			}
			return err
		}
		meta, ok := decodeMeta(page)
		if !ok {
			continue
		}
		if bestSlot == -1 || meta.TxID > best.TxID {
			best = meta
			bestSlot = slot
		}
	}

	if bestSlot == -1 {
		return ErrCorrupt
	}
	p.meta = best
	p.metaSlot = bestSlot
	return nil
}

func decodeMeta(page []byte) (Meta, bool) {
	if len(page) < metaSize {
		return Meta{}, false
	}
	if binary.LittleEndian.Uint32(page[0:4]) != MetaMagic {
		return Meta{}, false
	}
	if binary.LittleEndian.Uint32(page[4:8]) != MetaVersion {
		return Meta{}, false
	}
	want := binary.LittleEndian.Uint32(page[metaChecksumOffset : metaChecksumOffset+4])
	got := crc32.ChecksumIEEE(page[:metaChecksumOffset])
	if want != got {
		return Meta{}, false
	}
	return Meta{
		TxID:      binary.LittleEndian.Uint64(page[8:16]),
		Root:      binary.LittleEndian.Uint64(page[16:24]),
		FreeHead:  binary.LittleEndian.Uint64(page[24:32]),
		PageCount: binary.LittleEndian.Uint64(page[32:40]),
	}, true
}

func encodeMeta(dst []byte, m Meta) {
	clear(dst)
	binary.LittleEndian.PutUint32(dst[0:4], MetaMagic)
	binary.LittleEndian.PutUint32(dst[4:8], MetaVersion)
	binary.LittleEndian.PutUint64(dst[8:16], m.TxID)
	binary.LittleEndian.PutUint64(dst[16:24], m.Root)
	binary.LittleEndian.PutUint64(dst[24:32], m.FreeHead)
	binary.LittleEndian.PutUint64(dst[32:40], m.PageCount)
	binary.LittleEndian.PutUint32(dst[metaChecksumOffset:metaChecksumOffset+4],
		crc32.ChecksumIEEE(dst[:metaChecksumOffset]))
}

func (p *Pager) writeMetaPage(slot int, m Meta) error {
	page := make([]byte, PageSize)
	encodeMeta(page, m)
	_, err := p.file.WriteAt(page, int64(slot*PageSize))
	return err
}

func (p *Pager) sync() error {
	if p.noSync {
		return nil
	}
	return p.file.Sync()
}

func (p *Pager) loadFreeList() error {
	if p.meta.FreeHead == 0 {
		p.freePrev = nil
		p.freeNew = nil
		return nil
	}

	seen := make(map[uint64]struct{})
	listed := make(map[uint64]struct{})
	var freePages []uint64
	var chain []uint64
	next := p.meta.FreeHead

	for next != 0 {
		if next < 2 || next >= p.meta.PageCount {
			return fmt.Errorf("%w: free-list page %d out of range", ErrCorrupt, next)
		}
		if _, dup := seen[next]; dup {
			return fmt.Errorf("%w: free-list cycle at page %d", ErrCorrupt, next)
		}
		seen[next] = struct{}{}
		chain = append(chain, next)

		page, err := p.readPage(next)
		if err != nil {
			return err
		}
		count := binary.LittleEndian.Uint32(page[freeCountOffset : freeCountOffset+4])
		if count > freeCap {
			return fmt.Errorf("%w: free-list page %d count %d", ErrCorrupt, next, count)
		}
		pos := freeHeaderSize
		for i := uint32(0); i < count; i++ {
			pg := binary.LittleEndian.Uint64(page[pos : pos+8])
			pos += 8
			if pg < 2 || pg >= p.meta.PageCount {
				return fmt.Errorf("%w: free page %d out of range", ErrCorrupt, pg)
			}
			if _, dup := listed[pg]; dup {
				return fmt.Errorf("%w: duplicate free page %d", ErrCorrupt, pg)
			}
			listed[pg] = struct{}{}
			freePages = append(freePages, pg)
		}
		next = binary.LittleEndian.Uint64(page[freeNextOffset : freeNextOffset+8])
	}

	p.freePrev = freePages
	// The chain pages are still referenced by the committed meta. They can be
	// reused only after the next commit replaces that meta page.
	p.freeNew = chain
	return nil
}

func (p *Pager) readPage(pageNo uint64) ([]byte, error) {
	page := make([]byte, PageSize)
	_, err := p.file.ReadAt(page, int64(pageNo*PageSize))
	if err != nil {
		return nil, err
	}
	return page, nil
}

// Begin starts a write transaction. Only one transaction may be active.
func (p *Pager) Begin() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	if p.tx {
		return ErrTxActive
	}
	p.tx = true
	p.txSnapshot = txState{
		root:      p.meta.Root,
		pageCount: p.meta.PageCount,
		freePrev:  append([]uint64(nil), p.freePrev...),
		freeNew:   append([]uint64(nil), p.freeNew...),
	}
	return nil
}

// Abort discards all changes made since Begin.
func (p *Pager) Abort() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.tx {
		return
	}
	for pageNo := range p.dirty {
		delete(p.cache, pageNo)
	}
	clear(p.dirty)
	p.meta.Root = p.txSnapshot.root
	p.meta.PageCount = p.txSnapshot.pageCount
	p.freePrev = p.txSnapshot.freePrev
	p.freeNew = p.txSnapshot.freeNew
	p.tx = false
}

// Commit writes all dirty pages, persists the free list, then publishes a new
// meta page and fsyncs it. The meta write is the atomic commit point.
func (p *Pager) Commit() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	if !p.tx {
		return ErrNoTx
	}

	freeHead, freePrev, freeNew, err := p.writeFreeList()
	if err != nil {
		return err
	}

	if err := p.flushDirty(); err != nil {
		return err
	}
	if err := p.sync(); err != nil {
		return err
	}

	next := Meta{
		TxID:      p.meta.TxID + 1,
		Root:      p.meta.Root,
		FreeHead:  freeHead,
		PageCount: p.meta.PageCount,
	}
	slot := 1 - p.metaSlot
	if err := p.writeMetaPage(slot, next); err != nil {
		return err
	}
	if err := p.sync(); err != nil {
		return err
	}

	p.meta = next
	p.metaSlot = slot
	p.freePrev = freePrev
	p.freeNew = freeNew
	clear(p.dirty)
	p.tx = false
	return nil
}

func (p *Pager) flushDirty() error {
	for pageNo := range p.dirty {
		page := p.cache[pageNo]
		if _, err := p.file.WriteAt(page, int64(pageNo*PageSize)); err != nil {
			return err
		}
	}
	return nil
}

// writeFreeList serializes the free list into chain pages and returns the new
// head plus the free sets as they should look after the commit succeeds.
func (p *Pager) writeFreeList() (head uint64, freePrev, freeNew []uint64, err error) {
	pool := append([]uint64(nil), p.freePrev...)
	var chain []uint64

	for {
		remaining := len(pool) + len(p.freeNew)
		need := (remaining + freeCap - 1) / freeCap
		if need <= len(chain) {
			// We took more chain pages than needed; give them back.
			pool = append(pool, chain[need:]...)
			chain = chain[:need]
			break
		}
		if len(pool) > 0 {
			chain = append(chain, pool[len(pool)-1])
			pool = pool[:len(pool)-1]
			continue
		}
		pg := p.meta.PageCount
		p.meta.PageCount++
		p.cache[pg] = make([]byte, PageSize)
		p.dirty[pg] = struct{}{}
		chain = append(chain, pg)
	}

	remaining := make([]uint64, 0, len(pool)+len(p.freeNew))
	remaining = append(remaining, pool...)
	remaining = append(remaining, p.freeNew...)

	if len(remaining) == 0 {
		return 0, nil, chain, nil
	}

	pos := 0
	for i, pg := range chain {
		page, gerr := p.cachePage(pg)
		if gerr != nil {
			return 0, nil, nil, gerr
		}
		clear(page)
		var next uint64
		if i+1 < len(chain) {
			next = chain[i+1]
		}
		binary.LittleEndian.PutUint64(page[freeNextOffset:freeNextOffset+8], next)
		n := len(remaining) - pos
		if n > freeCap {
			n = freeCap
		}
		binary.LittleEndian.PutUint32(page[freeCountOffset:freeCountOffset+4], uint32(n))
		off := freeHeaderSize
		for j := 0; j < n; j++ {
			binary.LittleEndian.PutUint64(page[off:off+8], remaining[pos+j])
			off += 8
		}
		p.markDirty(pg)
		pos += n
	}

	if len(remaining) > 0 {
		return chain[0], remaining, chain, nil
	}
	return 0, nil, chain, nil
}

// Get returns the page contents. The returned slice must not be modified by
// callers; the pager owns the memory.
func (p *Pager) Get(pageNo uint64) []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		panic(ErrClosed)
	}
	if pageNo < 2 || pageNo >= p.meta.PageCount {
		panic(fmt.Sprintf("pager: page %d out of range (page count %d)", pageNo, p.meta.PageCount))
	}
	if page, ok := p.cache[pageNo]; ok {
		return page
	}
	page, err := p.readPage(pageNo)
	if err != nil {
		panic(fmt.Errorf("pager: read page %d: %w", pageNo, err))
	}
	p.cache[pageNo] = page
	return page
}

// Put replaces the contents of an allocated page. The write becomes durable at
// Commit.
func (p *Pager) Put(pageNo uint64, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		panic(ErrClosed)
	}
	if !p.tx {
		panic(ErrNoTx)
	}
	if len(data) > PageSize {
		panic(fmt.Sprintf("pager: page data too large: %d", len(data)))
	}
	page := make([]byte, PageSize)
	copy(page, data)
	p.cache[pageNo] = page
	p.dirty[pageNo] = struct{}{}
}

// Alloc returns a zeroed page ready to be written. The page is only durable
// after Commit.
func (p *Pager) Alloc() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.tx {
		panic(ErrNoTx)
	}
	pg, err := p.allocPage()
	if err != nil {
		panic(err)
	}
	return pg
}

func (p *Pager) allocPage() (uint64, error) {
	if len(p.freePrev) > 0 {
		pg := p.freePrev[len(p.freePrev)-1]
		p.freePrev = p.freePrev[:len(p.freePrev)-1]
		page := make([]byte, PageSize)
		p.cache[pg] = page
		p.dirty[pg] = struct{}{}
		return pg, nil
	}
	pg := p.meta.PageCount
	p.meta.PageCount++
	page := make([]byte, PageSize)
	p.cache[pg] = page
	p.dirty[pg] = struct{}{}
	return pg, nil
}

func (p *Pager) cachePage(pageNo uint64) ([]byte, error) {
	if page, ok := p.cache[pageNo]; ok {
		return page, nil
	}
	page, err := p.readPage(pageNo)
	if err != nil {
		return nil, err
	}
	p.cache[pageNo] = page
	return page, nil
}

func (p *Pager) markDirty(pageNo uint64) {
	p.dirty[pageNo] = struct{}{}
}

// Free marks pageNo as free. It becomes reusable only after the next commit.
func (p *Pager) Free(pageNo uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.tx {
		panic(ErrNoTx)
	}
	if pageNo < 2 || pageNo >= p.meta.PageCount {
		panic(fmt.Sprintf("pager: free page %d out of range", pageNo))
	}
	delete(p.cache, pageNo)
	delete(p.dirty, pageNo)
	p.freeNew = append(p.freeNew, pageNo)
}

// Root returns the current tree root page, or 0 for an empty tree.
func (p *Pager) Root() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.meta.Root
}

// SetRoot updates the tree root. The new root becomes durable at Commit.
func (p *Pager) SetRoot(root uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.tx {
		panic(ErrNoTx)
	}
	p.meta.Root = root
}

// TxID returns the transaction id of the committed state.
func (p *Pager) TxID() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.meta.TxID
}

// PageCount returns the number of pages in the file.
func (p *Pager) PageCount() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.meta.PageCount
}

// FreeCount returns the number of pages available for reuse after the next
// commit.
func (p *Pager) FreeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.freePrev)
}

// Path returns the file path.
func (p *Pager) Path() string { return p.path }

// Close closes the underlying file. Uncommitted changes are discarded.
func (p *Pager) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	p.cache = nil
	p.dirty = nil
	return p.file.Close()
}
