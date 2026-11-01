package pager

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) (*Pager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	p, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return p, path
}

func writePage(t *testing.T, p *Pager, pageNo uint64, marker string) {
	t.Helper()
	data := make([]byte, PageSize)
	copy(data, marker)
	p.Put(pageNo, data)
}

func readMarker(p *Pager, pageNo uint64) string {
	page := p.Get(pageNo)
	for i, b := range page {
		if b == 0 {
			return string(page[:i])
		}
	}
	return string(page)
}

func TestCommitAndReopen(t *testing.T) {
	p, path := openTemp(t)

	if p.Root() != 0 {
		t.Fatalf("new database root = %d, want 0", p.Root())
	}
	if p.PageCount() != 2 {
		t.Fatalf("new database page count = %d, want 2", p.PageCount())
	}

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	root := p.Alloc()
	writePage(t, p, root, "hello page")
	p.SetRoot(root)
	if err := p.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if p.TxID() != 1 {
		t.Fatalf("txid = %d, want 1", p.TxID())
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	p2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer p2.Close()
	if p2.Root() != root {
		t.Fatalf("reopened root = %d, want %d", p2.Root(), root)
	}
	if got := readMarker(p2, root); got != "hello page" {
		t.Fatalf("reopened page = %q", got)
	}
	if p2.TxID() != 1 {
		t.Fatalf("reopened txid = %d, want 1", p2.TxID())
	}
}

func TestMetaPagesAlternate(t *testing.T) {
	p, path := openTemp(t)

	var roots []uint64
	for i := 0; i < 4; i++ {
		if err := p.Begin(); err != nil {
			t.Fatal(err)
		}
		pg := p.Alloc()
		writePage(t, p, pg, "data")
		p.SetRoot(pg)
		roots = append(roots, pg)
		if err := p.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	p.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var valid []Meta
	for slot := 0; slot < 2; slot++ {
		m, ok := decodeMeta(raw[slot*PageSize : (slot+1)*PageSize])
		if ok {
			valid = append(valid, m)
		}
	}
	if len(valid) != 2 {
		t.Fatalf("valid meta pages = %d, want 2", len(valid))
	}
	if valid[0].TxID == valid[1].TxID {
		t.Fatalf("meta txids are equal: %d", valid[0].TxID)
	}
	if !(valid[0].TxID == 3 && valid[1].TxID == 4) && !(valid[0].TxID == 4 && valid[1].TxID == 3) {
		t.Fatalf("meta txids = %d, %d, want 3 and 4", valid[0].TxID, valid[1].TxID)
	}
}

func TestCorruptNewestMetaFallsBack(t *testing.T) {
	p, path := openTemp(t)

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	oldRoot := p.Alloc()
	writePage(t, p, oldRoot, "old")
	p.SetRoot(oldRoot)
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	newRoot := p.Alloc()
	writePage(t, p, newRoot, "new")
	p.SetRoot(newRoot)
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	p.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	newestSlot := 0
	var newest Meta
	for slot := 0; slot < 2; slot++ {
		m, ok := decodeMeta(raw[slot*PageSize : (slot+1)*PageSize])
		if ok && m.TxID > newest.TxID {
			newest = m
			newestSlot = slot
		}
	}
	// Simulate a torn write of the newest meta page.
	raw[newestSlot*PageSize+8] ^= 0xFF
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	p2, err := Open(path)
	if err != nil {
		t.Fatalf("Open after corrupt meta: %v", err)
	}
	defer p2.Close()
	if p2.Root() != oldRoot {
		t.Fatalf("recovered root = %d, want previous root %d", p2.Root(), oldRoot)
	}
	if got := readMarker(p2, oldRoot); got != "old" {
		t.Fatalf("recovered page = %q", got)
	}
}

func TestFreeListRoundTrip(t *testing.T) {
	p, path := openTemp(t)

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	a := p.Alloc()
	b := p.Alloc()
	writePage(t, p, a, "a")
	writePage(t, p, b, "b")
	p.SetRoot(a)
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	p.Free(b)
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	if p.FreeCount() != 1 {
		t.Fatalf("free count = %d, want 1", p.FreeCount())
	}
	p.Close()

	p2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Close()
	if p2.FreeCount() != 1 {
		t.Fatalf("reopened free count = %d, want 1", p2.FreeCount())
	}
	if err := p2.Begin(); err != nil {
		t.Fatal(err)
	}
	reused := p2.Alloc()
	if reused != b {
		t.Fatalf("Alloc = %d, want freed page %d", reused, b)
	}
	if err := p2.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestFreedPageNotReusedBeforeCommit(t *testing.T) {
	p, _ := openTemp(t)
	defer p.Close()

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	a := p.Alloc()
	writePage(t, p, a, "a")
	p.SetRoot(a)
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	p.Free(a)
	fresh := p.Alloc()
	if fresh == a {
		t.Fatalf("page %d was reused in the same transaction that freed it", a)
	}
	writePage(t, p, fresh, "fresh")
	p.SetRoot(fresh)
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	if got := p.Alloc(); got != a {
		t.Fatalf("Alloc after commit = %d, want %d", got, a)
	}
	p.Abort()
}

func TestAbortRestoresState(t *testing.T) {
	p, path := openTemp(t)

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	root := p.Alloc()
	writePage(t, p, root, "committed")
	p.SetRoot(root)
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	pages := p.PageCount()

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	extra := p.Alloc()
	writePage(t, p, extra, "discarded")
	p.SetRoot(extra)
	p.Free(root)
	p.Abort()

	if p.Root() != root {
		t.Fatalf("root after abort = %d, want %d", p.Root(), root)
	}
	if p.PageCount() != pages {
		t.Fatalf("page count after abort = %d, want %d", p.PageCount(), pages)
	}
	if p.FreeCount() != 0 {
		t.Fatalf("free count after abort = %d, want 0", p.FreeCount())
	}
	p.Close()

	p2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Close()
	if got := readMarker(p2, root); got != "committed" {
		t.Fatalf("committed page = %q", got)
	}
}

func TestLargeFreeListSpansPages(t *testing.T) {
	p, path := openTemp(t)

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	root := p.Alloc()
	p.SetRoot(root)
	var pages []uint64
	for i := 0; i < 1300; i++ {
		pages = append(pages, p.Alloc())
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	for _, pg := range pages {
		p.Free(pg)
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	if p.FreeCount() != len(pages) {
		t.Fatalf("free count = %d, want %d", p.FreeCount(), len(pages))
	}
	p.Close()

	p2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen with large free list: %v", err)
	}
	defer p2.Close()
	if p2.FreeCount() != len(pages) {
		t.Fatalf("reopened free count = %d, want %d", p2.FreeCount(), len(pages))
	}

	seen := make(map[uint64]bool)
	if err := p2.Begin(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(pages); i++ {
		pg := p2.Alloc()
		if seen[pg] {
			t.Fatalf("page %d allocated twice", pg)
		}
		seen[pg] = true
	}
	for _, pg := range pages {
		if !seen[pg] {
			t.Fatalf("freed page %d was never reused", pg)
		}
	}
	p2.Abort()
}

func TestCorruptFreeListRejected(t *testing.T) {
	p, path := openTemp(t)

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	a := p.Alloc()
	p.SetRoot(a)
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	p.Free(a)
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	meta := p.meta
	p.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Point the free list at page 1 (a meta page), which is invalid.
	off := int(meta.FreeHead * PageSize)
	binary.LittleEndian.PutUint64(raw[off:off+8], 1)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Open error = %v, want ErrCorrupt", err)
	}
}
