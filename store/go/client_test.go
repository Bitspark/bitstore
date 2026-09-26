package bitstore_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	bitstore "github.com/Bitspark/bitstore/store/go"
)

// liar answers every request with the handler, as a bitstore would, headers
// included, so that only the lie is wrong.
func liar(t *testing.T, h http.HandlerFunc) *bitstore.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Bit-Service", "bitstore")
		w.Header().Set("Bit-Contract", bitstore.Contract)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := bitstore.New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var (
	ctx      = context.Background()
	asked    = []byte("the bytes asked for")
	other    = []byte("other bytes entirely")
	askedFor = bitstore.NameOf(asked)
)

// Checklist item 10: bytes that do not hash to the name asked for are an
// integrity error, never data -- whole, streamed or batched.
func TestVerifyOnRead(t *testing.T) {
	c := liar(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/blobs/" + string(askedFor):
			w.Write(other)
		case "/v1/get":
			fmt.Fprintf(w, "%s %d\n%s", askedFor, len(other), other)
		}
	})
	if b, err := c.Get(ctx, askedFor); !errors.Is(err, bitstore.ErrIntegrity) {
		t.Errorf("get returned %q, %v; want ErrIntegrity", b, err)
	}
	r, err := c.GetStream(ctx, askedFor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(r); !errors.Is(err, bitstore.ErrIntegrity) {
		t.Errorf("get stream ended with %v; want ErrIntegrity", err)
	}
	if blobs, err := c.GetMany(ctx, []bitstore.Name{askedFor}); !errors.Is(err, bitstore.ErrIntegrity) {
		t.Errorf("get many returned %d blobs, %v; want ErrIntegrity", len(blobs), err)
	}
}

