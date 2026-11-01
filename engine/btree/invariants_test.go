package btree

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

// checkNode validates the B+tree invariants and returns the number of keys in
// the subtree. Child i must contain keys in [lo, hi).
func checkNode(t *testing.T, tree *Tree, pageNo uint64, lo, hi []byte) int {
	t.Helper()
	node := tree.get(pageNo)
	nkeys := int(nodeNKeys(node))

	if nodeType(node) == TypeLeaf {
		if nkeys == 0 {
			t.Fatalf("empty leaf page %d", pageNo)
		}
		for i := 0; i < nkeys; i++ {
			key := nodeKey(node, uint16(i))
			if i > 0 && bytes.Compare(nodeKey(node, uint16(i-1)), key) >= 0 {
				t.Fatalf("leaf keys out of order at page %d index %d", pageNo, i)
			}
			if lo != nil && bytes.Compare(key, lo) < 0 {
				t.Fatalf("key %q below lower bound %q at page %d", key, lo, pageNo)
			}
			if hi != nil && bytes.Compare(key, hi) >= 0 {
				t.Fatalf("key %q above upper bound %q at page %d", key, hi, pageNo)
			}
		}
		return nkeys
	}

	if nkeys == 0 {
		t.Fatalf("empty internal page %d", pageNo)
	}
	for i := 1; i < nkeys; i++ {
		if bytes.Compare(nodeKey(node, uint16(i-1)), nodeKey(node, uint16(i))) >= 0 {
			t.Fatalf("internal keys out of order at page %d index %d", pageNo, i)
		}
	}
	total := 0
	for i := 0; i <= nkeys; i++ {
		var childLo, childHi []byte
		if i > 0 {
			childLo = nodeKey(node, uint16(i-1))
		}
		if i < nkeys {
			childHi = nodeKey(node, uint16(i))
		}
		total += checkNode(t, tree, nodeGetPtr(node, uint16(i)), childLo, childHi)
	}
	return total
}

func TestTreeInvariants(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	tree := newMemPages().tree()
	inserted := make(map[string]string)

	for i := 0; i < 1500; i++ {
		key := fmt.Sprintf("key-%03d", rng.Intn(300))
		val := fmt.Sprintf("value-%d", i)
		if err := tree.Insert([]byte(key), []byte(val)); err != nil {
			t.Fatal(err)
		}
		inserted[key] = val

		if got := checkNode(t, tree, tree.root, nil, nil); got != len(inserted) {
			t.Fatalf("tree holds %d keys, want %d", got, len(inserted))
		}
		for k, want := range inserted {
			got, ok := tree.Get([]byte(k))
			if !ok || string(got) != want {
				t.Fatalf("Get(%q) = %q, %v; want %q", k, got, ok, want)
			}
		}
	}
}
