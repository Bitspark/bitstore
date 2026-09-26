package bitstore

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
)

// Bytes is the carrier of Data's own values and exact (not necessarily UTF-8) keys.
type Bytes = []byte

// Data is an immutable Deixis[Bytes] value. Its zero value is the empty leaf.
// Sharing a child is unobservable; a child is always a whole Data value.
type Data struct {
	own      Bytes
	children []Child
}

// Child supplies a byte key and a whole, non-nil child to NewData.
type Child struct {
	Key  Bytes
	Data *Data
}

var ErrInvalidData = errors.New("bitstore: invalid data")

// NewData copies the supplied bytes and keys and rejects duplicate keys.
// Empty own bytes, an empty child key, and no children are all valid.
func NewData(own Bytes, children ...Child) (*Data, error) {
	d := &Data{own: bytes.Clone(own), children: make([]Child, len(children))}
	for i, c := range children {
		if c.Data == nil {
			return nil, fmt.Errorf("%w: nil child", ErrInvalidData)
		}
		d.children[i] = Child{bytes.Clone(c.Key), c.Data}
	}
	sort.Slice(d.children, func(i, j int) bool { return bytes.Compare(d.children[i].Key, d.children[j].Key) < 0 })
	for i := 1; i < len(d.children); i++ {
		if bytes.Equal(d.children[i-1].Key, d.children[i].Key) {
			return nil, fmt.Errorf("%w: duplicate key", ErrInvalidData)
		}
	}
	return d, nil
}

// Own returns a copy of the mandatory own value, including when it is empty.
func (d *Data) Own() Bytes { return bytes.Clone(d.own) }

// Children returns byte-sorted children with copied keys.
func (d *Data) Children() []Child {
	cs := make([]Child, len(d.children))
	for i, c := range d.children {
		cs[i] = Child{bytes.Clone(c.Key), c.Data}
	}
	return cs
}

// At resolves a path of exact byte keys. An empty path is this value; a path
// containing one empty key selects the empty-key child. Missing is not an empty leaf.
func (d *Data) At(path ...Bytes) (*Data, bool) {
	for _, key := range path {
		i := sort.Search(len(d.children), func(i int) bool { return bytes.Compare(d.children[i].Key, key) >= 0 })
		if i == len(d.children) || !bytes.Equal(d.children[i].Key, key) {
			return nil, false
		}
		d = d.children[i].Data
	}
	return d, true
}
