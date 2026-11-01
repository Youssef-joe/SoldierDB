package btree

import (
	"bytes"
	"testing"
)

func TestLeafNodeRoundTrip(t *testing.T) {
	keys := [][]byte{[]byte("alpha"), []byte("beta"), []byte("gamma")}
	values := [][]byte{[]byte("1"), []byte("22"), []byte("333")}
	node := buildLeaf(keys, values)

	if nodeType(node) != TypeLeaf {
		t.Fatalf("type = %d, want leaf", nodeType(node))
	}
	if got := nodeNKeys(node); got != 3 {
		t.Fatalf("nkeys = %d, want 3", got)
	}
	if nodeUsed(node) != len(node) {
		t.Fatalf("nodeUsed = %d, len = %d", nodeUsed(node), len(node))
	}
	for i := range keys {
		if got := nodeKey(node, uint16(i)); !bytes.Equal(got, keys[i]) {
			t.Fatalf("key %d = %q, want %q", i, got, keys[i])
		}
		if got := nodeValue(node, uint16(i)); !bytes.Equal(got, values[i]) {
			t.Fatalf("value %d = %q, want %q", i, got, values[i])
		}
	}
}

func TestInternalNodeRoundTrip(t *testing.T) {
	keys := [][]byte{[]byte("b"), []byte("d")}
	ptrs := []uint64{10, 11, 12}
	node := buildInternal(keys, ptrs)

	if nodeType(node) != TypeInternal {
		t.Fatalf("type = %d, want internal", nodeType(node))
	}
	if got := nodeNKeys(node); got != 2 {
		t.Fatalf("nkeys = %d, want 2", got)
	}
	for i := range keys {
		if got := nodeKey(node, uint16(i)); !bytes.Equal(got, keys[i]) {
			t.Fatalf("key %d = %q, want %q", i, got, keys[i])
		}
	}
	for i := range ptrs {
		if got := nodeGetPtr(node, uint16(i)); got != ptrs[i] {
			t.Fatalf("ptr %d = %d, want %d", i, got, ptrs[i])
		}
	}
}

func TestUpperAndLowerBound(t *testing.T) {
	node := buildLeaf(
		[][]byte{[]byte("b"), []byte("d"), []byte("f")},
		[][]byte{[]byte("1"), []byte("2"), []byte("3")},
	)
	cases := []struct {
		key   string
		upper uint16
		lower uint16
	}{
		{"a", 0, 0},
		{"b", 1, 0},
		{"c", 1, 1},
		{"d", 2, 1},
		{"g", 3, 3},
	}
	for _, tc := range cases {
		if got := upperBound(node, []byte(tc.key)); got != tc.upper {
			t.Errorf("upperBound(%q) = %d, want %d", tc.key, got, tc.upper)
		}
		if got := lowerBound(node, []byte(tc.key)); got != tc.lower {
			t.Errorf("lowerBound(%q) = %d, want %d", tc.key, got, tc.lower)
		}
	}
}
