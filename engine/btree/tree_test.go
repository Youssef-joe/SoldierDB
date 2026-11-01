package btree

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Youssef-joe/YousDB/engine/kv"
	"github.com/Youssef-joe/YousDB/engine/pager"
)

type memPages struct {
	pages map[uint64][]byte
	next  uint64
}

func newMemPages() *memPages {
	return &memPages{pages: make(map[uint64][]byte), next: 1}
}

func (m *memPages) get(pageNo uint64) []byte {
	page, ok := m.pages[pageNo]
	if !ok {
		panic(fmt.Sprintf("missing page %d", pageNo))
	}
	return page
}

func (m *memPages) alloc(data []byte) uint64 {
	pageNo := m.next
	m.next++
	m.pages[pageNo] = append([]byte(nil), data...)
	return pageNo
}

func (m *memPages) del(pageNo uint64) {
	delete(m.pages, pageNo)
}

func (m *memPages) tree() *Tree {
	return New(0, m.get, m.alloc, m.del)
}

func TestEmptyTree(t *testing.T) {
	tree := newMemPages().tree()
	if _, ok := tree.Get([]byte("missing")); ok {
		t.Fatal("Get on empty tree found a value")
	}
	if tree.Depth() != 0 {
		t.Fatalf("Depth = %d, want 0", tree.Depth())
	}
	count := 0
	tree.Scan(nil, nil, func(_, _ []byte) bool { count++; return true })
	if count != 0 {
		t.Fatalf("Scan visited %d entries, want 0", count)
	}
}

func TestInsertGetReplace(t *testing.T) {
	tree := newMemPages().tree()

	if err := tree.Insert([]byte("b"), []byte("2")); err != nil {
		t.Fatal(err)
	}
	if err := tree.Insert([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := tree.Insert([]byte("c"), []byte("3")); err != nil {
		t.Fatal(err)
	}

	for key, want := range map[string]string{"a": "1", "b": "2", "c": "3"} {
		got, ok := tree.Get([]byte(key))
		if !ok || string(got) != want {
			t.Fatalf("Get(%q) = %q, %v; want %q", key, got, ok, want)
		}
	}

	if err := tree.Insert([]byte("b"), []byte("twenty-two")); err != nil {
		t.Fatal(err)
	}
	got, ok := tree.Get([]byte("b"))
	if !ok || string(got) != "twenty-two" {
		t.Fatalf("Get after replace = %q, %v", got, ok)
	}

	if _, ok := tree.Get([]byte("d")); ok {
		t.Fatal("Get of missing key succeeded")
	}
}

func TestManyKeys(t *testing.T) {
	const n = 5000
	orders := map[string][]int{
		"sequential": nil,
		"reverse":    nil,
		"random":     nil,
	}
	orders["sequential"] = make([]int, n)
	orders["reverse"] = make([]int, n)
	orders["random"] = make([]int, n)
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < n; i++ {
		orders["sequential"][i] = i
		orders["reverse"][i] = n - 1 - i
		orders["random"][i] = i
	}
	rng.Shuffle(n, func(i, j int) { orders["random"][i], orders["random"][j] = orders["random"][j], orders["random"][i] })

	for name, order := range orders {
		t.Run(name, func(t *testing.T) {
			tree := newMemPages().tree()
			for _, i := range order {
				key := []byte(fmt.Sprintf("key-%06d", i))
				val := []byte(fmt.Sprintf("value-%06d", i))
				if err := tree.Insert(key, val); err != nil {
					t.Fatal(err)
				}
			}
			if tree.Depth() < 2 {
				t.Fatalf("Depth = %d, want at least 2", tree.Depth())
			}
			for i := 0; i < n; i++ {
				key := []byte(fmt.Sprintf("key-%06d", i))
				got, ok := tree.Get(key)
				if !ok {
					t.Fatalf("missing key %q", key)
				}
				want := fmt.Sprintf("value-%06d", i)
				if string(got) != want {
					t.Fatalf("Get(%q) = %q, want %q", key, got, want)
				}
			}

			var got []string
			tree.Scan(nil, nil, func(key, _ []byte) bool {
				got = append(got, string(key))
				return true
			})
			if len(got) != n {
				t.Fatalf("Scan visited %d keys, want %d", len(got), n)
			}
			if !sort.StringsAreSorted(got) {
				t.Fatal("Scan keys are not sorted")
			}
		})
	}
}

func TestScanRanges(t *testing.T) {
	tree := newMemPages().tree()
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		if err := tree.Insert([]byte(k), []byte(k+"-value")); err != nil {
			t.Fatal(err)
		}
	}

	collect := func(start, end string) []string {
		var out []string
		var s, e []byte
		if start != "\x00" {
			s = []byte(start)
		}
		if end != "\x00" {
			e = []byte(end)
		}
		tree.Scan(s, e, func(key, _ []byte) bool {
			out = append(out, string(key))
			return true
		})
		return out
	}

	cases := []struct {
		start, end string
		want       []string
	}{
		{"\x00", "\x00", []string{"a", "b", "c", "d", "e"}},
		{"b", "d", []string{"b", "c"}},
		{"b", "\x00", []string{"b", "c", "d", "e"}},
		{"\x00", "c", []string{"a", "b"}},
		{"c", "c", nil},
		{"z", "\x00", nil},
		{"\x00", "a", nil},
	}
	for _, tc := range cases {
		got := collect(tc.start, tc.end)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("Scan(%q, %q) = %v, want %v", tc.start, tc.end, got, tc.want)
		}
	}

	// Early termination.
	count := 0
	tree.Scan(nil, nil, func(_, _ []byte) bool {
		count++
		return count < 2
	})
	if count != 2 {
		t.Fatalf("early stop visited %d, want 2", count)
	}
}

