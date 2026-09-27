package bitstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

// Bytes is a finite byte sequence; Key is an exact byte string, including empty.
type Bytes = []byte
type Key = Bytes
type TreePath = []Key

// Data is addressless access to fixed content. Successful reads return the same
// bytes in independently owned buffers. Errors and cancellation are not absence.
type Data interface {
	Read(context.Context) (Bytes, error)
}

// DeixisNode is the public structural presentation of the Deixis contract.
// Every lawful node has an own value, a complete finite map of exact byte keys
// to whole children, and consistent selection and decomposition. Graphs are
// finite and acyclic; sharing does not change their logical tree meaning.
// Deixis owns these laws; this declaration imports no private implementation.
type DeixisNode[T any] interface {
	Own() T
	Children() []Child[T]
	At(TreePath) (DeixisNode[T], bool)
	Decompose() (T, []Child[T])
}
type Child[T any] struct {
	Key  Key
	Tree DeixisNode[T]
}

// DataTree is exactly DeixisNode[Data], not a separate nominal tree type.
type DataTree = DeixisNode[Data]

var ErrInvalidDataTree = errors.New("bitstore: invalid data tree")
var ErrInvalidTree = errors.New("bitstore: invalid tree")
var ErrPathNotFound = errors.New("bitstore: path not found")

type valueData struct{ bytes Bytes }

func (d *valueData) Read(ctx context.Context) (Bytes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return bytes.Clone(d.bytes), nil
}

// BytesData copies bytes into an immutable addressless reader.
func BytesData(b Bytes) Data { return &valueData{bytes.Clone(b)} }

type treeNode[T any] struct {
	own      T
	children []Child[T]
}

func nilValue(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

// Compose constructs a node without invoking its payload. It retains whole
// lawful child nodes and copies keys. It refuses nil children, duplicate keys
// and structural cycles. Children implemented outside this package are checked
// through their complete Children graph, sharing allowed; Own, At and payloads
// are never called. A cycle is found by node identity, so a child whose values
// are not comparable must itself be finite.
func Compose[T any](own T, children ...Child[T]) (DeixisNode[T], error) {
	n := &treeNode[T]{own: own}
	var err error
	if n.children, err = sortedChildren(children); err != nil {
		return nil, err
	}
	if err = validateForeign(n.children); err != nil {
		return nil, err
	}
	return n, nil
}

// sortedChildren copies children and keys in key order, refusing nil children
// and duplicate keys.
func sortedChildren[T any](children []Child[T]) ([]Child[T], error) {
	result := make([]Child[T], len(children))
	for i, c := range children {
		if nilValue(c.Tree) {
			return nil, fmt.Errorf("%w: nil child", ErrInvalidTree)
		}
		result[i] = Child[T]{bytes.Clone(c.Key), c.Tree}
	}
	sort.Slice(result, func(i, j int) bool { return bytes.Compare(result[i].Key, result[j].Key) < 0 })
	for i := 1; i < len(result); i++ {
		if bytes.Equal(result[i-1].Key, result[i].Key) {
			return nil, fmt.Errorf("%w: duplicate key", ErrInvalidTree)
		}
	}
	return result, nil
}

// validateForeign walks children implemented outside this package with an
// explicit stack. Nodes this package constructed were validated when they were.
func validateForeign[T any](children []Child[T]) error {
	type visit struct {
		tree DeixisNode[T]
		exit bool
	}
	stack := make([]visit, 0, len(children))
	for _, c := range children {
		stack = append(stack, visit{tree: c.Tree})
	}
	active := map[DeixisNode[T]]bool{}
	done := map[DeixisNode[T]]bool{}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if trusted[T](v.tree) {
			continue
		}
		if v.exit {
			delete(active, v.tree)
			done[v.tree] = true
			continue
		}
		// Only comparable, reflexive values (unlike one holding NaN) have an identity a map can track.
		identified := reflect.ValueOf(v.tree).Comparable() && v.tree == v.tree
		if identified {
			if active[v.tree] {
				return fmt.Errorf("%w: cyclic tree", ErrInvalidTree)
			}
			if done[v.tree] {
				continue
			}
			active[v.tree] = true
			stack = append(stack, visit{tree: v.tree, exit: true})
		}
		parts, err := sortedChildren(v.tree.Children())
		if err != nil {
			return err
		}
		for _, c := range parts {
			stack = append(stack, visit{tree: c.Tree})
		}
	}
	return nil
}

func trusted[T any](tree DeixisNode[T]) bool {
	switch any(tree).(type) {
	case *treeNode[T], *snapshot:
		return true
	}
	return false
}
func (n *treeNode[T]) Own() T { return n.own }
func (n *treeNode[T]) Children() []Child[T] {
	result := make([]Child[T], len(n.children))
	for i, c := range n.children {
		result[i] = Child[T]{bytes.Clone(c.Key), c.Tree}
	}
	return result
}
func (n *treeNode[T]) At(path TreePath) (DeixisNode[T], bool) { return selectNode[T](n, path) }
func (n *treeNode[T]) Decompose() (T, []Child[T])             { return n.Own(), n.Children() }
func selectNode[T any](node DeixisNode[T], path TreePath) (DeixisNode[T], bool) {
	for _, key := range path {
		var next DeixisNode[T]
		for _, child := range node.Children() {
			if bytes.Equal(key, child.Key) {
				next = child.Tree
				break
			}
		}
		if nilValue(next) {
			return nil, false
		}
		node = next
	}
	return node, true
}

