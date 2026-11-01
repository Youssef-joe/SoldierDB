package btree

import "bytes"

// Delete removes key from the tree and reports whether it was present.
func (t *Tree) Delete(key []byte) bool {
	if t.root == 0 {
		return false
	}

	newRoot, removed := t.deleteNode(t.root, key)
	if !removed {
		return false
	}
	t.del(t.root)

	// Collapse a root that no longer needs an internal node.
	for newRoot != 0 {
		node := t.get(newRoot)
		if nodeType(node) == TypeInternal && nodeNKeys(node) == 0 {
			only := nodeGetPtr(node, 0)
			t.del(newRoot)
			newRoot = only
			continue
		}
		break
	}

	t.root = newRoot
	return true
}

// deleteNode returns the replacement page for the subtree and whether the key
// was removed. A zero page means the subtree became empty.
func (t *Tree) deleteNode(pageNo uint64, key []byte) (uint64, bool) {
	node := t.get(pageNo)
	switch nodeType(node) {
	case TypeLeaf:
		return t.deleteLeaf(node, key)
	case TypeInternal:
		return t.deleteInternal(node, key)
	default:
		panic("btree: unknown node type")
	}
}

func (t *Tree) deleteLeaf(node, key []byte) (uint64, bool) {
	nkeys := int(nodeNKeys(node))
	idx := int(upperBound(node, key))
	if idx == 0 || !bytes.Equal(nodeKey(node, uint16(idx-1)), key) {
		return 0, false
	}
	idx--

	keys := make([][]byte, 0, nkeys-1)
	values := make([][]byte, 0, nkeys-1)
	for i := 0; i < nkeys; i++ {
		if i == idx {
			continue
		}
		keys = append(keys, nodeKey(node, uint16(i)))
		values = append(values, nodeValue(node, uint16(i)))
	}
	if len(keys) == 0 {
		return 0, true
	}
	return t.alloc(buildLeaf(keys, values)), true
}

func (t *Tree) deleteInternal(node, key []byte) (uint64, bool) {
	nkeys := int(nodeNKeys(node))
	idx := int(upperBound(node, key))
	child := nodeGetPtr(node, uint16(idx))

	newChild, removed := t.deleteNode(child, key)
	if !removed {
		return 0, false
	}
	t.del(child)

	keys := make([][]byte, nkeys)
	ptrs := make([]uint64, nkeys+1)
	for i := 0; i < nkeys; i++ {
		keys[i] = nodeKey(node, uint16(i))
	}
	for i := 0; i <= nkeys; i++ {
		ptrs[i] = nodeGetPtr(node, uint16(i))
	}
	ptrs[idx] = newChild

	if newChild == 0 {
		// Remove the empty child and the separator next to it.
		if idx > 0 {
			keys = append(keys[:idx-1], keys[idx:]...)
		} else {
			keys = append(keys[:0], keys[1:]...)
		}
		ptrs = append(ptrs[:idx], ptrs[idx+1:]...)
	} else if int(nodeNKeys(t.get(newChild))) <= 1 && nodeType(t.get(newChild)) == TypeLeaf {
		// Underfull leaf: merge with a sibling when the result fits.
		keys, ptrs = t.mergeLeafSiblings(keys, ptrs, idx)
	}

	if len(keys) == 0 {
		return ptrs[0], true
	}
	return t.alloc(buildInternal(keys, ptrs)), true
}

// mergeLeafSiblings merges the leaf at ptrs[idx] with an adjacent leaf. It
// returns the updated key and pointer slices.
func (t *Tree) mergeLeafSiblings(keys [][]byte, ptrs []uint64, idx int) ([][]byte, []uint64) {
	child := t.get(ptrs[idx])
	if nodeType(child) != TypeLeaf {
		return keys, ptrs
	}

	tryLeft := idx > 0
	if tryLeft {
		leftPage := ptrs[idx-1]
		left := t.get(leftPage)
		if nodeType(left) == TypeLeaf {
			merged := mergeLeaves(left, child)
			if merged != nil {
				mp := t.alloc(merged)
				t.del(leftPage)
				t.del(ptrs[idx])
				ptrs[idx-1] = mp
				ptrs = append(ptrs[:idx], ptrs[idx+1:]...)
				keys = append(keys[:idx-1], keys[idx:]...)
				return keys, ptrs
			}
		}
	}

	if idx < len(keys) {
		rightPage := ptrs[idx+1]
		right := t.get(rightPage)
		if nodeType(right) == TypeLeaf {
			merged := mergeLeaves(child, right)
			if merged != nil {
				mp := t.alloc(merged)
				t.del(ptrs[idx])
				t.del(rightPage)
				ptrs[idx] = mp
				ptrs = append(ptrs[:idx+1], ptrs[idx+2:]...)
				keys = append(keys[:idx], keys[idx+1:]...)
				return keys, ptrs
			}
		}
	}
	return keys, ptrs
}

func mergeLeaves(left, right []byte) []byte {
	nl := int(nodeNKeys(left))
	nr := int(nodeNKeys(right))
	keys := make([][]byte, 0, nl+nr)
	values := make([][]byte, 0, nl+nr)
	for i := 0; i < nl; i++ {
		keys = append(keys, nodeKey(left, uint16(i)))
		values = append(values, nodeValue(left, uint16(i)))
	}
	for i := 0; i < nr; i++ {
		keys = append(keys, nodeKey(right, uint16(i)))
		values = append(values, nodeValue(right, uint16(i)))
	}
	merged := buildLeaf(keys, values)
	if len(merged) > PageSize {
		return nil
	}
	return merged
}
