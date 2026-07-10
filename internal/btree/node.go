package btree

const (
	PageSize = 4096

	NodeTypeLeaf     NodeType = 1
	NodeTypeInternal NodeType = 2
)

type NodeType uint16

type Node struct {
	Type   NodeType
	Keys   [][]byte
	Values [][]byte
}

func NewLeaf() Node {
	return Node{Type: NodeTypeLeaf}
}
