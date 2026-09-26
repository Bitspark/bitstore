package bitstore

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// DataProfile pins the identity-bytes specialization of the v0.4.0 candidate
// Deixis codec. A Bitstore release does not freeze the upstream codec.
const DataProfile = "deixis-codec-v2/identity-bytes@v0.4.0"

// Root keeps a data address and its interpretation together. It is not a raw
// blob Name. OpenDataTree validates both and verifies the addressed chunk.
type Root struct {
	Profile string `json:"profile"`
	Address string `json:"address"`
}

func rootOf(n Name) Root { return Root{DataProfile, "dxl2:" + n.Digest()} }

func (r Root) name() (Name, error) {
	if r.Profile != DataProfile {
		return "", fault("unsupported", "unsupported_profile")
	}
	if len(r.Address) != 69 || r.Address[:5] != "dxl2:" {
		return "", fmt.Errorf("%w: expected dxl2:<64 lowercase hex digits>", ErrInvalidName)
	}
	return ParseName("sha256:" + r.Address[5:])
}

// CodecError separates invalid, unsupported and resource-refused observations.
// Missing chunks and integrity failures instead wrap ErrNotFound and ErrIntegrity.
type CodecError struct {
	Class     string
	Code      string
	Dimension string
}

func (e *CodecError) Error() string {
	return "bitstore: " + e.Class + ": " + e.Code + " " + e.Dimension
}
func fault(class, code string) error { return &CodecError{Class: class, Code: code} }
func invalid(code string) error      { return fault("invalid", code) }
func limit(dimension string) error {
	return &CodecError{Class: "resource-refused", Code: "limit_exceeded", Dimension: dimension}
}

// DataTreeLimits is an application resource policy, not a change to codec validity.
// Zero fields select the published portable floors. Smaller explicit limits
// describe a restricted application profile, not full portable conformance.
type DataTreeLimits struct {
	KeyBytes, PayloadBytes, Children, ChunkBytes, FlatBytes uint64
	Depth, Nodes, UniqueChunks, UniqueBytes, UnfoldedBytes  uint64
}

func (l DataTreeLimits) defaults() DataTreeLimits {
	fields := []*uint64{&l.KeyBytes, &l.PayloadBytes, &l.Children, &l.ChunkBytes, &l.FlatBytes, &l.Depth, &l.Nodes, &l.UniqueChunks, &l.UniqueBytes, &l.UnfoldedBytes}
	values := []uint64{4096, 16 << 20, 65536, 32 << 20, 64 << 20, 256, 16777216, 1000000, 1 << 30, 1 << 30}
	for i, p := range fields {
		if *p == 0 {
			*p = values[i]
		}
	}
	return l
}

type link struct {
	key  Bytes
	name Name
}
type chunk struct {
	own      Bytes
	children []link
	id       Bytes
}
type parser struct {
	b []byte
	i int
	l DataTreeLimits
}

func (p *parser) take(n uint64) ([]byte, error) {
	if n > uint64(len(p.b)-p.i) {
		return nil, invalid("unexpected_eof")
	}
	b := p.b[p.i : p.i+int(n)]
	p.i += int(n)
	return b, nil
}

func (p *parser) num() (uint64, error) {
	var n uint64
	for i := 0; i < 10; i++ {
		if p.i == len(p.b) {
			if i == 0 {
				return 0, invalid("unexpected_eof")
			}
			return 0, invalid("malformed_uvarint")
		}
		b := p.b[p.i]
		p.i++
		if i == 9 && b&128 != 0 {
			return 0, invalid("malformed_uvarint")
		}
		if i == 9 && b > 1 {
			return 0, invalid("uvarint_overflow")
		}
		n |= uint64(b&127) << (7 * i)
		if b&128 == 0 {
			if i > 0 && b == 0 {
				return 0, invalid("non_shortest_uvarint")
			}
			return n, nil
		}
	}
	return 0, invalid("malformed_uvarint")
}

func (p *parser) bounded(max uint64, dimension string) (uint64, error) {
	n, err := p.num()
	if err != nil {
		return 0, err
	}
	if n > max {
		return 0, limit(dimension)
	}
	return n, nil
}

