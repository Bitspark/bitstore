// Command trees is bitstore's test-only DataTree driver for bitwire's structural
// cases, run by scripts/trees.mjs. Expectations are withheld: it interprets
// inputs and records what happened. "production" is bitstore's NewDataTree,
// Select and Read; every other realization is deliberately unlawful and must
// fail.
//
// The cases are written for WireTree. A Data reader stands where a case says
// "send": the named reader was invoked and read ("delivered", with that reader
// instance's invocation count), or a present node's failing reader was invoked
// ("refused"). "missing" means selection found no node, no reader was invoked,
// and the derived read reported structural absence. docs/TREES.md has the full
// mapping.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	bs "github.com/Bitspark/bitstore/store/go"
)

// ---- Inputs ----
type declaration struct {
	ID       string      `json:"id"`
	Own      *string     `json:"own"`
	Children [][2]string `json:"children"`
}
type step struct {
	Op         string     `json:"op"`
	Path       []string   `json:"path"`
	Keep       [][]string `json:"keep"`
	Selections [][]string `json:"selections"`
	Paths      [][]string `json:"paths"`
	Via        string     `json:"via"`
	Key        string     `json:"key"`
	To         string     `json:"to"`
	Node       string     `json:"node"`
	Mode       string     `json:"mode"`
	Own        *string    `json:"own"`
}
type testCase struct {
	ID      string `json:"id"`
	Family  string `json:"family"`
	Root    string `json:"root"`
	Fault   string `json:"fault"`
	Foreign bool   `json:"foreign"`
	Steps   []step `json:"steps"`
}
type inputs struct {
	Declarations []declaration `json:"declarations"`
	Cases        []testCase    `json:"cases"`
}

func keys(path []string) bs.TreePath {
	result := make(bs.TreePath, len(path))
	for i, text := range path {
		key, err := hex.DecodeString(text)
		if err != nil {
			panic(err)
		}
		result[i] = key
	}
	return result
}

// identical reports whether a and b are the same capability, without panicking
// on values whose dynamic type cannot be compared.
func identical(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	t := reflect.TypeOf(a)
	return t == reflect.TypeOf(b) && t.Comparable() && a == b
}

// walk selects by exact keys for driver-implemented nodes, independently of
// the realization under test.
func walk(tree bs.DataTree, path bs.TreePath) (bs.DataTree, bool) {
	node := tree
	for _, key := range path {
		var next bs.DataTree
		for _, c := range node.Children() {
			if bytes.Equal(c.Key, key) {
				next = c.Tree
				break
			}
		}
		if next == nil {
			return nil, false
		}
		node = next
	}
	return node, true
}

// ---- Realizations ----
type realization struct {
	compose func(own bs.Data, children []bs.Child[bs.Data]) (bs.DataTree, error)
	sel     func(tree bs.DataTree, path bs.TreePath) (bs.DataTree, bool)
	read    func(ctx context.Context, tree bs.DataTree, path bs.TreePath) (bs.Bytes, error)
}

// production is the implementation under test: bitstore's public operations.
var production = realization{
	compose: func(own bs.Data, children []bs.Child[bs.Data]) (bs.DataTree, error) {
		return bs.NewDataTree(own, children...)
	},
	sel:  bs.Select[bs.Data],
	read: bs.Read,
}

func present(tree bs.DataTree, path bs.TreePath) bool {
	_, ok := bs.Select(tree, path)
	return ok
}

// derivedFrom builds a derived read from a realization's own selection.
func derivedFrom(sel func(bs.DataTree, bs.TreePath) (bs.DataTree, bool)) func(context.Context, bs.DataTree, bs.TreePath) (bs.Bytes, error) {
	return func(ctx context.Context, tree bs.DataTree, path bs.TreePath) (bs.Bytes, error) {
		node, ok := sel(tree, path)
		if !ok {
			return nil, bs.ErrPathNotFound
		}
		b, err := node.Own().Read(ctx)
		if err != nil {
			return nil, err
		}
		return bytes.Clone(b), nil
	}
}

type failing struct{}

func (failing) Read(context.Context) (bs.Bytes, error) { return nil, errors.New("fabricated") }

type forwarder struct{ inner bs.Data }

func (f *forwarder) Read(ctx context.Context) (bs.Bytes, error) { return f.inner.Read(ctx) }

