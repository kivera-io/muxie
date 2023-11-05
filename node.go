package muxie

import (
	"net/http"
	"sort"
	"strings"
)

// Node is the trie's node which path patterns with their data like an HTTP handler are saved to.
// See `Trie` too.
type Node struct {
	parent *Node

	nodeType string

	children               map[string]*Node
	childNamedParameter    bool // is the child a named parameter (single segmnet)
	childWildcardParameter bool // or it is a wildcard (can be more than one path segments) ?
	childPrefixParameter   bool // or it is a prefixed parameter
	childSuffixParameter   bool // or it is a suffixed parameter

	childPrefixLengths []int
	childSuffixLengths []int

	paramCount int

	paramKeys []string // the param keys without : or *.
	end       bool     // it is a complete node, here we stop and we can say that the node is valid.
	key       string   // if end == true then key is filled with the original value of the insertion's key.
	// if key != "" && its parent has childWildcardParameter == true,
	// we need it to track the static part for the closest-wildcard's parameter storage.

	// insert main data relative to http and a tag for things like route names.
	Handler http.Handler
	Tag     string

	// other insert data.
	Data interface{}
}

// NewNode returns a new, empty, Node.
func NewNode() *Node {
	n := new(Node)
	return n
}

func (n *Node) addChild(s string, child *Node) {
	if n.children == nil {
		n.children = make(map[string]*Node)
	}

	if _, exists := n.children[s]; exists {
		return
	}

	child.parent = n
	n.children[s] = child
}

func (n *Node) addPrefixLength(l int) {
	addLength(&n.childPrefixLengths, l)
}

func (n *Node) addSuffixLength(l int) {
	addLength(&n.childSuffixLengths, l)
}

func addLength(paramSlice *[]int, l int) {
	if paramSlice == nil {
		paramSlice = &[]int{l}
	} else {
		*paramSlice = append(*paramSlice, l)
		paramDesc := []int{}
		m := map[int]struct{}{}
		for _, val := range *paramSlice {
			if _, ok := m[val]; !ok {
				m[val] = struct{}{}
				paramDesc = append(paramDesc, val)
			}
		}
		sort.Slice(paramDesc, func(i, j int) bool {
			return paramDesc[i] > paramDesc[j]
		})
		paramSlice = &paramDesc
	}
}

func (n *Node) getChild(s string) *Node {
	if n.children == nil {
		return nil
	}

	return n.children[s]
}

func (n *Node) hasChild(s string) bool {
	return n.getChild(s) != nil
}

func (n *Node) getPrefixParamChild(s string) (*Node, bool) {
	if !n.childPrefixParameter {
		return nil, false
	}
	sLen := len(s)
	for _, indx := range n.childPrefixLengths {
		// Lengths are in descending order
		if indx > sLen {
			continue
		}
		child := n.getChild(s[:indx] + PrefixParamStart)
		if child != nil {
			return child, true
		}
	}
	return nil, false
}

func (n *Node) getSuffixParamChild(s string) (*Node, bool) {
	if !n.childSuffixParameter {
		return nil, false
	}
	sLen := len(s)
	for _, suffixLen := range n.childSuffixLengths {
		// Lengths are in descending order
		indx := sLen - suffixLen
		if indx < 0 {
			continue
		}
		child := n.getChild(SuffixParamStart + s[indx:])
		if child != nil {
			return child, true
		}
	}
	return nil, false
}

func (n *Node) findClosestParentWildcardNode() *Node {
	n = n.parent
	for n != nil {
		if n.childWildcardParameter {
			return n.getChild(WildcardParamStart)
		}
		n = n.parent
	}

	return nil
}

func (n *Node) findClosestUnvisitedNode(visitedNodes map[*Node]struct{}, path string, i int) (*Node, int, int) {
	n = n.parent
	var start int
	for n != nil {
		i = strings.LastIndex(path[:i], pathSep)
		if i == -1 {
			i = 0
		}
		start = strings.LastIndex(path[:i], pathSep) + 1
		segment := path[start:i]
		if child, exists := n.getPrefixParamChild(segment); exists {
			if _, visited := visitedNodes[child]; !visited {
				return child, start, i
			}
		}
		if child, exists := n.getSuffixParamChild(segment); exists {
			if _, visited := visitedNodes[child]; !visited {
				return child, start, i
			}
		}
		if n.childNamedParameter {
			child := n.getChild(ParamStart)
			if _, visited := visitedNodes[child]; !visited {
				return child, start, i
			}
		}
		if n.childWildcardParameter {
			child := n.getChild(WildcardParamStart)
			if _, visited := visitedNodes[child]; !visited {
				return child, start, i
			}
		}

		n = n.parent
	}

	return nil, start, i
}

// NodeKeysSorter is the type definition for the sorting logic
// that caller can pass on `GetKeys` and `Autocomplete`.
type NodeKeysSorter = func(list []string) func(i, j int) bool

// DefaultKeysSorter sorts as: first the "key (the path)" with the lowest number of slashes.
var DefaultKeysSorter = func(list []string) func(i, j int) bool {
	return func(i, j int) bool {
		return len(strings.Split(list[i], pathSep)) < len(strings.Split(list[j], pathSep))
	}
}

// Keys returns this node's key (if it's a final path segment)
// and its children's node's key. The "sorter" can be optionally used to sort the result.
func (n *Node) Keys(sorter NodeKeysSorter) (list []string) {
	if n == nil {
		return
	}

	if n.end {
		list = append(list, n.key)
	}

	if n.children != nil {
		for _, child := range n.children {
			list = append(list, child.Keys(sorter)...)
		}
	}

	if sorter != nil {
		sort.Slice(list, sorter(list))
	}

	return
}

// Parent returns the parent of that node, can return nil if this is the root node.
func (n *Node) Parent() *Node {
	return n.parent
}

// String returns the key, which is the path pattern for the HTTP Mux.
func (n *Node) String() string {
	return n.key
}

// IsEnd returns true if this Node is a final path, has a key.
func (n *Node) IsEnd() bool {
	return n.end
}
