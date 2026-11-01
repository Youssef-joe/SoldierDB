package btree

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/Youssef-joe/YousDB/engine/kv"
	"github.com/Youssef-joe/YousDB/engine/pager"
)

func TestDeleteLeaf(t *testing.T) {
	tree := newMemPages().tree()
	for _, k := range []string{"a", "b", "c"} {
		if err := tree.Insert([]byte(k), []byte(k)); err != nil {
			t.Fatal(err)
		}
	}

	if !tree.Delete([]byte("b")) {
		t.Fatal("Delete(b) = false")
	}
	if _, ok := tree.Get([]byte("b")); ok {
		t.Fatal("b still present")
	}
	for _, k := range []string{"a", "c"} {
		if got, ok := tree.Get([]byte(k)); !ok || string(got) != k {
			t.Fatalf("Get(%q) = %q, %v", k, got, ok)
		}
	}
	if tree.Delete([]byte("missing")) {
		t.Fatal("Delete(missing) = true")
	}
}

func TestDeleteAll(t *testing.T) {
	const n = 3000
	tree := newMemPages().tree()
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%05d", i))
		if err := tree.Insert(key, []byte("value")); err != nil {
			t.Fatal(err)
		}
	}

	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	rng := rand.New(rand.NewSource(3))
	rng.Shuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })

	for pos, i := range order {
		key := []byte(fmt.Sprintf("key-%05d", i))
		if !tree.Delete(key) {
			t.Fatalf("Delete(%q) = false at position %d", key, pos)
		}
		if tree.root != 0 {
			checkNode(t, tree, tree.root, nil, nil)
		}
	}

	if tree.root != 0 {
		t.Fatalf("root = %d after deleting everything", tree.root)
	}
	count := 0
	tree.Scan(nil, nil, func(_, _ []byte) bool { count++; return true })
	if count != 0 {
		t.Fatalf("Scan visited %d entries, want 0", count)
	}
}

func TestDeleteAndReinsert(t *testing.T) {
	tree := newMemPages().tree()
	for i := 0; i < 1000; i++ {
		if err := tree.Insert([]byte(fmt.Sprintf("k%04d", i)), []byte("v")); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 1000; i += 2 {
		if !tree.Delete([]byte(fmt.Sprintf("k%04d", i))) {
			t.Fatalf("delete k%04d", i)
		}
	}
	for i := 0; i < 1000; i += 2 {
		if err := tree.Insert([]byte(fmt.Sprintf("k%04d", i)), []byte("again")); err != nil {
			t.Fatal(err)
		}
	}
	checkNode(t, tree, tree.root, nil, nil)
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("k%04d", i)
		got, ok := tree.Get([]byte(key))
		if !ok {
			t.Fatalf("missing %s", key)
		}
		if i%2 == 0 && string(got) != "again" {
			t.Fatalf("%s = %q, want again", key, got)
		}
	}
}

func TestDeleteOracle(t *testing.T) {
	tree := newMemPages().tree()
	oracle := kv.NewMemoryStore()
	rng := rand.New(rand.NewSource(99))

	for i := 0; i < 4000; i++ {
		key := fmt.Sprintf("key-%03d", rng.Intn(400))
		if rng.Intn(2) == 0 {
			val := fmt.Sprintf("value-%d", i)
			if err := tree.Insert([]byte(key), []byte(val)); err != nil {
				t.Fatal(err)
			}
			oracle.Set(key, []byte(val))
		} else {
			want := oracle.Delete(key)
			if got := tree.Delete([]byte(key)); got != want {
				t.Fatalf("Delete(%q) = %v, oracle %v", key, got, want)
			}
		}

		if tree.root != 0 {
			checkNode(t, tree, tree.root, nil, nil)
		}

		for j := 0; j < 20; j++ {
			k := fmt.Sprintf("key-%03d", rng.Intn(400))
			gotTree, okTree := tree.Get([]byte(k))
			gotOracle, err := oracle.Get(k)
			if errors.Is(err, kv.ErrNotFound) {
				if okTree {
					t.Fatalf("step %d: tree has %q, oracle does not", i, k)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if !okTree || !bytes.Equal(gotTree, gotOracle) {
				t.Fatalf("step %d: Get(%q) = %q, %v; oracle %q", i, k, gotTree, okTree, gotOracle)
			}
		}
	}
}

func TestDeletePagerPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "delete.db")
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
	for i := 0; i < 1500; i++ {
		if err := tree.Insert([]byte(fmt.Sprintf("k%05d", i)), []byte(fmt.Sprintf("v%05d", i))); err != nil {
			t.Fatal(err)
		}
	}
	p.SetRoot(tree.Root())
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := p.Begin(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1500; i += 3 {
		if !tree.Delete([]byte(fmt.Sprintf("k%05d", i))) {
			t.Fatalf("delete %d", i)
		}
	}
	p.SetRoot(tree.Root())
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	p2, err := pager.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Close()
	tree2 := New(p2.Root(), func(pageNo uint64) []byte { return p2.Get(pageNo) }, nil, nil)
	checkNode(t, tree2, tree2.Root(), nil, nil)
	for i := 0; i < 1500; i++ {
		key := fmt.Sprintf("k%05d", i)
		_, ok := tree2.Get([]byte(key))
		if i%3 == 0 && ok {
			t.Fatalf("%s should be deleted", key)
		}
		if i%3 != 0 && !ok {
			t.Fatalf("%s should exist", key)
		}
	}
}