// Select follows exact child keys; an empty path is identity.
func Select[T any](node DeixisNode[T], path TreePath) (DeixisNode[T], bool) {
	if nilValue(node) {
		return nil, false
	}
	return node.At(path)
}

// NewDataTree composes readers without reading them.
func NewDataTree(own Data, children ...Child[Data]) (DataTree, error) {
	if nilValue(own) {
		return nil, fmt.Errorf("%w: nil own reader", ErrInvalidDataTree)
	}
	d, err := Compose[Data](own, children...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDataTree, err)
	}
	return d, nil
}

// Read is select(tree,path).Own().Read(ctx). Structural absence has its own error.
func Read(ctx context.Context, tree DataTree, path TreePath) (Bytes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	selected, ok := Select[Data](tree, path)
	if !ok {
		return nil, ErrPathNotFound
	}
	if nilValue(selected.Own()) {
		return nil, ErrInvalidDataTree
	}
	b, err := selected.Own().Read(ctx)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return bytes.Clone(b), nil
}

// snapshot is the internal, fully materialized carrier used by the unchanged
// identity-bytes codec. Reader capabilities are never serialized.
type snapshot struct {
	own      Bytes
	children []snapshotChild
}
type snapshotChild struct {
	Key  Bytes
	Tree *snapshot
}

func (s *snapshot) Own() Data { return s }
func (s *snapshot) Read(ctx context.Context) (Bytes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return bytes.Clone(s.own), nil
}
func (s *snapshot) Children() []Child[Data] {
	cs := make([]Child[Data], len(s.children))
	for i, c := range s.children {
		cs[i] = Child[Data]{bytes.Clone(c.Key), c.Tree}
	}
	return cs
}
func (s *snapshot) At(path TreePath) (DataTree, bool) { return selectNode[Data](s, path) }
func (s *snapshot) Decompose() (Data, []Child[Data])  { return s.Own(), s.Children() }

func materialize(ctx context.Context, tree DataTree, l DataTreeLimits, dimension string) (*snapshot, error) {
	memo := map[DataTree]*snapshot{}
	active := map[DataTree]bool{}
	var count uint64
	total := uint64(len(header("dxf2")))
	charge := func(n uint64) error {
		if n > l.UnfoldedBytes || total > l.UnfoldedBytes-n {
			return limit(dimension)
		}
		total += n
		return nil
	}
	var visit func(DataTree, uint64) (*snapshot, error)
	visit = func(node DataTree, depth uint64) (*snapshot, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if nilValue(node) {
			return nil, ErrInvalidDataTree
		}
		if depth > l.Depth {
			return nil, limit("logical_depth")
		}
		comparable := reflect.ValueOf(node).Comparable()
		if comparable {
			if active[node] {
				return nil, fmt.Errorf("%w: cyclic tree", ErrInvalidDataTree)
			}
			if result, ok := memo[node]; ok {
				return result, nil
			}
			active[node] = true
			defer delete(active, node)
		}
		count++
		if count > l.Nodes {
			return nil, limit("unfolded_node_count")
		}
		own := node.Own()
		if nilValue(own) {
			return nil, ErrInvalidDataTree
		}
		cs := node.Children()
		if uint64(len(cs)) > l.Children {
			return nil, limit("entries_per_node")
		}
		if err := charge(varsize(uint64(len(cs)))); err != nil {
			return nil, err
		}
		cs = append([]Child[Data](nil), cs...)
		for i, c := range cs {
			if uint64(len(c.Key)) > l.KeyBytes {
				return nil, limit("key_length")
			}
			if err := charge(varsize(uint64(len(c.Key))) + uint64(len(c.Key))); err != nil {
				return nil, err
			}
			cs[i].Key = bytes.Clone(c.Key)
		}
		sort.Slice(cs, func(i, j int) bool { return bytes.Compare(cs[i].Key, cs[j].Key) < 0 })
		for i := 1; i < len(cs); i++ {
			if bytes.Equal(cs[i-1].Key, cs[i].Key) {
				return nil, fmt.Errorf("%w: duplicate key", ErrInvalidDataTree)
			}
		}
		b, err := own.Read(ctx)
		if err != nil {
			return nil, err
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if uint64(len(b)) > l.PayloadBytes {
			return nil, limit("payload_length")
		}
		if err := charge(varsize(uint64(len(b))) + uint64(len(b))); err != nil {
			return nil, err
		}
		result := &snapshot{own: bytes.Clone(b)}
		for _, c := range cs {
			child, err := visit(c.Tree, depth+1)
			if err != nil {
				return nil, err
			}
			result.children = append(result.children, snapshotChild{c.Key, child})
		}
		if comparable {
			memo[node] = result
		}
		return result, nil
	}
	return visit(tree, 0)
}