func TestProtocolFaults(t *testing.T) {
	unasked := bitstore.NameOf(other)
	cases := []struct {
		name string
		h    http.HandlerFunc
		call func(c *bitstore.Client) error
		want error
	}{
		{"a record nobody asked for", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "%s %d\n%s", unasked, len(other), other)
		}, func(c *bitstore.Client) error { _, err := c.GetMany(ctx, []bitstore.Name{askedFor}); return err }, bitstore.ErrProtocol},
		{"a record twice", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "%s %d\n%s%s %d\n%s", askedFor, len(asked), asked, askedFor, len(asked), asked)
		}, func(c *bitstore.Client) error { _, err := c.GetMany(ctx, []bitstore.Name{askedFor}); return err }, bitstore.ErrProtocol},
		{"a record cut short", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "%s %d\n%s", askedFor, len(asked), asked[:4])
		}, func(c *bitstore.Client) error { _, err := c.GetMany(ctx, []bitstore.Name{askedFor}); return err }, bitstore.ErrProtocol},
		{"a size for a name not asked", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"sizes": {%q: 3}}`, unasked)
		}, func(c *bitstore.Client) error { _, err := c.Size(ctx, []bitstore.Name{askedFor}); return err }, bitstore.ErrProtocol},
		{"an upload acknowledged under another name", func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			fmt.Fprintf(w, `{"name": %q, "size": %d}`, unasked, len(asked))
		}, func(c *bitstore.Client) error { _, err := c.Put(ctx, asked); return err }, bitstore.ErrIntegrity},
		{"a batch item acknowledged under another name", func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			fmt.Fprintf(w, `{"results": [{"name": %q, "size": %d}]}`, unasked, len(asked))
		}, func(c *bitstore.Client) error { _, err := c.PutMany(ctx, [][]byte{asked}); return err }, bitstore.ErrIntegrity},
		{"fewer results than items", func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			io.WriteString(w, `{"results": []}`)
		}, func(c *bitstore.Client) error { _, err := c.PutMany(ctx, [][]byte{asked}); return err }, bitstore.ErrProtocol},
		{"a range longer than asked", func(w http.ResponseWriter, r *http.Request) {
			w.Write(asked)
		}, func(c *bitstore.Client) error { _, err := c.GetRange(ctx, askedFor, 0, 2); return err }, bitstore.ErrProtocol},
		{"a refusal without the envelope", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
			io.WriteString(w, "404 page not found")
		}, func(c *bitstore.Client) error { _, err := c.Get(ctx, askedFor); return err }, bitstore.ErrProtocol},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.call(liar(t, c.h)); !errors.Is(err, c.want) {
				t.Errorf("%v; want %v", err, c.want)
			}
		})
	}
}

// Whatever answered is not a bitstore unless it says so, and the client
// follows no redirect.
func TestTheClientTrustsNoOtherService(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"no Bit-Service": func(w http.ResponseWriter, r *http.Request) { w.Write(asked) },
		"another service": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Bit-Service", "bitwire")
			w.Write(asked)
		},
		"a redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			c, _ := bitstore.New(srv.URL)
			if _, err := c.Get(ctx, askedFor); !errors.Is(err, bitstore.ErrProtocol) {
				t.Errorf("%v; want ErrProtocol", err)
			}
		})
	}
}

func TestRefusalsMapToTheirErrors(t *testing.T) {
	for code, want := range map[string]error{"not_found": bitstore.ErrNotFound, "too_large": bitstore.ErrTooLarge, "bad_request": bitstore.ErrBadRequest} {
		c := liar(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(map[string]int{"not_found": 404, "too_large": 413, "bad_request": 400}[code])
			fmt.Fprintf(w, `{"error": %q, "message": "refused"}`, code)
		})
		if _, err := c.Get(ctx, askedFor); !errors.Is(err, want) {
			t.Errorf("%s: %v; want %v", code, err, want)
		}
	}
}

func TestNewRefusesAnEndpointItCannotUse(t *testing.T) {
	for _, bad := range []string{"", "bitstore.local", "ftp://host", "http://", "http://host/?q=1", "http://host/#f", "http://user:pw@host/"} {
		if _, err := bitstore.New(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, err := bitstore.New("https://host/api/operations"); err != nil {
		t.Errorf("a base path was refused: %v", err)
	}
}

func TestAStreamCutShortIsNotData(t *testing.T) {
	c := liar(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(asked)))
		w.Write(asked[:5])
	})
	if b, err := c.Get(ctx, askedFor); err == nil {
		t.Errorf("a cut body returned %q", b)
	}
}

// Each client accepts only its own surface: operations an omitted header or
// "operations" once; management "management" once. An empty, repeated or
// unknown discriminator is refused, before the body is read.
func TestSurfaceIdentity(t *testing.T) {
	health := `{"status":"ok","service":"bitstore","contract":"x","build":{"revision":"unknown","dirty":true,"go":"go"},"time":"2026-09-25T00:00:00Z","checks":[]}`
	cases := []struct {
		name       string
		surfaces   []string // Bit-Surface values sent; nil sends none
		operations bool     // an operations client accepts it
		management bool     // a management client accepts it
	}{
		{"no discriminator", nil, true, false},
		{"operations", []string{"operations"}, true, false},
		{"management", []string{"management"}, false, true},
		{"explicitly empty", []string{""}, false, false},
		{"repeated", []string{"management", "management"}, false, false},
		{"unknown", []string{"admin"}, false, false},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Bit-Service", "bitstore")
			for _, s := range c.surfaces {
				w.Header().Add("Bit-Surface", s)
			}
			io.WriteString(w, health)
		}))
		o, _ := bitstore.New(srv.URL)
		m, _ := bitstore.NewManagement(srv.URL)
		if _, err := o.Livez(ctx); (err == nil) != c.operations {
			t.Errorf("%s: an operations client: %v", c.name, err)
		}
		if _, err := m.Livez(ctx); (err == nil) != c.management {
			t.Errorf("%s: a management client: %v", c.name, err)
		}
		srv.Close()
	}
}

// A management description must name its surface.
func TestAManagementDescriptionNamesItsSurface(t *testing.T) {
	for surface, ok := range map[string]bool{`"management"`: true, `"operations"`: false, `""`: false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Bit-Service", "bitstore")
			w.Header().Set("Bit-Surface", "management")
			fmt.Fprintf(w, `{"service":"bitstore","surface":%s,"contract":"x","operations":[],"refusals":[],"files":[]}`, surface)
		}))
		m, _ := bitstore.NewManagement(srv.URL)
		if _, err := m.Describe(ctx); (err == nil) != ok {
			t.Errorf("surface %s: %v", surface, err)
		}
		srv.Close()
	}
}
