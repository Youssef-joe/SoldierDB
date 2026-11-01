package btree

import (
	"bytes"
	"encoding/binary"
	"sort"
)

const (
	// PageSize is the page size used by the tree.
	PageSize = 4096
	// MaxKeySize is the largest key the tree accepts.
	MaxKeySize = 1000
	// MaxValueSize is the largest value the tree accepts.
	MaxValueSize = 3000

	headerSize = 4

	// TypeInternal marks an internal (branch) node.
	TypeInternal uint16 = 1
	// TypeLeaf marks a leaf node.
	TypeLeaf uint16 = 2
)

// Node layout:
//
//	| type (2) | nkeys (2) | ptrs (8*(nkeys+1)) | offsets (2*nkeys) | kvs |
//
// For leaf nodes a key-value is | klen (2) | vlen (2) | key | value |.
// For internal nodes a key-value is | klen (2) | key | and child pointers live
// in the ptrs array. Internal key i is the smallest key in subtree ptrs[i+1].
func nodeType(node []byte) uint16 {
	return binary.LittleEndian.Uint16(node[0:2])
}

func nodeNKeys(node []byte) uint16 {
	return binary.LittleEndian.Uint16(node[2:4])
}

func nodeSetHeader(node []byte, typ, nkeys uint16) {
	binary.LittleEndian.PutUint16(node[0:2], typ)
	binary.LittleEndian.PutUint16(node[2:4], nkeys)
}

func nodePtrPos(idx uint16) int {
	return headerSize + 8*int(idx)
}

func nodeGetPtr(node []byte, idx uint16) uint64 {
	return binary.LittleEndian.Uint64(node[nodePtrPos(idx):])
}

func nodeSetPtr(node []byte, idx uint16, ptr uint64) {
	binary.LittleEndian.PutUint64(node[nodePtrPos(idx):], ptr)
}

func nodeOffsetPos(nkeys, idx uint16) int {
	return headerSize + 8*(int(nkeys)+1) + 2*int(idx)
}

func nodeGetOffset(node []byte, idx uint16) uint16 {
	if idx == 0 {
		return 0
	}
	nkeys := nodeNKeys(node)
	return binary.LittleEndian.Uint16(node[nodeOffsetPos(nkeys, idx-1):])
}

func nodeSetOffset(node []byte, idx, offset uint16) {
	nkeys := nodeNKeys(node)
	binary.LittleEndian.PutUint16(node[nodeOffsetPos(nkeys, idx):], offset)
}

func nodeKVPos(node []byte, idx uint16) int {
	nkeys := nodeNKeys(node)
	return headerSize + 8*(int(nkeys)+1) + 2*int(nkeys) + int(nodeGetOffset(node, idx))
}

func nodeKey(node []byte, idx uint16) []byte {
	pos := nodeKVPos(node, idx)
	klen := int(binary.LittleEndian.Uint16(node[pos:]))
	pos += 2
	if nodeType(node) == TypeLeaf {
		pos += 2 // vlen
	}
	return node[pos : pos+klen]
}

func nodeValue(node []byte, idx uint16) []byte {
	pos := nodeKVPos(node, idx)
	klen := int(binary.LittleEndian.Uint16(node[pos:]))
	vlen := int(binary.LittleEndian.Uint16(node[pos+2:]))
	pos += 4
	return node[pos+klen : pos+klen+vlen]
}

// nodeUsed returns the number of bytes used by the node.
func nodeUsed(node []byte) int {
	nkeys := nodeNKeys(node)
	if nkeys == 0 {
		return headerSize + 8
	}
	last := nkeys - 1
	pos := nodeKVPos(node, last)
	klen := int(binary.LittleEndian.Uint16(node[pos:]))
	pos += 2
	if nodeType(node) == TypeLeaf {
		vlen := int(binary.LittleEndian.Uint16(node[pos:]))
		pos += 2 + klen + vlen
	} else {
		pos += klen
	}
	return pos
}

func buildLeaf(keys, values [][]byte) []byte {
	nkeys := len(keys)
	used := headerSize + 8*(nkeys+1) + 2*nkeys
	for i := range keys {
		used += 4 + len(keys[i]) + len(values[i])
	}
	buf := make([]byte, used)
	nodeSetHeader(buf, TypeLeaf, uint16(nkeys))
	kvStart := headerSize + 8*(nkeys+1) + 2*nkeys
	pos := kvStart
	for i := range keys {
		klen := len(keys[i])
		vlen := len(values[i])
		binary.LittleEndian.PutUint16(buf[pos:], uint16(klen))
		binary.LittleEndian.PutUint16(buf[pos+2:], uint16(vlen))
		pos += 4
		copy(buf[pos:], keys[i])
		pos += klen
		copy(buf[pos:], values[i])
		pos += vlen
		nodeSetOffset(buf, uint16(i), uint16(pos-kvStart))
	}
	return buf
}

func buildInternal(keys [][]byte, ptrs []uint64) []byte {
	nkeys := len(keys)
	if len(ptrs) != nkeys+1 {
		panic("btree: internal node needs nkeys+1 pointers")
	}
	used := headerSize + 8*(nkeys+1) + 2*nkeys
	for _, key := range keys {
		used += 2 + len(key)
	}
	buf := make([]byte, used)
	nodeSetHeader(buf, TypeInternal, uint16(nkeys))
	for i, ptr := range ptrs {
		nodeSetPtr(buf, uint16(i), ptr)
	}
	kvStart := headerSize + 8*(nkeys+1) + 2*nkeys
	pos := kvStart
	for i, key := range keys {
		binary.LittleEndian.PutUint16(buf[pos:], uint16(len(key)))
		pos += 2
		copy(buf[pos:], key)
		pos += len(key)
		nodeSetOffset(buf, uint16(i), uint16(pos-kvStart))
	}
	return buf
}

// upperBound returns the number of keys <= key, i.e. the first index whose key
// is strictly greater than the target.
func upperBound(node []byte, key []byte) uint16 {
	nkeys := int(nodeNKeys(node))
	idx := sort.Search(nkeys, func(i int) bool {
		return bytes.Compare(nodeKey(node, uint16(i)), key) > 0
	})
	return uint16(idx)
}

// lowerBound returns the first index whose key is >= target.
func lowerBound(node []byte, key []byte) uint16 {
	nkeys := int(nodeNKeys(node))
	idx := sort.Search(nkeys, func(i int) bool {
		return bytes.Compare(nodeKey(node, uint16(i)), key) >= 0
	})
	return uint16(idx)
}

// cloneKey copies a key so it survives reuse of the source page.
func cloneKey(key []byte) []byte {
	return append([]byte(nil), key...)
}
