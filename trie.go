package muxie

import (
	"net/http"
	"strings"
)

const (
	// ParamStart is the character, as a string, which a path pattern starts to define its named parameter.
	ParamStart = ":"
	// WildcardParamStart is the character, as a string, which a path pattern starts to define its named parameter for wildcards.
	// It allows everything else after that path prefix
	// but the Trie checks for static paths and named parameters before that in order to support everything that other implementations do not,
	// and if nothing else found then it tries to find the closest wildcard path(super and unique).
	WildcardParamStart = "*"

	PrefixParamStart = "+:"

	SuffixParamStart = "-:"
)

// Trie contains the main logic for adding and searching nodes for path segments.
// It supports wildcard and named path parameters.
// Trie supports very coblex and useful path patterns for routes.
// The Trie checks for static paths(path without : or *) and named parameters before that in order to support everything that other implementations do not,
// and if nothing else found then it tries to find the closest wildcard path(super and unique).
type Trie struct {
	root *Node

	// if true then it will handle any path if not other parent wildcard exists,
	// so even 404 (on http services) is up to it, see Trie#Insert.
	hasRootWildcard bool

	hasRootSlash bool

	// If true then named path parameters that weren't explored will be considered if no match is found,
	// useful in situations where a named parameter value conflicts with segment in a fixed path.
	// For example:
	// path1: /a/b/c/z
	// path2: /a/:p1/c/d
	// req: http://localhost:8080/a/b/c/d
	// with searchUnvisitedParams == false => not found!
	// with searchUnvisitedParams == true => found path2
	searchUnvisitedParams bool

	caseInsensitive bool
}

type TrieOptions struct {
	CaseInsensitive       bool
	SearchUnvisitedParams bool
}

// NewTrie returns a new, empty Trie.
// It is only useful for end-developers that want to design their own mux/router based on my trie implementation.
//
// See `Trie`
func NewTrie() *Trie {
	return &Trie{
		root:                  NewNode(),
		hasRootWildcard:       false,
		caseInsensitive:       false,
		searchUnvisitedParams: false,
	}
}

func NewTrieWithOptions(options TrieOptions) *Trie {
	return &Trie{
		root:                  NewNode(),
		hasRootWildcard:       false,
		caseInsensitive:       options.CaseInsensitive,
		searchUnvisitedParams: options.SearchUnvisitedParams,
	}
}

// Sets the option to search invisited named parameter nodes
func (t *Trie) SearchUnvisitedParams() *Trie {
	t.searchUnvisitedParams = true
	return t
}

// Sets the trie to match paths without case sensitivity
func (t *Trie) CaseInsensitive() *Trie {
	t.caseInsensitive = true
	return t
}

// InsertOption is just a function which accepts a pointer to a Node which can alt its `Handler`, `Tag` and `Data`  fields.
//
// See `WithHandler`, `WithTag` and `WithData`.
type InsertOption func(*Node)

// WithHandler sets the node's `Handler` field (useful for HTTP).
func WithHandler(handler http.Handler) InsertOption {
	if handler == nil {
		panic("muxie/WithHandler: empty handler")
	}

	return func(n *Node) {
		if n.Handler == nil {
			n.Handler = handler
		}
	}
}

// WithTag sets the node's `Tag` field (may be useful for HTTP).
func WithTag(tag string) InsertOption {
	return func(n *Node) {
		if n.Tag == "" {
			n.Tag = tag
		}
	}
}

// WithData sets the node's optionally `Data` field.
func WithData(data interface{}) InsertOption {
	return func(n *Node) {
		// data can be replaced.
		n.Data = data
	}
}

// Insert adds a node to the trie.
func (t *Trie) Insert(pattern string, options ...InsertOption) {
	if pattern == "" {
		panic("muxie/trie#Insert: empty pattern")
	}

	n := t.insert(pattern, "", nil, nil)
	for _, opt := range options {
		opt(n)
	}
}

const (
	pathSep  = "/"
	pathSepB = '/'
)

func slowPathSplit(path string) []string {
	if path == pathSep {
		return []string{pathSep}
	}

	// remove last sep if any.
	if path[len(path)-1] == pathSepB {
		path = path[:len(path)-1]
	}

	return strings.Split(path, pathSep)[1:]
}

func resolveStaticPart(key string) string {
	i := strings.Index(key, ParamStart)
	if i == -1 {
		i = strings.Index(key, WildcardParamStart)
	}
	if i == -1 {
		i = len(key)
	}

	return key[:i]
}