func (p *parser) field(max uint64, dimension string) ([]byte, error) {
	n, err := p.bounded(max, dimension)
	if err != nil {
		return nil, err
	}
	return p.take(n)
}

func (p *parser) header(magic string) ([]byte, error) {
	b, err := p.take(4)
	if err != nil {
		return nil, err
	}
	if string(b) != magic {
		return nil, invalid("unknown_magic")
	}
	n, err := p.num()
	if err != nil {
		return nil, err
	}
	if n < 2 || n > 32 {
		return nil, invalid("malformed_slot_codec_id")
	}
	id, err := p.take(n)
	if err != nil {
		return nil, err
	}
	if err := checkID(id); err != nil {
		return nil, err
	}
	return id, nil
}

// Validate the bounded identifier's framing without judging support yet.
func checkID(id []byte) error {
	p := parser{b: id}
	for {
		if p.i == len(id) {
			return invalid("malformed_slot_codec_id")
		}
		kind := id[p.i]
		p.i++
		if kind == 2 {
			continue
		}
		if kind > 2 {
			return nil
		} // reserved form, structurally opaque
		if kind == 1 {
			if _, err := p.take(16); err != nil {
				return invalid("malformed_slot_codec_id")
			}
		}
		if p.i == len(id) {
			return invalid("malformed_slot_codec_id")
		}
		n, err := p.num()
		if err != nil {
			return err
		}
		if (kind == 0 && n == 0) || p.i != len(id) {
			return invalid("malformed_slot_codec_id")
		}
		return nil
	}
}

func supported(id []byte) error {
	if !bytes.Equal(id, []byte{0, 1}) {
		return fault("unsupported", "unsupported_slot_codec")
	}
	return nil
}

func (p *parser) key(previous []byte, first bool) ([]byte, error) {
	k, err := p.field(p.l.KeyBytes, "key_length")
	if err != nil {
		return nil, err
	}
	if !first {
		switch bytes.Compare(k, previous) {
		case 0:
			return nil, invalid("duplicate_key")
		case -1:
			return nil, invalid("unsorted_keys")
		}
	}
	return k, nil
}

func decodeChunk(b []byte, l DataTreeLimits, expectedID []byte) (*chunk, error) {
	if uint64(len(b)) > l.ChunkBytes {
		return nil, limit("chunk_octets")
	}
	p := parser{b: b, l: l}
	id, err := p.header("dxl2")
	if err != nil {
		return nil, err
	}
	n, err := p.bounded(l.Children, "links_per_chunk")
	if err != nil {
		return nil, err
	}
	names := make([]Name, 0)
	seen := map[Name]bool{}
	for i := uint64(0); i < n; i++ {
		h, err := p.take(32)
		if err != nil {
			return nil, err
		}
		name := Name("sha256:" + hex.EncodeToString(h))
		if seen[name] {
			return nil, invalid("duplicate_link_hash")
		}
		seen[name] = true
		names = append(names, name)
	}
	own, err := p.field(l.PayloadBytes, "payload_length")
	if err != nil {
		return nil, err
	}
	count, err := p.bounded(l.Children, "entries_per_node")
	if err != nil {
		return nil, err
	}
	c := &chunk{own: bytes.Clone(own), id: bytes.Clone(id)}
	used := map[uint64]bool{}
	next := uint64(0)
	var prev []byte
	for i := uint64(0); i < count; i++ {
		key, err := p.key(prev, i == 0)
		if err != nil {
			return nil, err
		}
		prev = key
		index, err := p.num()
		if err != nil {
			return nil, err
		}
		if index >= n {
			return nil, invalid("bad_link_index")
		}
		if !used[index] {
			if index != next {
				return nil, invalid("links_out_of_order")
			}
			used[index] = true
			next++
		}
		c.children = append(c.children, link{bytes.Clone(key), names[index]})
	}
	if next != n {
		return nil, invalid("unused_link")
	}
	if p.i != len(b) {
		return nil, invalid("trailing_bytes")
	}
	if expectedID != nil && !bytes.Equal(id, expectedID) {
		return nil, invalid("slot_codec_mismatch")
	}
	if err := supported(id); err != nil {
		return nil, err
	}
	return c, nil
}

