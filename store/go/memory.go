package bitstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
)

// Memory is an in-memory Store. It keeps nothing across processes and makes
// no persistence promise.
type Memory struct {
	maxBytes int64
	mu       sync.RWMutex
	blobs    map[Name][]byte
}

// NewMemory returns an empty in-memory store accepting blobs of at most
// maxBytes bytes, or of any size when maxBytes is zero.
func NewMemory(maxBytes int64) *Memory {
	return &Memory{maxBytes: maxBytes, blobs: map[Name][]byte{}}
}

func (m *Memory) Get(ctx context.Context, name Name) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.blobs[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return bytes.Clone(b), nil
}

func (m *Memory) GetMany(ctx context.Context, names []Name) (map[Name][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	found := map[Name][]byte{}
	for _, n := range names {
		if b, ok := m.blobs[n]; ok {
			found[n] = bytes.Clone(b)
		}
	}
	return found, nil
}

func (m *Memory) Has(ctx context.Context, names []Name) ([]Name, error) {
	sizes, err := m.Size(ctx, names)
	if err != nil {
		return nil, err
	}
	return Present(names, sizes), nil
}

func (m *Memory) Size(ctx context.Context, names []Name) (map[Name]int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	sizes := map[Name]int64{}
	for _, n := range names {
		if b, ok := m.blobs[n]; ok {
			sizes[n] = int64(len(b))
		}
	}
	return sizes, nil
}

func (m *Memory) GetRange(ctx context.Context, name Name, offset, length int64) ([]byte, error) {
	if offset < 0 {
		return nil, fmt.Errorf("bitstore: negative offset %d", offset)
	}
	b, err := m.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	rg := Overlap(int64(len(b)), offset, length)
	return b[rg.Start:rg.End], nil
}

func (m *Memory) GetStream(ctx context.Context, name Name) (io.ReadCloser, error) {
	b, err := m.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *Memory) Put(ctx context.Context, b []byte) (Name, error) {
	name, _, err := m.PutStream(ctx, bytes.NewReader(b))
	return name, err
}

func (m *Memory) PutMany(ctx context.Context, items [][]byte) ([]PutResult, error) {
	results := make([]PutResult, len(items))
	for i, b := range items {
		name, err := m.Put(ctx, b)
		if err != nil {
			results[i] = PutResult{Err: err}
			continue
		}
		results[i] = PutResult{Name: name, Size: int64(len(b))}
	}
	return results, nil
}

func (m *Memory) PutStream(ctx context.Context, r io.Reader) (Name, int64, error) {
	h := sha256.New()
	var buf bytes.Buffer
	n, err := Copy(ctx, io.MultiWriter(h, &buf), r, m.maxBytes)
	if err != nil {
		return "", 0, err
	}
	name := Name("sha256:" + hex.EncodeToString(h.Sum(nil)))
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.blobs[name]; !ok {
		m.blobs[name] = buf.Bytes()
	}
	return name, n, nil
}

// Overlap is the slice bounds of [offset, offset+length) clamped to a blob of
// the size; a negative length reaches the end. An offset at or past the end
// overlaps nothing.
func Overlap(size, offset, length int64) Range {
	if offset >= size {
		return Range{size, size}
	}
	end := size
	if length >= 0 && length < size-offset {
		end = offset + length
	}
	return Range{offset, end}
}

// Range is a pair of slice bounds.
type Range struct{ Start, End int64 }

// Copy copies r to w, failing with ErrTooLarge as soon as more than maxBytes
// arrive (unless maxBytes is zero) and with the context's error once it is
// done. A caller commits only when Copy returns no error.
func Copy(ctx context.Context, w io.Writer, r io.Reader, maxBytes int64) (int64, error) {
	src := io.Reader(&contextReader{ctx: ctx, r: r})
	if maxBytes > 0 {
		src = io.LimitReader(src, maxBytes+1)
	}
	n, err := io.Copy(w, src)
	if err != nil {
		return n, err
	}
	if maxBytes > 0 && n > maxBytes {
		return n, fmt.Errorf("%w: more than %d bytes", ErrTooLarge, maxBytes)
	}
	if err := ctx.Err(); err != nil {
		return n, err
	}
	return n, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// Present lists, once each and in request order, the names that have a size:
// Has in terms of Size, for implementations of Store.
func Present(names []Name, sizes map[Name]int64) []Name {
	var here []Name
	seen := map[Name]bool{}
	for _, n := range names {
		if _, ok := sizes[n]; ok && !seen[n] {
			here = append(here, n)
			seen[n] = true
		}
	}
	return here
}