// partial omits the empty key from Children, so its child map is incomplete.
type partial struct{ node bs.DataTree }

func (p *partial) Own() bs.Data { return p.node.Own() }
func (p *partial) Children() []bs.Child[bs.Data] {
	var result []bs.Child[bs.Data]
	for _, c := range p.node.Children() {
		if len(c.Key) > 0 {
			result = append(result, c)
		}
	}
	return result
}
func (p *partial) At(path bs.TreePath) (bs.DataTree, bool)   { return p.node.At(path) }
func (p *partial) Decompose() (bs.Data, []bs.Child[bs.Data]) { return p.node.Decompose() }

// node is a driver-implemented DeixisNode: a foreign child in the cases, and
// the construction of the cycle-accepting realization.
type node struct {
	own      bs.Data
	children []bs.Child[bs.Data]
}

func (n *node) Own() bs.Data { return n.own }
func (n *node) Children() []bs.Child[bs.Data] {
	result := make([]bs.Child[bs.Data], len(n.children))
	for i, c := range n.children {
		result[i] = bs.Child[bs.Data]{Key: bytes.Clone(c.Key), Tree: c.Tree}
	}
	return result
}
func (n *node) At(path bs.TreePath) (bs.DataTree, bool)   { return walk(n, path) }
func (n *node) Decompose() (bs.Data, []bs.Child[bs.Data]) { return n.own, n.Children() }

// unvalidated is bitstore v0.2.0's construction: exact keys and no duplicates,
// but no search for a cycle through a foreign child.
func unvalidated(own bs.Data, children []bs.Child[bs.Data]) (bs.DataTree, error) {
	seen := map[string]bool{}
	n := &node{own: own}
	for _, c := range children {
		if c.Tree == nil || seen[string(c.Key)] {
			return nil, errors.New("invalid child")
		}
		seen[string(c.Key)] = true
		n.children = append(n.children, bs.Child[bs.Data]{Key: bytes.Clone(c.Key), Tree: c.Tree})
	}
	return n, nil
}

