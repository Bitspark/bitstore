package bitstore

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
)

type measure struct{ nodes, octets, height uint64 }

func varsize(n uint64) uint64 {
	var s uint64 = 1
	for n >= 128 {
		n >>= 7
		s++
	}
	return s
}
func ownMeasure(own []byte, count int) measure {
	return measure{nodes: 1, octets: varsize(uint64(len(own))) + uint64(len(own)) + varsize(uint64(count))}
}
func (m *measure) add(key []byte, child measure, l DataTreeLimits) error {
	if child.nodes > l.Nodes || m.nodes > l.Nodes-child.nodes {
		return limit("unfolded_node_count")
	}
	extra := varsize(uint64(len(key))) + uint64(len(key))
	if extra > l.UnfoldedBytes || child.octets > l.UnfoldedBytes-extra || m.octets > l.UnfoldedBytes-extra-child.octets {
		return limit("unfolded_flat_octets")
	}
	m.nodes += child.nodes
	m.octets += extra + child.octets
	if child.height+1 > m.height {
		m.height = child.height + 1
	}
	if m.height > l.Depth {
		return limit("logical_depth")
	}
	return nil
}
func (m measure) check(l DataTreeLimits) error {
	if m.nodes > l.Nodes {
		return limit("unfolded_node_count")
	}
	if m.octets > l.UnfoldedBytes || uint64(len(header("dxf2"))) > l.UnfoldedBytes-m.octets {
		return limit("unfolded_flat_octets")
	}
	return nil
}

type encoded struct {
	root   Root
	chunks map[Name][]byte
	order  []Name
}

func encodeLinked(ctx context.Context, d *snapshot, l DataTreeLimits) (*encoded, error) {
	e := &encoded{chunks: map[Name][]byte{}}
	type entry struct {
		name Name
		m    measure
	}
	memo := map[*snapshot]entry{}
	var total uint64
	var visit func(*snapshot, uint64) (entry, error)
	visit = func(d *snapshot, depth uint64) (entry, error) {
		if err := ctx.Err(); err != nil {
			return entry{}, err
		}
		if err := checkNode(d, l, depth); err != nil {
			return entry{}, err
		}
		if old, ok := memo[d]; ok {
			if old.m.height > l.Depth-depth {
				return entry{}, limit("logical_depth")
			}
			return old, nil
		}
		m := ownMeasure(d.own, len(d.children))
		indices := map[Name]uint64{}
		var names []Name
		var body []byte
		body = field(body, d.own)
		body = number(body, uint64(len(d.children)))
		for _, c := range d.children {
			child, err := visit(c.Tree, depth+1)
			if err != nil {
				return entry{}, err
			}
			if err := m.add(c.Key, child.m, l); err != nil {
				return entry{}, err
			}
			index, ok := indices[child.name]
			if !ok {
				index = uint64(len(names))
				names = append(names, child.name)
				indices[child.name] = index
			}
			body = field(body, c.Key)
			body = number(body, index)
		}
		if err := m.check(l); err != nil {
			return entry{}, err
		}
		b := number(header("dxl2"), uint64(len(names)))
		for _, name := range names {
			h, _ := hex.DecodeString(name.Digest())
			b = append(b, h...)
		}
		b = append(b, body...)
		if uint64(len(b)) > l.ChunkBytes {
			return entry{}, limit("chunk_octets")
		}
		name := NameOf(b)
		if _, ok := e.chunks[name]; !ok {
			if uint64(len(e.chunks)) >= l.UniqueChunks {
				return entry{}, limit("unique_chunks")
			}
			if uint64(len(b)) > l.UniqueBytes || total > l.UniqueBytes-uint64(len(b)) {
				return entry{}, limit("unique_octets")
			}
			total += uint64(len(b))
			e.chunks[name] = b
			e.order = append(e.order, name)
		}
		result := entry{name, m}
		memo[d] = result
		return result, nil
	}
	r, err := visit(d, 0)
	if err != nil {
		return nil, err
	}
	e.root = rootOf(r.name)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return e, nil
}

// EncodeLinked emits canonical linked chunks, keyed by their raw storage names.
// Equal children share a chunk; their full logical expansion is still bounded.
func EncodeLinked(ctx context.Context, tree DataTree, limits DataTreeLimits) (Root, map[Name][]byte, error) {
	l := limits.defaults()
	d, err := materialize(ctx, tree, l)
	if err != nil {
		return Root{}, nil, err
	}
	e, err := encodeLinked(ctx, d, l)
	if err != nil {
		return Root{}, nil, err
	}
	return e.root, e.chunks, nil
}

// PutDataTree writes immutable chunks leaves first and acknowledges a root only after
// every put succeeds under its expected name. Failure can leave harmless orphan
// chunks. Publishing/naming a root or doing CAS is an application responsibility.
func PutDataTree(ctx context.Context, store Store, tree DataTree, limits DataTreeLimits) (Root, error) {
	if err := ctx.Err(); err != nil {
		return Root{}, err
	}
	l := limits.defaults()
	d, err := materialize(ctx, tree, l)
	if err != nil {
		return Root{}, err
	}
	e, err := encodeLinked(ctx, d, l)
	if err != nil {
		return Root{}, err
	}
	for _, name := range e.order {
		got, err := store.Put(ctx, e.chunks[name])
		if err != nil {
			return Root{}, err
		}
		if got != name {
			return Root{}, fmt.Errorf("%w: put acknowledged %s instead of %s", ErrIntegrity, got, name)
		}
	}
	return e.root, nil
}