func isPrefixParam(key string) bool {
	return strings.Contains(key, PrefixParamStart)
}

func isSuffixParam(key string) bool {
	return strings.Contains(key, SuffixParamStart)
}

func (t *Trie) insert(key, tag string, optionalData interface{}, handler http.Handler) *Node {
	input := slowPathSplit(key)

	n := t.root
	if key == pathSep {
		t.hasRootSlash = true
	}

	var paramKeys []string

	for i, s := range input {
		c := s[0]
		n.pathIndex = i + 1
		n.paramCount = len(paramKeys)

		if isParam, isWildcard, isPrefixParam, isSuffixParam := c == ParamStart[0], c == WildcardParamStart[0], isPrefixParam(s), isSuffixParam(s); isParam || isWildcard || isPrefixParam || isSuffixParam {
			n.hasDynamicChild = true
			var indx int

			if isParam {
				paramKeys = append(paramKeys, s[1:]) // without :

				n.childNamedParameter = true
				s = ParamStart

			} else if isWildcard {
				paramKeys = append(paramKeys, s[1:]) // without *

				n.childWildcardParameter = true
				s = WildcardParamStart
				if t.root == n {
					t.hasRootWildcard = true
				}

			} else if isPrefixParam {
				indx = strings.Index(s, PrefixParamStart)
				paramKeys = append(paramKeys, s[indx+2:])

				n.childPrefixParameter = true
				n.addPrefixLength(indx)
				s = s[:indx+2]

			} else if isSuffixParam {
				indx = strings.Index(s, SuffixParamStart)
				paramKeys = append(paramKeys, s[:indx])

				n.childSuffixParameter = true
				n.addSuffixLength(len(s) - (indx + 2))
				s = s[indx:]
			}
		}

		if t.caseInsensitive {
			s = strings.ToLower(s)
		}

		if !n.hasChild(s) {
			child := NewNode()
			n.addChild(s, child)
		}

		n = n.getChild(s)
	}

	n.Tag = tag
	n.Handler = handler
	n.Data = optionalData

	n.paramKeys = paramKeys
	n.key = key
	n.staticKey = resolveStaticPart(key)
	n.end = true

	return n
}

// SearchPrefix returns the last node which holds the key which starts with "prefix".
func (t *Trie) SearchPrefix(prefix string) *Node {
	input := slowPathSplit(prefix)
	n := t.root

	for i := 0; i < len(input); i++ {
		s := input[i]
		if t.caseInsensitive {
			s = strings.ToLower(s)
		}
		if child := n.getChild(s); child != nil {
			n = child
			continue
		}

		return nil
	}

	return n
}

// Parents returns the list of nodes that a node with "prefix" key belongs to.
func (t *Trie) Parents(prefix string) (parents []*Node) {
	n := t.SearchPrefix(prefix)
	if n != nil {
		// without this node.
		n = n.Parent()
		for {
			if n == nil {
				break
			}

			if n.IsEnd() {
				parents = append(parents, n)
			}

			n = n.Parent()
		}
	}

	return
}

// HasPrefix returns true if "prefix" is found inside the registered nodes.
func (t *Trie) HasPrefix(prefix string) bool {
	return t.SearchPrefix(prefix) != nil
}

// Autocomplete returns the keys that starts with "prefix",
// this is useful for custom search-engines built on top of my trie implementation.
func (t *Trie) Autocomplete(prefix string, sorter NodeKeysSorter) (list []string) {
	n := t.SearchPrefix(prefix)
	if n != nil {
		list = n.Keys(sorter)
	}
	return
}

// ParamsSetter is the interface which should be implemented by the
// params writer for `Search` in order to store the found named path parameters, if any.
type ParamsSetter interface {
	Set(string, string)
}

// Append a parameter value to the paramValues slice
func appendParameterValue(paramValues *[]string, value string) {
	if ln := len(*paramValues); cap(*paramValues) > ln {
		*paramValues = (*paramValues)[:ln+1]
		(*paramValues)[ln] = value
	} else {
		*paramValues = append(*paramValues, value)
	}
}