func realizations() map[string]realization {
	with := func(f func(r *realization)) realization {
		r := production
		f(&r)
		return r
	}
	reading := func(read func(context.Context, bs.DataTree, bs.TreePath) (bs.Bytes, error)) realization {
		return with(func(r *realization) { r.read = read })
	}
	rekey := func(f func([]byte) []byte) realization {
		return with(func(r *realization) {
			r.compose = func(own bs.Data, children []bs.Child[bs.Data]) (bs.DataTree, error) {
				next := make([]bs.Child[bs.Data], len(children))
				for i, c := range children {
					next[i] = bs.Child[bs.Data]{Key: f(bytes.Clone(c.Key)), Tree: c.Tree}
				}
				return bs.NewDataTree(own, next...)
			}
		})
	}
	return map[string]realization{
		"production": production,
		// A missing descendant is read from its deepest present ancestor.
		"fallback": reading(func(ctx context.Context, tree bs.DataTree, path bs.TreePath) (bs.Bytes, error) {
			n := tree
			for _, key := range path {
				if next, ok := bs.Select(n, bs.TreePath{key}); ok {
					n = next
				}
			}
			b, err := n.Own().Read(ctx)
			return bytes.Clone(b), err
		}),
		// A missing path selects a fabricated node whose reader fails.
		"fabricating": with(func(r *realization) {
			r.sel = func(tree bs.DataTree, path bs.TreePath) (bs.DataTree, bool) {
				if n, ok := bs.Select(tree, path); ok {
					return n, true
				}
				n, err := bs.NewDataTree(failing{})
				return n, err == nil
			}
			r.read = derivedFrom(r.sel)
		}),
		// A missing path reads as empty content.
		"empty-for-missing": reading(func(ctx context.Context, tree bs.DataTree, path bs.TreePath) (bs.Bytes, error) {
			if !present(tree, path) {
				return bs.Bytes{}, nil
			}
			return bs.Read(ctx, tree, path)
		}),
		// A missing path is reported as a failed read rather than as absence.
		"missing-as-failure": reading(func(ctx context.Context, tree bs.DataTree, path bs.TreePath) (bs.Bytes, error) {
			if !present(tree, path) {
				return nil, errors.New("unreadable")
			}
			return bs.Read(ctx, tree, path)
		}),
		// A reader's failure is reported as empty content.
		"swallowing": reading(func(ctx context.Context, tree bs.DataTree, path bs.TreePath) (bs.Bytes, error) {
			b, err := bs.Read(ctx, tree, path)
			if err != nil && present(tree, path) {
				return bs.Bytes{}, nil
			}
			return b, err
		}),
		// The derived read alters the bytes it returns.
		"corrupting": reading(func(ctx context.Context, tree bs.DataTree, path bs.TreePath) (bs.Bytes, error) {
			b, err := bs.Read(ctx, tree, path)
			if err != nil {
				return nil, err
			}
			return append(b, 0), nil
		}),
		// The derived read hands the caller the reader's own buffer.
		"aliasing": reading(func(ctx context.Context, tree bs.DataTree, path bs.TreePath) (bs.Bytes, error) {
			n, ok := bs.Select(tree, path)
			if !ok {
				return nil, bs.ErrPathNotFound
			}
			return n.Own().Read(ctx)
		}),
		// The derived read reports failure after its reader succeeded.
		"failing-after-read": reading(func(ctx context.Context, tree bs.DataTree, path bs.TreePath) (bs.Bytes, error) {
			if _, err := bs.Read(ctx, tree, path); err != nil {
				return nil, err
			}
			return nil, errors.New("lost")
		}),
		// The own reader is replaced by a forwarding wrapper.
		"wrapping-own": with(func(r *realization) {
			r.compose = func(own bs.Data, children []bs.Child[bs.Data]) (bs.DataTree, error) {
				return bs.NewDataTree(&forwarder{own}, children...)
			}
		}),
		// Children omits the empty key, so the child map is incomplete.
		"incomplete-children": with(func(r *realization) {
			r.compose = func(own bs.Data, children []bs.Child[bs.Data]) (bs.DataTree, error) {
				n, err := bs.NewDataTree(own, children...)
				if err != nil {
					return nil, err
				}
				return &partial{n}, nil
			}
		}),
		// Keys pass through lossy text conversion: ff becomes U+FFFD.
		"lossy-keys": rekey(func(key []byte) []byte { return []byte(strings.ToValidUTF8(string(key), "�")) }),
		// Construction never looks for a cycle through a foreign child.
		"cycle-accepting": with(func(r *realization) { r.compose = unvalidated }),
		// A leading UTF-8 byte order mark is stripped from keys.
		"bom-stripping": rekey(func(key []byte) []byte { return bytes.TrimPrefix(key, []byte("\xef\xbb\xbf")) }),
	}
}

// ---- Instrumented readers and structural editing ----

// call is one reader invocation during a derived read. copy is the driver's
// own baseline of what the reader returned.
type call struct {
	err    error
	buffer []byte
	copy   []byte
}

type harness struct {
	r            realization
	trace        []any
	partsExact   bool
	unchanged    bool
	calls        []call
	declarations map[string]declaration
	instances    map[string]int
	shared       map[string]*reader
	built        map[string]bs.DataTree
}

func newHarness(r realization, in inputs) *harness {
	h := &harness{r: r, trace: []any{}, partsExact: true, unchanged: true, declarations: map[string]declaration{},
		instances: map[string]int{}, shared: map[string]*reader{}, built: map[string]bs.DataTree{}}
	for _, d := range in.Declarations {
		h.declarations[d.ID] = d
	}
	return h
}

func (h *harness) observe(entry ...any) { h.trace = append(h.trace, entry) }

// reader is one stateful instrumented reader per name and case.
type reader struct {
	h        *harness
	name     string
	instance int
	count    int
}

func (r *reader) Read(context.Context) (bs.Bytes, error) {
	r.count++
	r.h.observe("delivered", r.name, r.count)
	// Content unique to this invocation.
	buffer := []byte(fmt.Sprintf("%s#%d:%d", r.name, r.instance, r.count))
	r.h.calls = append(r.h.calls, call{buffer: buffer, copy: bytes.Clone(buffer)})
	return buffer, nil
}

// refuser is a present node's failing reader; it records its own failure.
type refuser struct {
	h   *harness
	err error
}

func (r *refuser) Read(context.Context) (bs.Bytes, error) {
	r.h.observe("refused")
	r.h.calls = append(r.h.calls, call{err: r.err})
	return nil, r.err
}