func TestSizeLimits(t *testing.T) {
	tree := newMemPages().tree()
	if err := tree.Insert(make([]byte, MaxKeySize+1), []byte("v")); !errors.Is(err, ErrKeyTooLarge) {
		t.Fatalf("large key error = %v", err)
	}
	if err := tree.Insert([]byte("k"), make([]byte, MaxValueSize+1)); !errors.Is(err, ErrValueTooLarge) {
		t.Fatalf("large value error = %v", err)
	}
	if err := tree.Insert(make([]byte, MaxKeySize), make([]byte, MaxValueSize)); err != nil {
		t.Fatalf("max-size insert: %v", err)
	}
}

func TestPagerPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tree.db")
	p, err := pager.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	makeTree := func(root uint64) *Tree {
		return New(root,
			func(pageNo uint64) []byte { return p.Get(pageNo) },
			func(data []byte) uint64 {
				pageNo := p.Alloc()
				p.Put(pageNo, data)
				return pageNo
			},
			func(pageNo uint64) { p.Free(pageNo) },
		)
	}

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	tree := makeTree(0)
	for i := 0; i < 2000; i++ {
		key := []byte(fmt.Sprintf("k%05d", i))
		val := []byte(fmt.Sprintf("v%05d", i))
		if err := tree.Insert(key, val); err != nil {
			t.Fatal(err)
		}
	}
	p.SetRoot(tree.Root())
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	root := tree.Root()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	p2, err := pager.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Close()
	tree2 := New(p2.Root(), func(pageNo uint64) []byte { return p2.Get(pageNo) }, nil, nil)
	if tree2.Root() != root {
		t.Fatalf("root = %d, want %d", tree2.Root(), root)
	}
	for i := 0; i < 2000; i++ {
		key := []byte(fmt.Sprintf("k%05d", i))
		got, ok := tree2.Get(key)
		if !ok || string(got) != fmt.Sprintf("v%05d", i) {
			t.Fatalf("Get(%q) = %q, %v", key, got, ok)
		}
	}
}

func TestOracleAgainstMemoryStore(t *testing.T) {
	tree := newMemPages().tree()
	oracle := kv.NewMemoryStore()
	rng := rand.New(rand.NewSource(7))

	for i := 0; i < 3000; i++ {
		key := fmt.Sprintf("key-%03d", rng.Intn(500))
		val := fmt.Sprintf("val-%d-%d", i, rng.Intn(1000))
		if err := tree.Insert([]byte(key), []byte(val)); err != nil {
			t.Fatal(err)
		}
		oracle.Set(key, []byte(val))
	}

	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("key-%03d", rng.Intn(500))
		gotTree, okTree := tree.Get([]byte(key))
		gotOracle, err := oracle.Get(key)
		if errors.Is(err, kv.ErrNotFound) {
			if okTree {
				t.Fatalf("tree found %q but oracle did not", key)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !okTree || !bytes.Equal(gotTree, gotOracle) {
			t.Fatalf("Get(%q) = %q, %v; oracle %q", key, gotTree, okTree, gotOracle)
		}
	}

	var treePairs []string
	tree.Scan(nil, nil, func(key, value []byte) bool {
		treePairs = append(treePairs, string(key)+"="+string(value))
		return true
	})
	var oraclePairs []string
	for _, pair := range oracle.Scan("", "") {
		oraclePairs = append(oraclePairs, pair.Key+"="+string(pair.Value))
	}
	if fmt.Sprint(treePairs) != fmt.Sprint(oraclePairs) {
		t.Fatalf("tree scan differs from oracle:\ntree:   %v\noracle: %v", treePairs, oraclePairs)
	}
}