func number(b []byte, n uint64) []byte { return binary.AppendUvarint(b, n) }
func field(b, v []byte) []byte         { b = number(b, uint64(len(v))); return append(b, v...) }
func header(magic string) []byte       { return append([]byte(magic), 2, 0, 1) }

func checkNode(d *snapshot, l DataTreeLimits, depth uint64) error {
	if d == nil {
		return ErrInvalidDataTree
	}
	if depth > l.Depth {
		return limit("logical_depth")
	}
	if uint64(len(d.own)) > l.PayloadBytes {
		return limit("payload_length")
	}
	if uint64(len(d.children)) > l.Children {
		return limit("entries_per_node")
	}
	for _, c := range d.children {
		if uint64(len(c.Key)) > l.KeyBytes {
			return limit("key_length")
		}
	}
	return nil
}

// EncodeFlat emits the canonical flat form. A flat checksum is not a data root.
func EncodeFlat(ctx context.Context, tree DataTree, limits DataTreeLimits) ([]byte, error) {
	l := limits.defaults()
	materialLimits := l
	dimension := "unfolded_flat_octets"
	if materialLimits.UnfoldedBytes > l.FlatBytes {
		materialLimits.UnfoldedBytes = l.FlatBytes
		dimension = "flat_artifact_octets"
	}
	d, err := materialize(ctx, tree, materialLimits, dimension)
	if err != nil {
		return nil, err
	}
	out := header("dxf2")
	var nodes uint64
	var visit func(*snapshot, uint64) error
	visit = func(d *snapshot, depth uint64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := checkNode(d, l, depth); err != nil {
			return err
		}
		nodes++
		if nodes > l.Nodes {
			return limit("unfolded_node_count")
		}
		out = field(out, d.own)
		out = number(out, uint64(len(d.children)))
		if uint64(len(out)) > l.FlatBytes {
			return limit("flat_artifact_octets")
		}
		if uint64(len(out)) > l.UnfoldedBytes {
			return limit("unfolded_flat_octets")
		}
		for _, c := range d.children {
			out = field(out, c.Key)
			if err := visit(c.Tree, depth+1); err != nil {
				return err
			}
		}
		if uint64(len(out)) > l.FlatBytes {
			return limit("flat_artifact_octets")
		}
		if uint64(len(out)) > l.UnfoldedBytes {
			return limit("unfolded_flat_octets")
		}
		return nil
	}
	if err := visit(d, 0); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// DecodeFlat accepts only canonical identity-bytes artifacts, with bounded work.
func DecodeFlat(b []byte, limits DataTreeLimits) (DataTree, error) {
	l := limits.defaults()
	if uint64(len(b)) > l.FlatBytes {
		return nil, limit("flat_artifact_octets")
	}
	p := parser{b: b, l: l}
	id, err := p.header("dxf2")
	if err != nil {
		return nil, err
	}
	var nodes uint64
	var visit func(uint64) (*snapshot, error)
	visit = func(depth uint64) (*snapshot, error) {
		if depth > l.Depth {
			return nil, limit("logical_depth")
		}
		nodes++
		if nodes > l.Nodes {
			return nil, limit("unfolded_node_count")
		}
		own, err := p.field(l.PayloadBytes, "payload_length")
		if err != nil {
			return nil, err
		}
		n, err := p.bounded(l.Children, "entries_per_node")
		if err != nil {
			return nil, err
		}
		d := &snapshot{own: bytes.Clone(own)}
		var prev []byte
		for i := uint64(0); i < n; i++ {
			key, err := p.key(prev, i == 0)
			if err != nil {
				return nil, err
			}
			prev = key
			child, err := visit(depth + 1)
			if err != nil {
				return nil, err
			}
			d.children = append(d.children, snapshotChild{bytes.Clone(key), child})
		}
		return d, nil
	}
	d, err := visit(0)
	if err != nil {
		return nil, err
	}
	if p.i != len(b) {
		return nil, invalid("trailing_bytes")
	}
	if err := supported(id); err != nil {
		return nil, err
	}
	return d, nil
}
