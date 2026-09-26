package bitstore_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	bs "github.com/Bitspark/bitstore/store/go"
)

type readerFunc func(context.Context) (bs.Bytes, error)

func (f readerFunc) Read(ctx context.Context) (bs.Bytes, error) { return f(ctx) }

// The slice field makes this external implementation non-comparable. The
// interface must still work; pointer identity is an optimization, not a law.
type externalNode struct {
	node   bs.DataTree
	marker []byte
}

func (n externalNode) Own() bs.Data                  { return n.node.Own() }
func (n externalNode) Children() []bs.Child[bs.Data] { return n.node.Children() }
func (n externalNode) At(p bs.TreePath) (bs.DataTree, bool) {
	if len(p) == 0 {
		return n, true
	}
	return n.node.At(p)
}
func (n externalNode) Decompose() (bs.Data, []bs.Child[bs.Data]) { return n.Own(), n.Children() }

func TestCommonStructureAndDerivedRead(t *testing.T) {
	ctx := context.Background()
	calls := 0
	own := readerFunc(func(context.Context) (bs.Bytes, error) { calls++; return []byte("root"), nil })
	leaf := node(t, []byte("child"))
	tree, err := bs.NewDataTree(own, bs.Child[bs.Data]{Key: []byte{255}, Tree: leaf})
	if err != nil {
		t.Fatal(err)
	}
	payload, children := tree.Decompose()
	rebuilt, err := bs.Compose[bs.Data](payload, children...)
	if err != nil || calls != 0 {
		t.Fatal("pure structure invoked read", err, calls)
	}
	children[0].Key[0] = 0
	if _, ok := tree.At(bs.TreePath{{255}}); !ok {
		t.Fatal("decomposition keys alias tree")
	}
	selected, ok := rebuilt.At(bs.TreePath{{255}})
	if !ok || selected != leaf {
		t.Fatal("recomposition changed selection")
	}
	b, err := bs.Read(ctx, tree, bs.TreePath{{255}})
	direct, _ := leaf.Own().Read(ctx)
	if err != nil || !bytes.Equal(b, direct) || calls != 0 {
		t.Fatal("derived read law", b, err)
	}
	if _, err = bs.Read(ctx, tree, bs.TreePath{{0}}); !errors.Is(err, bs.ErrPathNotFound) {
		t.Fatal(err)
	}
	_, err = bs.EncodeFlat(ctx, externalNode{tree, []byte{1}}, bs.DataTreeLimits{})
	if err != nil || calls != 1 {
		t.Fatal("external generic tree not accepted", err, calls)
	}
	// The exact same structure works for arbitrary payloads, including nil.
	other, err := bs.Compose[any](nil)
	if err != nil {
		t.Fatal(err)
	}
	op, cs := other.Decompose()
	if op != nil || len(cs) != 0 {
		t.Fatal("generic structure specialized to data")
	}
}

func TestDataReadErrorsAndCancellationAreNotMissing(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("reader unavailable")
	tree, err := bs.NewDataTree(readerFunc(func(context.Context) (bs.Bytes, error) { return nil, failure }))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tree.At(nil); !ok {
		t.Fatal("failure erased existing structure")
	}
	if _, err = bs.Read(ctx, tree, nil); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err = bs.EncodeFlat(ctx, tree, bs.DataTreeLimits{}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	store := bs.NewMemory(0)
	if _, err = bs.PutDataTree(ctx, store, tree, bs.DataTreeLimits{}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(ctx)
	tree, err = bs.NewDataTree(readerFunc(func(context.Context) (bs.Bytes, error) { cancel(); return []byte("must not publish"), nil }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bs.PutDataTree(ctx, store, tree, bs.DataTreeLimits{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Immutable in-memory Data protects both its input and every read result.
	input := []byte{7}
	data := bs.BytesData(input)
	input[0] = 0
	a, _ := data.Read(context.Background())
	a[0] = 1
	b, _ := data.Read(context.Background())
	if len(b) != 1 || b[0] != 7 {
		t.Fatal("reader aliases mutable bytes", b)
	}
}

type cyclicTree struct{ own bs.Data }

func (n *cyclicTree) Own() bs.Data                              { return n.own }
func (n *cyclicTree) Children() []bs.Child[bs.Data]             { return []bs.Child[bs.Data]{{Key: nil, Tree: n}} }
func (n *cyclicTree) At(bs.TreePath) (bs.DataTree, bool)        { return n, true }
func (n *cyclicTree) Decompose() (bs.Data, []bs.Child[bs.Data]) { return n.Own(), n.Children() }
func TestUnlawfulCyclicTreeIsRejected(t *testing.T) {
	_, err := bs.EncodeFlat(context.Background(), &cyclicTree{bs.BytesData(nil)}, bs.DataTreeLimits{})
	if !errors.Is(err, bs.ErrInvalidDataTree) {
		t.Fatal(err)
	}
}

// An interface field makes the type comparable while this particular value is
// not hashable. Encoding may memoize comparable values but must accept either.
type valueNode struct{ own bs.Data }

func (n valueNode) Own() bs.Data                  { return n.own }
func (n valueNode) Children() []bs.Child[bs.Data] { return nil }
func (n valueNode) At(p bs.TreePath) (bs.DataTree, bool) {
	if len(p) == 0 {
		return n, true
	}
	return nil, false
}
func (n valueNode) Decompose() (bs.Data, []bs.Child[bs.Data]) { return n.own, nil }

type sliceData []byte

func (d sliceData) Read(context.Context) (bs.Bytes, error) { return bytes.Clone(d), nil }
func TestInterfacePayloadDoesNotMakeMemoizationPanic(t *testing.T) {
	_, err := bs.EncodeFlat(context.Background(), valueNode{sliceData{1, 2}}, bs.DataTreeLimits{})
	if err != nil {
		t.Fatal(err)
	}
}
func TestStructuralBudgetRefusesBeforeReadingLargeKeyTree(t *testing.T) {
	reads := 0
	own := readerFunc(func(context.Context) (bs.Bytes, error) { reads++; return nil, nil })
	tree, err := bs.NewDataTree(own, bs.Child[bs.Data]{Key: make([]byte, 128), Tree: node(t, nil)})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = bs.EncodeLinked(context.Background(), tree, bs.DataTreeLimits{UnfoldedBytes: 16})
	code(t, err, "limit_exceeded")
	if reads != 0 {
		t.Fatal("read payload after structural budget exhausted", reads)
	}
}

func TestFlatBudgetCountsSharedChildrenPerOccurrence(t *testing.T) {
	leaf := node(t, []byte("x"))
	tree := node(t, nil, bs.Child[bs.Data]{Key: []byte("a"), Tree: leaf}, bs.Child[bs.Data]{Key: []byte("b"), Tree: leaf})
	_, err := bs.EncodeFlat(context.Background(), tree, bs.DataTreeLimits{UnfoldedBytes: 16})
	code(t, err, "limit_exceeded")
}

func TestFlatMaterializationPreservesLimitDimension(t *testing.T) {
	_, err := bs.EncodeFlat(context.Background(), node(t, nil), bs.DataTreeLimits{FlatBytes: 8})
	var fault *bs.CodecError
	if !errors.As(err, &fault) || fault.Dimension != "flat_artifact_octets" {
		t.Fatal(err)
	}
}
