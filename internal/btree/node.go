package btree

import (
	"bytes"
	"encoding/binary"
	"myowndb/internal/btree"

	"github.com/go-playground/validator/v10/translations/id"
	"github.com/zeebo/assert"
)

const (
	btree_page_size = 4096

	NodeTypeLeaf     NodeType = 1
	NodeTypeInternal NodeType = 2
	HEADER = 4
	btree_max_key_size = 1000
	btree_max_val_size = 3000
	BNODE_NODE = 1 // internal nodes without values
	BNODE_LEAF = 2 // laef nodes with values
)

type BNODE []byte //let's see how that can be dumbed to the disk
type b_tree struct {
	 // pointer (non zero page number)
	root uint64
	// callbacks for managing on-disk pages
	get func(uint64) []byte // reads from disk
	new func([]byte) uint64	// allocates and writes a new page (copy-on-write)
	del func(uint64) // deallocate a page
}

type NodeType uint16

type Node struct {
	Type   NodeType
	Keys   [][]byte
	Values [][]byte
}

func NewLeaf() Node {
	return Node{Type: NodeTypeLeaf}
}

func init() {
	node1max := HEADER + 8 + 2 + 4 + btree_max_key_size + btree_max_val_size
	assert(node1max <= btree_page_size) // here is the maximum kv
}

func (node BNODE) btype() uint16 {
	return binary.LittleEndian.Uint16(node[0:2])
}

func (node BNODE) nkeys() uint16 {
	return binary.LittleEndian.Uint16(node[2:4])
}

func (node BNODE) set_header(btype uint16, nkeys uint16) {
	binary.LittleEndian.PutUint16(node[0:2], btype)
	binary.LittleEndian.PutUint16(node[0:2], btype)
	
}

//child pointers
func (node BNODE) get_ptr(idx uint16) uint64 {
	assert(idx < node.nkeys())
	pos := HEADER + 8*idx
	return binary.LittleEndian.Uint64(node[pos:])
}
func (node BNODE) set_ptr(idx uint16, val uint64)


//offset list 
func offset_pos(node BNODE, idx uint16) uint16 {
	assert(1 <= idx && idx <= node.nkeys())
	return HEADER + 8*node.nkeys() + 2*(idx-1)
}
func (node BNODE) getOffset(idx uint16) uint16 {
	if idx == 0 {
		return 0
	}
	return binary.LittleEndian.Uint16(node[offset_pos(node, idx):])
}
func (node BNODE) set_offset(idx uint16, offset uint16)


//key-values
func(node BNODE) kv_pos(idx uint16) uint16 {
	assert(idx <= node.nkeys())
	return HEADER + 8*node.nkeys() + 2*node.nkeys() + node.getOffset(idx)
}
func (node BNODE) get_key(idx uint16) []byte {
	assert(idx < node.nkeys())
	pos := node.kv_pos(idx)
	klen := binary.LittleEndian.Uint16(node[pos:])
	return node[pos+4:][:klen]
}
func (node BNODE) get_val(idx uint16) []byte

//node size in bytes
func (node BNODE) nbytes() uint16 {
	return node.kv_pos(node.nkeys())
}

//kv lookups within a node
// that returns the first kid node whose range intersects the key (kid[id] <= key)
// TODO : binary search
func nodeLookupLE(node BNODE, key []byte) uint16 {
	nkeys := node.nkeys()
	found := uint16(0)
	// the first key is a copy from the parent node,
	// thus it is always less than or equal to the key
	for i := uint16(1); i < nkeys; i++ {
		cmp := bytes.Compare(node.get_key(i), key)
		if cmp <= 0 {
			found = i
		}
		if cmp >= 0 {
			break
		}
	
	}
		return found
		//the function is called nodeLookupLE cuz it uses the LESS-Than-Or-Equal operator
		// for point queries, we should use the equal operator instead, which is a step we can add later
}

// let's add a new key to the leaf
func leafInsert(
	new BNODE, old BNODE, idx uint16,
	key []byte, val []byte,
) {
	new.set_header(BNODE_LEAF, old.nkeys()+1) //setting up the header
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendKv(new, idx, key, val)
	nodeAppendRange(new, old, idx+1, idx, old.nkeys()-idx)
}

//node copying functions
// nodeAppendRange ccopies a range of KVs and nodeAppendKV copies a KV pair, this must be done
// in order cuz these functions rely on the previous offset
// here is we copy KV into the position
func nodeAppendKv(new BNODE, idx uint16, ptr uint64, key []byte, val []byte) {
	//ptrs
	new.set_ptr(idx, ptr)
	//KVs
	pos := new.kv_pos(idx)
	binary.LittleEndian.PutUint16(new[pos+0:], uint16(len(key)))
	binary.LittleEndian.PutUint16(new[pos+2:], uint16(len(val)))
	copy(new[pos+4:], key)
	copy(new[pos+4+uint16(len(key)):], val)
	// the offset of the next key
	new.set_offset(idx+1, new.getOffset(idx)+4+uint16(len(key)+len(val)))
}

// copy multiple KVs into the position from the old node
	func nodeAppendRange (
		new BNODE, old BNODE,
		dstNew uint16, srcOld uint16, n uint16
	)

// replace a link with one or multiple links
func nodeReplaceKidN (
	tree *b_tree, new BNODE, old BNODE, idx uint16
	kids ...BNODE
) {
	inc := uint16(len(kids))
	new.set_header(BNODE_NODE, old.nkeys()+inc-1)
	nodeAppendRange(new, old, 0, 0, idx)
	for i, node := range kids {
		nodeAppendKv(new, idx+uint16(i), tree.new(node), node.get_key(0), nil)
		//                position       pointer        key              val 
		// tree.new callback is used to allocate the child nodes  
	}
	nodeAppendRange(new, old, idx+inc, idx+1, old.nkeys()-(idx+1))
}

// split an oversized node into 2 so that the 2nd node always fits on a page
func nodeSplit2(left BNODE, right BNODE, old BNODE) {
	// code omiited
}

// split a node if it's too big. the results are 1-3 nodes
func nodeSplit3(old BNODE) (uint16, [3]BNODE) {
	if old.nbytes() <= btree_page_size {
		old = old[:btree_page_size]
		return 1, [3]BNODE{old} // node split
	}
	left := BNODE(make([]byte, 2*btree_page_size)) //might be split later
	right := BNODE(make([]byte, btree_page_size))
	nodeSplit2(left, right, old)
	if left.nbytes() <= btree_page_size {
		left = left[:btree_page_size]
		return 2, [3]BNODE{left, right} //2 nodes
	}
	left_left := BNODE(make([]byte, btree_page_size))
	middle := BNODE(make([]byte, btree_page_size))
	nodeSplit2(left_left, middle, left)
	assert(left_left.nbytes() <= btree_page_size)
	
	return 3, [3]BNODE{left_left, middle, right} // 3 nodeSplit3
	// the returned nodes are allocated from memory; they are just temporary data until nodeReplaceKidN actually allocate them
}


//insert a KV into a node, the result might be split
// the caller is responsible for deallocating the input node
// and splitting and allocating result nodes..
func tree_insert(tree *b_tree, node BNODE, key []byte, val []byte) BNODE {
	
}