// Helper function to return the minimum of two ints
func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// Search is the most important part of the Trie.
// It will try to find the responsible node for a specific query (or a request path for HTTP endpoints).
//
// Search supports searching for static paths(path without : or *) and paths that contain
// named parameters or wildcards.
// Priority as:
// 1. static paths
// 2. named parameters with ":"
// 3. wildcards
// 4. fixed segments treated as named parameters (if searchUnvisitedParams == true)
// 5. closest wildcard if not found, if any
// 6. root wildcard
func (t *Trie) Search(q string, params ParamsSetter) *Node {
	end := len(q)

	if end == 0 || (end == 1 && q[0] == pathSepB) {
		// fixes only root wildcard but no / registered at.
		if t.hasRootSlash {
			return t.root.getChild(pathSep)
		} else if t.hasRootWildcard {
			// no need to going through setting parameters, this one has not but it is wildcard.
			return t.root.getChild(WildcardParamStart)
		}

		return nil
	}

	n := t.root
	start := 1
	i := 1
	var paramValues []string
	visitedNodes := map[*Node]struct{}{}

	var qc string
	if t.caseInsensitive {
		qc = strings.ToLower(q)
	} else {
		qc = q
	}

	for {
		if i == end || q[i] == pathSepB {
			s := qc[start:i]
			if child := n.getChild(s); child != nil {
				n = child

			} else if child, exists := n.getPrefixParamChild(s); exists {
				n = child
				visitedNodes[n] = struct{}{}
				appendParameterValue(&paramValues, q[start:i])

			} else if child, exists := n.getSuffixParamChild(s); exists {
				n = child
				visitedNodes[n] = struct{}{}
				appendParameterValue(&paramValues, q[start:i])

			} else if n.childNamedParameter {
				n = n.getChild(ParamStart)
				visitedNodes[n] = struct{}{}
				appendParameterValue(&paramValues, q[start:i])

			} else if n.childWildcardParameter {
				n = n.getChild(WildcardParamStart)
				appendParameterValue(&paramValues, q[start:])
				break

			} else {
				var unvisited *Node
				if t.searchUnvisitedParams {
					unvisited, start, i = n.findClosestUnvisitedNode(visitedNodes, qc, i)
				}
				if unvisited != nil {
					n = unvisited
					visitedNodes[n] = struct{}{}
					lim := min(n.paramCount, cap(paramValues))
					paramValues = paramValues[:lim]
					appendParameterValue(&paramValues, q[start:i])
				} else {
					n = n.findClosestParentWildcardNode()
					if n != nil {
						// means that it has :param/static and *wildcard, we go trhough the :param
						// but the next path segment is not the /static, so go back to *wildcard
						// instead of not found.
						//
						// Fixes:
						// /hello/*p
						// /hello/:p1/static/:p2
						// req: http://localhost:8080/hello/dsadsa/static/dsadsa => found
						// req: http://localhost:8080/hello/dsadsa => but not found!
						// and
						// /second/wild/*p
						// /second/wild/static/otherstatic/
						// req: /second/wild/static/otherstatic/random => but not found!
						params.Set(n.paramKeys[0], q[len(n.staticKey):])
						return n
					}
					return nil

				}
			}

			if i == end {
				if t.searchUnvisitedParams && !n.end {
					n, start, i = n.findClosestUnvisitedNode(visitedNodes, qc, i)
					if n != nil {
						visitedNodes[n] = struct{}{}
						lim := min(n.paramCount, cap(paramValues))
						paramValues = paramValues[:lim]
						appendParameterValue(&paramValues, q[start:i])
					}
					if i == end {
						break
					}
				}
				break
			}

			i++
			start = i
			continue
		}

		i++
	}

	if n == nil || !n.end {
		if n != nil { // we need it on both places, on last segment (below) or on the first unnknown (above).
			if n = n.findClosestParentWildcardNode(); n != nil {
				params.Set(n.paramKeys[0], q[len(n.staticKey):])
				return n
			}
		}

		if t.hasRootWildcard {
			// that's the case for root wildcard, tests are passing
			// even without it but stick with it for reference.
			// Note ote that something like:
			// Routes: /other2/*myparam and /other2/static
			// Reqs: /other2/staticed will be handled
			// by the /other2/*myparam and not the root wildcard (see above), which is what we want.
			n = t.root.getChild(WildcardParamStart)
			params.Set(n.paramKeys[0], q[1:])
			return n
		}

		return nil
	}

	for i, paramValue := range paramValues {
		if len(n.paramKeys) > i {
			params.Set(n.paramKeys[i], paramValue)
		}
	}

	return n
}
