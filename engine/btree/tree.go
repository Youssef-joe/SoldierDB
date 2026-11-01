package btree

import (
	"bytes"
	"errors"
	"fmt"
)

var (
	// ErrKeyTooLarge is returned for keys larger than MaxKeySize.
	ErrKeyTooLarge = errors.New("btree: key too large")
	// ErrValueTooLarge is returned for values larger than MaxValueSize.
	ErrValueTooLarge = errors.New("btree: value too large")
)

// Tree is a copy-on-write B+tree over a page store.
//
// get returns the bytes of a page. alloc stores a node and returns its page
// number. del releases a page that is no longer reachable from the new tree.
type Tree struct {
	root  uint64
	get   func(uint64) []byte
	alloc func([]byte) uint64
	del   func(uint64)
}

// New creates a tree rooted at root. Callbacks must be non-nil except del.
func New(root uint64, get func(uint64) []byte, alloc func([]byte) uint64, del func(uint64)) *Tree {
	if del == nil {
		del = func(uint64) {}
	}
	return &Tree{root: root, get: get, alloc: alloc, del: del}
}

// Root returns the current root page, or 0 for an empty tree.
func (t *Tree) Root() uint64 { return t.root }

// SetRoot replaces the root page number.
func (t *Tree) SetRoot(root uint64) { t.root = root }

type split struct {
	left  uint64
	right uint64
	key   []byte
}

// Insert adds or replaces a key.
func (t *Tree) Insert(key, value []byte) error {
	if len(key) > MaxKeySize {
		return fmt.Errorf("%w: %d > %d", ErrKeyTooLarge, len(key), MaxKeySize)
	}
	if len(value) > MaxValueSize {
		return fmt.Errorf("%w: %d > %d", ErrValueTooLarge, len(value), MaxValueSize)
	}

	if t.root == 0 {
		t.root = t.alloc(buildLeaf([][]byte{key}, [][]byte{value}))
		return nil
	}

	newRoot, sp := t.insertNode(t.root, key, value)
	t.del(t.root)
	if sp != nil {
		newRoot = t.alloc(buildInternal([][]byte{sp.key}, []uint64{sp.left, sp.right}))
	}
	t.root = newRoot
	return nil
}

func (t *Tree) insertNode(pageNo uint64, key, value []byte) (uint64, *split) {
	node := t.get(pageNo)
	switch nodeType(node) {
	case TypeLeaf:
		return t.insertLeaf(node, key, value)
	case TypeInternal:
		return t.insertInternal(node, key, value)
	default:
		panic(fmt.Sprintf("btree: unknown node type %d", nodeType(node)))
	}
}

func (t *Tree) insertLeaf(node, key, value []byte) (uint64, *split) {
	nkeys := int(nodeNKeys(node))
	idx := int(upperBound(node, key))
	replaced := idx > 0 && bytes.Equal(nodeKey(node, uint16(idx-1)), key)

	keys := make([][]byte, 0, nkeys+1)
	values := make([][]byte, 0, nkeys+1)
	for i := 0; i < nkeys; i++ {
		if !replaced && i == idx {
			keys = append(keys, key)
			values = append(values, value)
		}
		if replaced && i == idx-1 {
			keys = append(keys, key)
			values = append(values, value)
		} else {
			keys = append(keys, nodeKey(node, uint16(i)))
			values = append(values, nodeValue(node, uint16(i)))
		}
	}
	if !replaced && idx == nkeys {
		keys = append(keys, key)
		values = append(values, value)
	}

	data := buildLeaf(keys, values)
	if len(data) <= PageSize {
		return t.alloc(data), nil
	}

	mid := len(keys) / 2
	left := buildLeaf(keys[:mid], values[:mid])
	right := buildLeaf(keys[mid:], values[mid:])
	return 0, &split{
		left:  t.alloc(left),
		right: t.alloc(right),
		key:   cloneKey(keys[mid]),
	}
}

