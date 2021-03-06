package mmdbwriter

import (
	"net"
	"reflect"

	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/pkg/errors"
)

type recordType byte

const (
	recordTypeEmpty recordType = iota
	recordTypeData
	recordTypeNode
	recordTypeAlias
	recordTypeFixedNode
	recordTypeReserved
)

type record struct {
	node       *node
	value      mmdbtype.DataType
	recordType recordType
}

// each node contains two records.
type node struct {
	children [2]record
	nodeNum  int
}

func (n *node) insert(
	ip net.IP,
	prefixLen int,
	recordType recordType,
	inserter func(value mmdbtype.DataType) (mmdbtype.DataType, error),
	insertedNode *node,
	currentDepth int,
) error {
	newDepth := currentDepth + 1
	// Check if we are inside the network already
	if newDepth > prefixLen {
		// Data already exists for the network so insert into all the children.
		// We will prune duplicate nodes when we finalize.
		err := n.children[0].insert(ip, prefixLen, recordType, inserter, insertedNode, newDepth)
		if err != nil {
			return err
		}
		return n.children[1].insert(ip, prefixLen, recordType, inserter, insertedNode, newDepth)
	}

	// We haven't reached the network yet.
	pos := bitAt(ip, currentDepth)
	r := &n.children[pos]
	return r.insert(ip, prefixLen, recordType, inserter, insertedNode, newDepth)
}

func (r *record) insert(
	ip net.IP,
	prefixLen int,
	recordType recordType,
	inserter func(value mmdbtype.DataType) (mmdbtype.DataType, error),
	insertedNode *node,
	newDepth int,
) error {
	switch r.recordType {
	case recordTypeNode, recordTypeFixedNode:
	case recordTypeEmpty, recordTypeData:
		// When we add record merging support, it should go here.
		if newDepth >= prefixLen {
			r.node = insertedNode
			r.recordType = recordType
			if recordType == recordTypeData {
				var err error
				r.value, err = inserter(r.value)
				if err != nil {
					return err
				}
				if r.value == nil {
					r.recordType = recordTypeEmpty
				}
			} else {
				r.value = nil
			}
			return nil
		}

		// We are splitting this record so we create two duplicate child
		// records.
		r.node = &node{children: [2]record{*r, *r}}
		r.value = nil
		r.recordType = recordTypeNode
	case recordTypeReserved:
		if prefixLen >= newDepth {
			return errors.Errorf(
				"attempt to insert %s/%d, which is in a reserved network",
				ip,
				prefixLen,
			)
		}
		// If we are inserting a network that contains a reserved network,
		// we silently remove the reserved network.
		return nil
	case recordTypeAlias:
		if prefixLen < newDepth {
			// Do nothing. We are inserting a network that contains an aliased
			// network. We silently ignore.
			return nil
		}
		// attempting to insert _into_ an aliased network
		return errors.Errorf(
			"attempt to insert %s/%d, which is in an aliased network",
			ip,
			prefixLen,
		)
	default:
		return errors.Errorf("inserting into record type %d not implemented!", r.recordType)
	}

	return r.node.insert(ip, prefixLen, recordType, inserter, insertedNode, newDepth)
}

func (n *node) get(
	ip net.IP,
	depth int,
) (int, record) {
	r := n.children[bitAt(ip, depth)]

	depth++

	switch r.recordType {
	case recordTypeNode, recordTypeAlias, recordTypeFixedNode:
		return r.node.get(ip, depth)
	default:
		return depth, r
	}
}

// finalize prunes unnecessary nodes (e.g., where the two records are the same) and
// sets the node number for the node. It returns a record pointer that is nil if
// the node is not mergeable or the value of the merged record if it can be merged.
// The second return value is the current node count, including the subtree.
func (n *node) finalize(currentNum int) (*record, int) {
	n.nodeNum = currentNum
	currentNum++

	for i := 0; i < 2; i++ {
		switch n.children[i].recordType {
		case recordTypeFixedNode:
			// We don't consider merging for fixed nodes
			_, currentNum = n.children[i].node.finalize(currentNum)
		case recordTypeNode:
			record, newCurrentNum := n.children[i].node.finalize(currentNum)
			if record == nil {
				// nothing to merge. Use current number from child.
				currentNum = newCurrentNum
			} else {
				n.children[i] = *record
			}
		default:
		}
	}

	if n.children[0].recordType == n.children[1].recordType &&
		(n.children[0].recordType == recordTypeEmpty ||
			(n.children[0].recordType == recordTypeData &&
				reflect.DeepEqual(n.children[0].value, n.children[1].value))) {
		return &record{
			recordType: n.children[0].recordType,
			value:      n.children[0].value,
		}, currentNum
	}

	return nil, currentNum
}

func bitAt(ip net.IP, depth int) byte {
	return (ip[depth/8] >> (7 - (depth % 8))) & 1
}
