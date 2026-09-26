package bitstore_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"

	bs "github.com/Bitspark/bitstore/store/go"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func node(t *testing.T, own []byte, cs ...bs.Child) *bs.Data {
	t.Helper()
	d, err := bs.NewData(own, cs...)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var ce *bs.CodecError
	if !errors.As(err, &ce) || ce.Code != want {
		t.Fatalf("got %v; want %s", err, want)
	}
}

func TestDataFixtures(t *testing.T) {
	b, err := os.ReadFile("../../vectors/data.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Vectors []struct {
			Label, Flat string
			Root        bs.Root
			Chunks      map[bs.Name]string
		}
		InvalidFlat []struct{ Label, Hex, Code string }
	}
	if err = json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	for _, v := range file.Vectors {
		t.Run(v.Label, func(t *testing.T) {
			flat := unhex(t, v.Flat)
			d, err := bs.DecodeFlat(flat, bs.DataLimits{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := bs.EncodeFlat(d, bs.DataLimits{})
			if err != nil || !bytes.Equal(got, flat) {
				t.Fatalf("flat %x, %v", got, err)
			}
			root, chunks, err := bs.EncodeLinked(d, bs.DataLimits{})
			if err != nil || root != v.Root {
				t.Fatalf("root %v, %v", root, err)
			}
			want := map[bs.Name][]byte{}
			for n, s := range v.Chunks {
				want[n] = unhex(t, s)
			}
			if !reflect.DeepEqual(chunks, want) {
				t.Fatal("linked bytes differ from grammar fixture")
			}
			store := bs.NewMemory(0)
			ctx := context.Background()
			for n, b := range want {
				got, err := store.Put(ctx, b)
				if err != nil || got != n {
					t.Fatal("fixture hash", err)
				}
			}
			d, err = bs.LoadData(ctx, store, v.Root, bs.DataLimits{})
			if err != nil {
				t.Fatal(err)
			}
			got, err = bs.EncodeFlat(d, bs.DataLimits{})
			if err != nil || !bytes.Equal(got, flat) {
				t.Fatal("reconstruction", err)
			}
		})
	}
	for _, v := range file.InvalidFlat {
		t.Run(v.Label, func(t *testing.T) { _, err := bs.DecodeFlat(unhex(t, v.Hex), bs.DataLimits{}); code(t, err, v.Code) })
	}
}

func TestExactDataAndReconstruction(t *testing.T) {
	own, key := []byte{7}, []byte{255}
	leaf := node(t, nil)
	d := node(t, own, bs.Child{Key: key, Data: leaf}, bs.Child{Key: nil, Data: leaf})
	own[0] = 9
	key[0] = 1
	if !bytes.Equal(d.Own(), []byte{7}) {
		t.Fatal("own aliases input")
	}
	out := d.Own()
	out[0] = 8
	cs := d.Children()
	cs[1].Key[0] = 1
	if got, ok := d.At([]byte{255}); !ok || got != leaf {
		t.Fatal("key aliases input/output")
	}
	if got, ok := d.At(); !ok || got != d {
		t.Fatal("empty path")
	}
	if got, ok := d.At(nil); !ok || got != leaf {
		t.Fatal("empty key")
	}
	if _, ok := d.At([]byte{1}); ok {
		t.Fatal("missing invented")
	}
	rebuilt := node(t, d.Own(), d.Children()...)
	a, _ := bs.EncodeFlat(d, bs.DataLimits{})
	b, _ := bs.EncodeFlat(rebuilt, bs.DataLimits{})
	if !bytes.Equal(a, b) {
		t.Fatal("reconstruction law")
	}
	if _, err := bs.NewData(nil, bs.Child{Data: leaf}, bs.Child{Data: leaf}); !errors.Is(err, bs.ErrInvalidData) {
		t.Fatal("duplicate", err)
	}
	if _, err := bs.NewData(nil, bs.Child{}); !errors.Is(err, bs.ErrInvalidData) {
		t.Fatal("nil", err)
	}
	var zero bs.Data
	if len(zero.Own()) != 0 || len(zero.Children()) != 0 {
		t.Fatal("zero leaf")
	}
}

type observedStore struct {
	bs.Store
	reads   []bs.Name
	missing bs.Name
	corrupt bool
	lie     bool
}

func (s *observedStore) GetStream(ctx context.Context, n bs.Name) (io.ReadCloser, error) {
	s.reads = append(s.reads, n)
	if n == s.missing {
		return nil, bs.ErrNotFound
	}
	if s.corrupt {
		return io.NopCloser(bytes.NewReader([]byte("bad"))), nil
	}
	return s.Store.GetStream(ctx, n)
}
func (s *observedStore) Put(ctx context.Context, b []byte) (bs.Name, error) {
	if s.lie {
		return bs.NameOf([]byte("lie")), nil
	}
	return s.Store.Put(ctx, b)
}

func TestLazyPathsIntegrityAbsenceAndReopen(t *testing.T) {
	ctx := context.Background()
	store := &observedStore{Store: bs.NewMemory(0)}
	leaf := node(t, []byte("leaf"))
	d := node(t, []byte("parent"), bs.Child{Key: nil, Data: node(t, nil, bs.Child{Key: []byte{255}, Data: leaf})}, bs.Child{Key: []byte("sibling"), Data: leaf})
	root, err := bs.PutData(ctx, store, d, bs.DataLimits{})
	if err != nil {
		t.Fatal(err)
	}
	v, err := bs.OpenData(ctx, store, root, bs.DataLimits{})
	if err != nil || len(store.reads) != 1 {
		t.Fatal("root not lazy", err)
	}
	if got, ok, err := v.At(ctx); err != nil || !ok || got != v || len(store.reads) != 1 {
		t.Fatal("empty path", err)
	}
	if _, ok, err := v.At(ctx, []byte("absent")); err != nil || ok || len(store.reads) != 1 {
		t.Fatal("missing path", err)
	}
	child, ok, err := v.At(ctx, nil, []byte{255})
	if err != nil || !ok || string(child.Own()) != "leaf" || len(store.reads) != 3 {
		t.Fatal("multilevel", err)
	}
	store.missing = store.reads[2]
	if _, _, err := v.At(ctx, []byte("sibling")); !errors.Is(err, bs.ErrNotFound) {
		t.Fatal("missing chunk became path absence", err)
	}
	store.missing = ""
	store.corrupt = true
	if _, err := bs.OpenData(ctx, store, root, bs.DataLimits{}); !errors.Is(err, bs.ErrIntegrity) {
		t.Fatal("corrupt", err)
	}
	store.corrupt = false
	store.lie = true
	if _, err := bs.PutData(ctx, store, d, bs.DataLimits{}); !errors.Is(err, bs.ErrIntegrity) {
		t.Fatal("lying put", err)
	}
	root.Profile = "future"
	_, err = bs.OpenData(ctx, store, root, bs.DataLimits{})
	code(t, err, "unsupported_profile")
}

func TestLinkedFramingAndPrecedence(t *testing.T) {
	ctx := context.Background()
	s := bs.NewMemory(0)
	h := bs.NameOf([]byte("missing")).Digest()
	cases := []struct{ hex, code string }{
		{"64786c3202000101" + h + "0001016101", "bad_link_index"},
		{"64786c3202000101" + h + "0000", "unused_link"},
		{"64786c3202000102" + h + h + "0000", "duplicate_link_hash"},
		{"64786c3202000102" + h + bs.NameOf(nil).Digest() + "00010001", "links_out_of_order"},
		{"64786c320200020001", "unexpected_eof"},
		{"64786c32020002000000", "unsupported_slot_codec"},
	}
	for _, c := range cases {
		b := unhex(t, c.hex)
		n, _ := s.Put(ctx, b)
		_, err := bs.OpenData(ctx, s, bs.Root{Profile: bs.DataProfile, Address: "dxl2:" + n.Digest()}, bs.DataLimits{})
		code(t, err, c.code)
	}
	// Parent is supported; child has a different, unsupported id and a bad body.
	// Child framing wins over slot mismatch, then a well-framed child mismatches.
	for _, tc := range []struct{ body, code string }{{"01", "unexpected_eof"}, {"0000", "slot_codec_mismatch"}} {
		child := unhex(t, "64786c3202000200"+tc.body)
		n, _ := s.Put(ctx, child)
		parent := unhex(t, "64786c3202000101"+n.Digest()+"00010000")
		pn, _ := s.Put(ctx, parent)
		v, err := bs.OpenData(ctx, s, bs.Root{Profile: bs.DataProfile, Address: "dxl2:" + pn.Digest()}, bs.DataLimits{})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = v.At(ctx, nil)
		code(t, err, tc.code)
	}
}

func TestBoundsCancellationAndSharing(t *testing.T) {
	ctx := context.Background()
	s := &observedStore{Store: bs.NewMemory(0)}
	d := node(t, []byte("own"))
	for i := 0; i < 12; i++ {
		d = node(t, nil, bs.Child{Key: []byte{0}, Data: d}, bs.Child{Key: []byte{1}, Data: d})
	}
	root, err := bs.PutData(ctx, s, d, bs.DataLimits{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bs.LoadData(ctx, s, root, bs.DataLimits{Nodes: 100})
	code(t, err, "limit_exceeded")
	if len(s.reads) > 13 {
		t.Fatal("shared chunks fetched repeatedly")
	}
	_, _, err = bs.EncodeLinked(d, bs.DataLimits{Nodes: 100})
	code(t, err, "limit_exceeded")
	_, err = bs.LoadData(ctx, s, root, bs.DataLimits{UniqueChunks: 2})
	code(t, err, "limit_exceeded")
	_, err = bs.LoadData(ctx, s, root, bs.DataLimits{ChunkBytes: 8})
	code(t, err, "limit_exceeded")
	_, err = bs.EncodeFlat(d, bs.DataLimits{Depth: 2})
	code(t, err, "limit_exceeded")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = bs.PutData(cancelled, s, d, bs.DataLimits{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func FuzzDecodeFlat(f *testing.F) {
	f.Add([]byte("dxf2\x02\x00\x01\x00\x00"))
	f.Fuzz(func(t *testing.T, b []byte) {
		d, err := bs.DecodeFlat(b, bs.DataLimits{FlatBytes: 4096, PayloadBytes: 4096, Children: 64, Depth: 32, Nodes: 256})
		if err != nil {
			return
		}
		out, err := bs.EncodeFlat(d, bs.DataLimits{})
		if err != nil || !bytes.Equal(out, b) {
			t.Fatalf("accepted noncanonical input: %x", b)
		}
	})
}
