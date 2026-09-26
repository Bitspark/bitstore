package conformance

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	bitstore "github.com/Bitspark/bitstore/store/go"
)

// A Subject is a store under test, empty and owned by the test.
type Subject struct {
	Store bitstore.Store
	// Reopen, when the store is durable, closes nothing and returns a store
	// over the same data, as after a restart. Nil for a store that keeps
	// nothing across restarts; the restart case is then skipped and says so.
	Reopen func(t *testing.T) bitstore.Store
	// MaxBytes is the store's per-blob cap, at least 1 MiB + 1 so that every
	// vector fits; zero skips the cases that need a cap, and says so.
	MaxBytes int64
}

// RunStore holds a store to the vectors and to every checklist item of
// README.md that a Store can show: 1 to 8, and 9 where the subject can
// reopen its data. New is called once per case and returns a fresh, empty
// subject. The suite reads the store only through its interface.
func RunStore(t *testing.T, newSubject func(t *testing.T) Subject) {
	vectors, err := Vectors()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	t.Run("vectors", func(t *testing.T) {
		s := newSubject(t).Store
		for _, v := range vectors {
			content, err := v.Content.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			name, err := s.Put(ctx, content)
			if err != nil || name != bitstore.Name(v.Name) {
				t.Fatalf("%s: put returned %s, %v; want %s", v.Comment, name, err, v.Name)
			}
			got, err := s.Get(ctx, name)
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("%s: get returned %d bytes, %v; want the %d bytes put", v.Comment, len(got), err, len(content))
			}
			sizes, err := s.Size(ctx, []bitstore.Name{name})
			if err != nil || sizes[name] != v.Size {
				t.Fatalf("%s: size is %v, %v; want %d", v.Comment, sizes, err, v.Size)
			}
			for _, rg := range [][2]int64{{0, 0}, {0, 1}, {0, -1}, {1, 3}, {v.Size / 2, v.Size}, {v.Size - 1, 5}, {v.Size, 1}, {v.Size + 7, 2}} {
				if rg[0] < 0 {
					continue // the empty vector has no last byte
				}
				want := overlap(content, rg[0], rg[1])
				got, err := s.GetRange(ctx, name, rg[0], rg[1])
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("%s: range %v returned %d bytes, %v; want %d", v.Comment, rg, len(got), err, len(want))
				}
			}
			r, err := s.GetStream(ctx, name)
			if err != nil {
				t.Fatalf("%s: get stream: %v", v.Comment, err)
			}
			streamed, err := io.ReadAll(r)
			r.Close()
			if err != nil || !bytes.Equal(streamed, content) {
				t.Fatalf("%s: get stream returned %d bytes, %v", v.Comment, len(streamed), err)
			}
		}
	})

	t.Run("1 put is idempotent", func(t *testing.T) {
		s := newSubject(t).Store
		first, err1 := s.Put(ctx, []byte("same bytes"))
		second, err2 := s.Put(ctx, []byte("same bytes"))
		name, n, err3 := s.PutStream(ctx, bytes.NewReader([]byte("same bytes")))
		if err1 != nil || err2 != nil || err3 != nil || first != second || second != name || n != 10 {
			t.Fatalf("puts returned %s %v, %s %v, %s %d %v", first, err1, second, err2, name, n, err3)
		}
		has, err := s.Has(ctx, []bitstore.Name{first, first})
		if err != nil || !slices.Equal(has, []bitstore.Name{first}) {
			t.Fatalf("has returned %v, %v; want the name once", has, err)
		}
	})

	absent := bitstore.NameOf([]byte("never put"))

	t.Run("2 absence is data in the plural verbs", func(t *testing.T) {
		s := newSubject(t).Store
		here, _ := s.Put(ctx, []byte("here"))
		has, err := s.Has(ctx, []bitstore.Name{absent, here})
		if err != nil || !slices.Equal(has, []bitstore.Name{here}) {
			t.Errorf("has returned %v, %v", has, err)
		}
		sizes, err := s.Size(ctx, []bitstore.Name{absent, here})
		if _, ok := sizes[absent]; err != nil || ok || sizes[here] != 4 {
			t.Errorf("size returned %v, %v", sizes, err)
		}
		blobs, err := s.GetMany(ctx, []bitstore.Name{absent, here})
		if _, ok := blobs[absent]; err != nil || ok || string(blobs[here]) != "here" {
			t.Errorf("get many returned %d blobs, %v", len(blobs), err)
		}
	})

	t.Run("3 absence is a refusal in the singular reads", func(t *testing.T) {
		s := newSubject(t).Store
		if _, err := s.Get(ctx, absent); !errors.Is(err, bitstore.ErrNotFound) {
			t.Errorf("get: %v, want ErrNotFound", err)
		}
		if _, err := s.GetRange(ctx, absent, 0, 1); !errors.Is(err, bitstore.ErrNotFound) {
			t.Errorf("get range: %v, want ErrNotFound", err)
		}
		if r, err := s.GetStream(ctx, absent); !errors.Is(err, bitstore.ErrNotFound) {
			if r != nil {
				r.Close()
			}
			t.Errorf("get stream: %v, want ErrNotFound", err)
		}
	})

	t.Run("4 range clamps", func(t *testing.T) {
		s := newSubject(t).Store
		name, _ := s.Put(ctx, []byte("0123456789"))
		for _, c := range []struct {
			offset, length int64
			want           string
		}{{8, 5, "89"}, {10, 1, ""}, {50, 1, ""}, {3, 0, ""}, {3, -1, "3456789"}} {
			got, err := s.GetRange(ctx, name, c.offset, c.length)
			if err != nil || string(got) != c.want {
				t.Errorf("range %d+%d returned %q, %v; want %q", c.offset, c.length, got, err, c.want)
			}
		}
	})

	t.Run("5 get many is keyed, unordered and deduplicated", func(t *testing.T) {
		s := newSubject(t).Store
		a, _ := s.Put(ctx, []byte("a"))
		b, _ := s.Put(ctx, []byte("b"))
		blobs, err := s.GetMany(ctx, []bitstore.Name{b, a, a, absent})
		if err != nil || len(blobs) != 2 || string(blobs[a]) != "a" || string(blobs[b]) != "b" {
			t.Errorf("get many returned %d blobs, %v; want a and b once each", len(blobs), err)
		}
	})

	t.Run("6 put stream commits only at clean end of stream", func(t *testing.T) {
		s := newSubject(t).Store
		failed, cancelled := []byte("a prefix that fails, "), []byte("a prefix that is cancelled, ")
		failing := io.MultiReader(bytes.NewReader(failed), &failAfter{err: errors.New("the client went away")})
		if _, _, err := s.PutStream(ctx, failing); err == nil {
			t.Fatal("an aborted stream was acknowledged")
		}
		// Cancelled while the stream is still open: its end never comes. (A
		// caller that cancels after every byte was sent may find the whole
		// blob committed; the contract allows it, and this case is not that.)
		cctx, cancel := context.WithCancel(ctx)
		cancelling := io.MultiReader(bytes.NewReader(cancelled), &cancelOpen{ctx: cctx, cancel: cancel})
		if _, _, err := s.PutStream(cctx, cancelling); err == nil {
			t.Fatal("a cancelled stream was acknowledged")
		}
		has, err := s.Has(ctx, []bitstore.Name{bitstore.NameOf(failed), bitstore.NameOf(cancelled)})
		if err != nil || len(has) != 0 {
			t.Errorf("visible after aborted streams: %v, %v", has, err)
		}
	})

	t.Run("7 too large commits nothing", func(t *testing.T) {
		sub := newSubject(t)
		if sub.MaxBytes == 0 {
			t.Skip("the subject has no cap")
		}
		s := sub.Store
		over := bytes.Repeat([]byte{7}, int(sub.MaxBytes)+1)
		if _, err := s.Put(ctx, over); !errors.Is(err, bitstore.ErrTooLarge) {
			t.Errorf("put: %v, want ErrTooLarge", err)
		}
		if _, _, err := s.PutStream(ctx, bytes.NewReader(over)); !errors.Is(err, bitstore.ErrTooLarge) {
			t.Errorf("put stream: %v, want ErrTooLarge", err)
		}
		has, err := s.Has(ctx, []bitstore.Name{bitstore.NameOf(over), bitstore.NameOf(over[:sub.MaxBytes])})
		if err != nil || len(has) != 0 {
			t.Errorf("visible after a refusal for size: %v, %v", has, err)
		}
		exact := over[:sub.MaxBytes]
		if name, err := s.Put(ctx, exact); err != nil || name != bitstore.NameOf(exact) {
			t.Errorf("a blob of exactly the cap: %s, %v", name, err)
		}
	})

	t.Run("8 put many is per item", func(t *testing.T) {
		sub := newSubject(t)
		items := [][]byte{[]byte("first"), []byte("third")}
		if sub.MaxBytes > 0 {
			items = [][]byte{[]byte("first"), bytes.Repeat([]byte{8}, int(sub.MaxBytes)+1), []byte("third")}
		}
		results, err := sub.Store.PutMany(ctx, items)
		if err != nil || len(results) != len(items) {
			t.Fatalf("put many returned %d results, %v; want %d", len(results), err, len(items))
		}
		for i, item := range items {
			refused := sub.MaxBytes > 0 && int64(len(item)) > sub.MaxBytes
			switch r := results[i]; {
			case refused && !errors.Is(r.Err, bitstore.ErrTooLarge):
				t.Errorf("item %d: %v, want ErrTooLarge", i, r.Err)
			case !refused && (r.Err != nil || r.Name != bitstore.NameOf(item) || r.Size != int64(len(item))):
				t.Errorf("item %d: %+v", i, r)
			}
		}
		has, _ := sub.Store.Has(ctx, []bitstore.Name{bitstore.NameOf([]byte("first")), bitstore.NameOf([]byte("third"))})
		if len(has) != 2 {
			t.Errorf("the neighbors of a refused item did not land: %v", has)
		}
	})

	t.Run("9 acknowledged content survives restart", func(t *testing.T) {
		sub := newSubject(t)
		if sub.Reopen == nil {
			t.Skip("the subject keeps nothing across restarts")
		}
		var names []bitstore.Name
		var contents [][]byte
		for _, v := range vectors {
			content, _ := v.Content.Bytes()
			name, _, err := sub.Store.PutStream(ctx, bytes.NewReader(content))
			if err != nil {
				t.Fatal(err)
			}
			names, contents = append(names, name), append(contents, content)
		}
		reopened := sub.Reopen(t)
		for i, name := range names {
			got, err := reopened.Get(ctx, name)
			if err != nil || !bytes.Equal(got, contents[i]) {
				t.Errorf("%s after restart: %d bytes, %v", name, len(got), err)
			}
		}
	})
}

// overlap is the expected answer to a range read, computed here rather than
// by the SDK, so that a mistake shared with the implementation is not the
// suite's only oracle.
func overlap(content []byte, offset, length int64) []byte {
	start := min(offset, int64(len(content)))
	end := int64(len(content))
	if length >= 0 {
		end = min(start+length, end)
	}
	return content[start:end]
}

type failAfter struct{ err error }

func (f *failAfter) Read([]byte) (int, error) { return 0, f.err }

// cancelOpen cancels the upload's context on its first read and never yields
// another byte: the stream is still open when it is cancelled.
type cancelOpen struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (c *cancelOpen) Read(p []byte) (int, error) {
	c.cancel()
	<-c.ctx.Done()
	return 0, c.ctx.Err()
}