func (t *Tree) insertInternal(node, key, value []byte) (uint64, *split) {
	nkeys := int(nodeNKeys(node))
	idx := int(upperBound(node, key))

	child := nodeGetPtr(node, uint16(idx))
	newChild, sp := t.insertNode(child, key, value)
	t.del(child)

	oldKeys := make([][]byte, nkeys)
	for i := 0; i < nkeys; i++ {
		oldKeys[i] = nodeKey(node, uint16(i))
	}
	oldPtrs := make([]uint64, nkeys+1)
	for i := 0; i <= nkeys; i++ {
		oldPtrs[i] = nodeGetPtr(node, uint16(i))
	}

	var keys [][]byte
	var ptrs []uint64
	if sp == nil {
		oldPtrs[idx] = newChild
		keys, ptrs = oldKeys, oldPtrs
	} else {
		keys = make([][]byte, 0, nkeys+1)
		keys = append(keys, oldKeys[:idx]...)
		keys = append(keys, sp.key)
		keys = append(keys, oldKeys[idx:]...)

		ptrs = make([]uint64, 0, nkeys+2)
		ptrs = append(ptrs, oldPtrs[:idx]...)
		ptrs = append(ptrs, sp.left, sp.right)
		ptrs = append(ptrs, oldPtrs[idx+1:]...)
	}

	data := buildInternal(keys, ptrs)
	if len(data) <= PageSize {
		return t.alloc(data), nil
	}

	mid := len(keys) / 2
	promoted := cloneKey(keys[mid])
	left := buildInternal(keys[:mid], ptrs[:mid+1])
	right := buildInternal(keys[mid+1:], ptrs[mid+1:])
	return 0, &split{
		left:  t.alloc(left),
		right: t.alloc(right),
		key:   promoted,
	}
}

// Get returns the value for key.
func (t *Tree) Get(key []byte) ([]byte, bool) {
	if t.root == 0 {
		return nil, false
	}
	pageNo := t.root
	for {
		node := t.get(pageNo)
		switch nodeType(node) {
		case TypeLeaf:
			idx := upperBound(node, key)
			if idx == 0 || !bytes.Equal(nodeKey(node, idx-1), key) {
				return nil, false
			}
			return nodeValue(node, idx-1), true
		case TypeInternal:
			pageNo = nodeGetPtr(node, upperBound(node, key))
		default:
			panic(fmt.Sprintf("btree: unknown node type %d", nodeType(node)))
		}
	}
}

// Scan visits keys in [start, end). A nil start or end means unbounded. The
// callback receives slices owned by the tree and must not retain them; it
// returns false to stop the scan.
func (t *Tree) Scan(start, end []byte, fn func(key, value []byte) bool) {
	if t.root == 0 {
		return
	}
	t.scanNode(t.root, start, end, fn)
}

func (t *Tree) scanNode(pageNo uint64, start, end []byte, fn func(key, value []byte) bool) bool {
	node := t.get(pageNo)
	switch nodeType(node) {
	case TypeLeaf:
		nkeys := int(nodeNKeys(node))
		first := 0
		if start != nil {
			first = int(lowerBound(node, start))
		}
		for i := first; i < nkeys; i++ {
			k := nodeKey(node, uint16(i))
			if end != nil && bytes.Compare(k, end) >= 0 {
				return true
			}
			if !fn(k, nodeValue(node, uint16(i))) {
				return false
			}
		}
		return true
	case TypeInternal:
		nkeys := int(nodeNKeys(node))
		for i := 0; i <= nkeys; i++ {
			var lower, upper []byte
			if i > 0 {
				lower = nodeKey(node, uint16(i-1))
			}
			if i < nkeys {
				upper = nodeKey(node, uint16(i))
			}
			if start != nil && upper != nil && bytes.Compare(upper, start) <= 0 {
				continue
			}
			if end != nil && lower != nil && bytes.Compare(lower, end) >= 0 {
				continue
			}
			if !t.scanNode(nodeGetPtr(node, uint16(i)), start, end, fn) {
				return false
			}
		}
		return true
	default:
		panic(fmt.Sprintf("btree: unknown node type %d", nodeType(node)))
	}
}

// Depth returns the number of levels in the tree (0 for an empty tree).
func (t *Tree) Depth() int {
	if t.root == 0 {
		return 0
	}
	depth := 1
	pageNo := t.root
	for {
		node := t.get(pageNo)
		if nodeType(node) == TypeLeaf {
			return depth
		}
		pageNo = nodeGetPtr(node, 0)
		depth++
	}
}