// ChildRoot is an exact key and an immutable address. Listing children does not
// fetch their chunks, and does not promise those chunks are currently available.
type ChildRoot struct {
	Key  Bytes
	Root Root
}

// DataTreeView is a verified node with lazy, path-relative access to whole child values.
// Its immutable observations can be shared concurrently if the Store can.
type DataTreeView struct {
	root   Root
	store  Store
	limits DataTreeLimits
	chunk  *chunk
}

func (v *DataTreeView) Root() Root { return v.root }
func (v *DataTreeView) Own() Bytes { return bytes.Clone(v.chunk.own) }
func (v *DataTreeView) Children() []ChildRoot {
	cs := make([]ChildRoot, len(v.chunk.children))
	for i, c := range v.chunk.children {
		cs[i] = ChildRoot{bytes.Clone(c.key), rootOf(c.name)}
	}
	return cs
}

func fetchChunk(ctx context.Context, store Store, name Name, l DataTreeLimits, expectedID []byte) (*chunk, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	r, err := store.GetStream(ctx, name)
	if err != nil {
		return nil, 0, err
	}
	defer r.Close()
	// LimitReader takes int64. Keep the application policy representable and
	// never wrap a permissive uint64 limit into a negative, unlimited read.
	max := l.ChunkBytes
	if max > uint64(1<<63-2) {
		max = uint64(1<<63 - 2)
	}
	b, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, r: r}, int64(max)+1))
	if err != nil {
		return nil, 0, err
	}
	if uint64(len(b)) > max {
		return nil, 0, limit("chunk_octets")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if NameOf(b) != name {
		return nil, 0, fmt.Errorf("%w: chunk %s", ErrIntegrity, name)
	}
	c, err := decodeChunk(b, l, expectedID)
	return c, uint64(len(b)), err
}

// OpenDataTree fetches and verifies just the root. Availability of its closure is
// checked by LoadDataTree, or one selected path at a time through DataTreeView.At.
func OpenDataTree(ctx context.Context, store Store, root Root, limits DataTreeLimits) (*DataTreeView, error) {
	name, err := root.name()
	if err != nil {
		return nil, err
	}
	l := limits.defaults()
	c, _, err := fetchChunk(ctx, store, name, l, nil)
	if err != nil {
		return nil, err
	}
	return &DataTreeView{root, store, l, c}, nil
}

// At resolves relative to this view. false,nil means no such child; a missing
// chunk instead returns ErrNotFound, and corrupt bytes return ErrIntegrity.
// Empty path is this node and does not fetch. Each opened subtree resets the
// traversal budget, as a relative root in its own right.
func (v *DataTreeView) At(ctx context.Context, path ...Bytes) (*DataTreeView, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if uint64(len(path)) > v.limits.Depth {
		return nil, false, limit("logical_depth")
	}
	for _, key := range path {
		var name Name
		for _, c := range v.chunk.children {
			if bytes.Equal(key, c.key) {
				name = c.name
				break
			}
		}
		if name == "" {
			return nil, false, nil
		}
		c, _, err := fetchChunk(ctx, v.store, name, v.limits, v.chunk.id)
		if err != nil {
			return nil, false, err
		}
		v = &DataTreeView{rootOf(name), v.store, v.limits, c}
	}
	return v, true, nil
}

// LoadDataTree verifies the complete reachable closure and reconstructs its value.
// Shared chunks are fetched once, but logical expansion, including all copies
// of a shared child, is counted before the result is returned.
func LoadDataTree(ctx context.Context, store Store, root Root, limits DataTreeLimits) (DataTree, error) {
	name, err := root.name()
	if err != nil {
		return nil, err
	}
	l := limits.defaults()
	type entry struct {
		d *snapshot
		m measure
	}
	memo := map[Name]entry{}
	var total uint64
	var visit func(Name, uint64) (entry, error)
	visit = func(name Name, depth uint64) (entry, error) {
		if err := ctx.Err(); err != nil {
			return entry{}, err
		}
		if depth > l.Depth {
			return entry{}, limit("logical_depth")
		}
		if e, ok := memo[name]; ok {
			if e.m.height > l.Depth-depth {
				return entry{}, limit("logical_depth")
			}
			return e, nil
		}
		if uint64(len(memo)) >= l.UniqueChunks {
			return entry{}, limit("unique_chunks")
		}
		var expected []byte
		if depth > 0 {
			expected = []byte{0, 1}
		}
		c, size, err := fetchChunk(ctx, store, name, l, expected)
		if err != nil {
			return entry{}, err
		}
		if size > l.UniqueBytes || total > l.UniqueBytes-size {
			return entry{}, limit("unique_octets")
		}
		total += size
		// Reserve the unique chunk before descent, including long single-child paths.
		memo[name] = entry{}
		d := &snapshot{own: c.own}
		m := ownMeasure(c.own, len(c.children))
		for _, child := range c.children {
			e, err := visit(child.name, depth+1)
			if err != nil {
				return entry{}, err
			}
			if e.d == nil {
				return entry{}, invalid("cyclic_link")
			}
			if err := m.add(child.key, e.m, l); err != nil {
				return entry{}, err
			}
			d.children = append(d.children, snapshotChild{child.key, e.d})
		}
		if err := m.check(l); err != nil {
			return entry{}, err
		}
		result := entry{d, m}
		memo[name] = result
		return result, nil
	}
	e, err := visit(name, 0)
	if err != nil {
		return nil, err
	}
	return e.d, nil
}