func (h *harness) primitive(name string, fresh bool) bs.Data {
	if found, ok := h.shared[name]; ok && !fresh {
		return found
	}
	h.instances[name]++
	r := &reader{h: h, name: name, instance: h.instances[name]}
	if !fresh {
		h.shared[name] = r
	}
	return r
}

func (h *harness) refuser() bs.Data { return &refuser{h: h, err: errors.New("refused")} }

func (h *harness) named(own bs.Data) string {
	r, ok := own.(*reader)
	if !ok || r.h != h {
		panic("an edit needs a named reader")
	}
	return r.name
}

// construct composes, then shows that neither the input nor the returned parts
// can change the tree. A refused construction panics with its error.
func (h *harness) construct(own bs.Data, children []bs.Child[bs.Data]) bs.DataTree {
	type part struct {
		key  string
		tree bs.DataTree
	}
	want := make([]part, len(children))
	input := make([]bs.Child[bs.Data], len(children))
	for i, c := range children {
		want[i] = part{hex.EncodeToString(c.Key), c.Tree}
		input[i] = bs.Child[bs.Data]{Key: bytes.Clone(c.Key), Tree: c.Tree}
	}
	tree, err := h.r.compose(own, input)
	if err != nil {
		panic(err)
	}
	spoil := func(children []bs.Child[bs.Data]) {
		for i := range children {
			for j := range children[i].Key {
				children[i].Key[j] = 'z'
			}
			children[i].Tree = nil
		}
	}
	spoil(input)
	same := func(got []bs.Child[bs.Data]) bool {
		if len(got) != len(want) {
			return false
		}
		for _, w := range want {
			found := false
			for _, g := range got {
				if hex.EncodeToString(g.Key) == w.key && identical(g.Tree, w.tree) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	exact := func() bool {
		o, cs := tree.Decompose()
		return identical(o, own) && identical(tree.Own(), own) && same(cs) && same(tree.Children())
	}
	ok := exact()
	_, decomposed := tree.Decompose()
	spoil(tree.Children())
	spoil(decomposed)
	h.partsExact = h.partsExact && ok && exact()
	return tree
}

func (h *harness) foreign(name string, children []bs.Child[bs.Data]) bs.DataTree {
	return &node{own: h.primitive(name, false), children: children}
}

// loop is a driver-implemented node that contains itself.
type loop struct{ h *harness }

func (l *loop) Own() bs.Data { return l.h.refuser() }
func (l *loop) Children() []bs.Child[bs.Data] {
	return []bs.Child[bs.Data]{{Key: []byte("again"), Tree: l}}
}
func (l *loop) At(bs.TreePath) (bs.DataTree, bool)        { return nil, false }
func (l *loop) Decompose() (bs.Data, []bs.Child[bs.Data]) { return l.Own(), l.Children() }

func (h *harness) build(id, fault, rootID string, foreign bool) bs.DataTree {
	if found, ok := h.built[id]; ok {
		return found
	}
	d := h.declarations[id]
	var children []bs.Child[bs.Data]
	for _, c := range d.Children {
		children = append(children, bs.Child[bs.Data]{Key: keys(c[:1])[0], Tree: h.build(c[1], fault, rootID, false)})
	}
	if id == rootID {
		switch fault {
		case "duplicate":
			children = append(children, bs.Child[bs.Data]{Key: []byte("a"), Tree: h.build("leaf", "", "", false)})
		case "missingChild":
			children = append(children, bs.Child[bs.Data]{Key: []byte("hole")})
		case "cycle":
			children = append(children, bs.Child[bs.Data]{Key: []byte("loop"), Tree: &loop{h}})
		}
		if foreign {
			inner := h.foreign("inner", nil)
			children = append(children, bs.Child[bs.Data]{Key: []byte("foreign"), Tree: h.foreign("foreign", []bs.Child[bs.Data]{{Key: []byte("inner"), Tree: inner}})})
		}
	}
	var own bs.Data
	if d.Own == nil {
		own = h.refuser()
	} else {
		own = h.primitive(*d.Own, false)
	}
	tree := h.construct(own, children)
	h.built[id] = tree
	return tree
}

func (h *harness) render(tree bs.DataTree) map[string]any {
	var own any = "unknown"
	switch o := tree.Own().(type) {
	case *reader:
		if o.h == h {
			own = []any{o.name, o.instance}
		}
	case *refuser:
		if o.h == h {
			own = nil
		}
	}
	children := []any{}
	for _, c := range tree.Children() {
		children = append(children, []any{hex.EncodeToString(c.Key), h.render(c.Tree)})
	}
	return map[string]any{"own": own, "children": children}
}

func equalPath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (h *harness) rebuild(tree bs.DataTree, where []string, keep [][]string) bs.DataTree {
	for _, path := range keep {
		if equalPath(path, where) {
			return tree
		}
	}
	own, children := tree.Decompose()
	next := make([]bs.Child[bs.Data], len(children))
	for i, c := range children {
		next[i] = bs.Child[bs.Data]{Key: c.Key, Tree: h.rebuild(c.Tree, append(append([]string{}, where...), hex.EncodeToString(c.Key)), keep)}
	}
	return h.construct(own, next)
}

func (h *harness) replaceAt(tree bs.DataTree, path []string, f func(bs.DataTree) bs.DataTree) bs.DataTree {
	if len(path) == 0 {
		return f(tree)
	}
	own, children := tree.Decompose()
	next := make([]bs.Child[bs.Data], len(children))
	found := false
	for i, c := range children {
		next[i] = c
		if hex.EncodeToString(c.Key) == path[0] {
			found = true
			next[i].Tree = h.replaceAt(c.Tree, path[1:], f)
		}
	}
	if !found {
		panic("edit path leaves the tree")
	}
	return h.construct(own, next)
}

func (h *harness) copy(tree bs.DataTree) bs.DataTree {
	var fresh bs.Data
	if _, refusing := tree.Own().(*refuser); refusing {
		fresh = h.refuser()
	} else {
		fresh = h.primitive(h.named(tree.Own()), true)
	}
	children := tree.Children()
	next := make([]bs.Child[bs.Data], len(children))
	for i, c := range children {
		next[i] = bs.Child[bs.Data]{Key: c.Key, Tree: h.copy(c.Tree)}
	}
	return h.construct(fresh, next)
}

func (h *harness) edit(tree bs.DataTree, s step) bs.DataTree {
	switch s.Op {
	case "rebuild":
		return h.rebuild(tree, []string{}, s.Keep)
	case "replace":
		return h.replaceAt(tree, s.Path, func(bs.DataTree) bs.DataTree { return h.build(s.Node, "", "", false) })
	case "own":
		return h.replaceAt(tree, s.Path, func(n bs.DataTree) bs.DataTree {
			own := h.refuser()
			if s.Own != nil {
				own = h.primitive(h.named(n.Own()), true)
			}
			return h.construct(own, n.Children())
		})
	case "substitute":
		return h.replaceAt(tree, s.Path, func(n bs.DataTree) bs.DataTree {
			if s.Mode == "copy" {
				return h.copy(n)
			}
			return h.rebuild(n, []string{}, nil)
		})
	case "omit", "rename", "add":
		return h.replaceAt(tree, s.Path, func(n bs.DataTree) bs.DataTree {
			var next []bs.Child[bs.Data]
			for _, c := range n.Children() {
				k := hex.EncodeToString(c.Key)
				if s.Op == "omit" && k == s.Key {
					continue
				}
				if s.Op == "rename" && k == s.Key {
					c.Key = keys([]string{s.To})[0]
				}
				next = append(next, c)
			}
			if s.Op == "add" {
				next = append(next, bs.Child[bs.Data]{Key: keys([]string{s.Key})[0], Tree: h.build(s.Node, "", "", false)})
			}
			return h.construct(n.Own(), next)
		})
	}
	panic("unknown edit " + s.Op)
}

// chain applies a selection chain with each node's own At. From the root, the
// chain followed by path must select exactly what the concatenation selects,
// including when a selection in the chain fails.
func (h *harness) chain(start bs.DataTree, selections [][]string, path []string, fromRoot bool) bs.DataTree {
	n := start
	for _, selection := range selections {
		next, ok := n.At(keys(selection))
		if !ok || next == nil {
			n = nil
			break
		}
		n = next
	}
	if fromRoot {
		var all []string
		for _, selection := range selections {
			all = append(all, selection...)
		}
		whole, wholeOK := h.r.sel(start, keys(append(all, path...)))
		var stepped bs.DataTree
		steppedOK := false
		if n != nil {
			stepped, steppedOK = h.r.sel(n, keys(path))
		}
		if wholeOK != steppedOK || (wholeOK && !identical(whole, stepped)) {
			h.observe("selectionDiffers")
		}
	}
	return n
}

// derived reads at path. A present node's reader records its own success or
// failure; what the caller sees must be exactly that. A missing path invokes
// no reader and reports absence. Anything else is recorded as the violation it is.
func (h *harness) derived(base bs.DataTree, path []string) {
	if base == nil {
		h.observe("missing")
		return
	}
	k := keys(path)
	_, isPresent := h.r.sel(base, k)
	h.calls = nil
	value, err := h.r.read(context.Background(), base, k)
	calls := h.calls
	h.calls = nil
	if !isPresent {
		switch {
		case len(calls) > 0:
			h.observe("fallback")
		case err == nil:
			h.observe("missingAdmitted")
		case errors.Is(err, bs.ErrPathNotFound):
			h.observe("missing")
		default:
			h.observe("missingMisreported")
		}
		return
	}
	switch {
	case len(calls) > 1:
		h.observe("readMoreThanOnce")
	case len(calls) == 0 && err == nil:
		h.observe("readWithoutOwn")
	case len(calls) == 0:
		h.observe("refusedWithoutOwn")
	case calls[0].err == nil && err != nil:
		h.observe("failedAfterRead")
	case calls[0].err != nil && err == nil:
		h.observe("failureSwallowed")
	case calls[0].err != nil:
		h.unchanged = h.unchanged && errors.Is(err, calls[0].err)
	default:
		// The caller holds exactly this invocation's bytes, in a buffer the reader does not share.
		c := calls[0]
		exact := bytes.Equal(value, c.copy)
		for i := range c.buffer {
			c.buffer[i] = 'z'
		}
		h.unchanged = h.unchanged && exact && bytes.Equal(value, c.copy)
	}
}

func (h *harness) buildRoot(test testCase) (root bs.DataTree, refused bool) {
	defer func() {
		if recover() != nil {
			root, refused = nil, true
		}
	}()
	return h.build(test.Root, test.Fault, test.Root, test.Foreign), false
}

func local(r realization, in inputs, test testCase) map[string]any {
	h := newHarness(r, in)
	root, refused := h.buildRoot(test)
	if refused {
		return map[string]any{"construction": "refused"}
	}
	if test.Fault != "" {
		return map[string]any{"construction": "accepted"}
	}
	var view bs.DataTree
	for _, s := range test.Steps {
		switch s.Op {
		case "structure":
			if n, ok := r.sel(root, keys(s.Path)); ok {
				h.observe("structure", h.render(n))
			} else {
				h.observe("structure", "missing")
			}
		case "send":
			start := root
			if s.Via == "view" {
				start = view
			}
			var base bs.DataTree
			if start != nil {
				base = h.chain(start, s.Selections, s.Path, s.Via != "view")
			}
			h.derived(base, s.Path)
		case "same":
			a, aOK := r.sel(root, keys(s.Paths[0]))
			b, bOK := r.sel(root, keys(s.Paths[1]))
			h.observe("same", aOK && bOK && identical(a.Own(), b.Own()))
		case "view":
			view = h.chain(root, s.Selections, nil, true)
		default:
			root = h.edit(root, s)
		}
	}
	return map[string]any{"trace": h.trace, "partsExact": h.partsExact, "unchanged": h.unchanged}
}

// observations runs one case. Only an unlawful realization may break the
// harness; that is a failed case too.
func observations(name string, r realization, in inputs, test testCase) (result map[string]any) {
	if name != "production" {
		defer func() {
			if v := recover(); v != nil {
				result = map[string]any{"failed": fmt.Sprint(v)}
			}
		}()
	}
	return local(r, in, test)
}

func main() {
	all := realizations()
	if len(os.Args) != 3 || all[os.Args[1]].compose == nil {
		fmt.Fprintln(os.Stderr, "usage: trees production|<unlawful realization> inputs.json")
		os.Exit(2)
	}
	name, r := os.Args[1], all[os.Args[1]]
	data, err := os.ReadFile(os.Args[2])
	if err != nil {
		panic(err)
	}
	var in inputs
	if err := json.Unmarshal(data, &in); err != nil {
		panic(err)
	}
	output := []any{}
	for _, test := range in.Cases {
		if test.Family == "structure" {
			output = append(output, map[string]any{"id": test.ID, "observations": observations(name, r, in, test)})
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		panic(err)
	}
}
