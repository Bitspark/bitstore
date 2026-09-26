// Package bitstore is the Go SDK of bitstore, content-addressed storage for
// Data, structured DataTree values and raw bytes: a blob's name is its SHA-256 digest, so the same bytes always
// have the same name and a client that hashes what it receives need not trust
// where it came from.
//
// Store is the semantic interface of the contract's nine verbs (api/SURFACE.md).
// NewMemory returns an in-memory Store for tests and for composing clients; the
// HTTP client is public; the deployed filesystem store remains in the private
// bitstore-svc service.
package bitstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// Contract is the date on the contract line of api/SURFACE.md, the operations
// surface.
const Contract = "2026-09-24"

// ManagementContract is the date on the contract line of
// api/management/SURFACE.md, the instance-administration surface. The two
// change independently.
const ManagementContract = "2026-09-25"

// A Name is sha256:<64 lowercase hexadecimal digits>, the digest of a blob's
// bytes. Names compare as strings; the grammar is the only canonical form.
type Name string

var grammar = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// NameOf is the name of the bytes.
func NameOf(b []byte) Name {
	sum := sha256.Sum256(b)
	return Name("sha256:" + hex.EncodeToString(sum[:]))
}

// ParseName checks s against the name grammar.
func ParseName(s string) (Name, error) {
	if !grammar.MatchString(s) {
		return "", fmt.Errorf("%w: %q is not sha256:<64 lowercase hex digits>", ErrInvalidName, s)
	}
	return Name(s), nil
}

// Digest is the name's 64 hexadecimal digits.
func (n Name) Digest() string { return string(n)[len("sha256:"):] }

// The contract's two refusals, a protocol error, and the fault a client
// detects itself. Callers match them with errors.Is.
var (
	// ErrNotFound: the name asked for is not here. Only the singular reads
	// refuse with it; in the plural verbs absence is an omitted key.
	ErrNotFound = errors.New("bitstore: not found")
	// ErrTooLarge: the bytes exceed what this store accepts. Nothing was
	// committed.
	ErrTooLarge = errors.New("bitstore: too large")
	// ErrInvalidName: a string outside the name grammar. It is a protocol
	// error, not a refusal of content.
	ErrInvalidName = errors.New("bitstore: invalid name")
	// ErrIntegrity: bytes that do not hash to the name they were asked for.
	// They are never returned as data.
	ErrIntegrity = errors.New("bitstore: integrity")
)

// A PutResult is one item's outcome in PutMany: a name and size, or the error
// that refused that item alone.
type PutResult struct {
	Name Name
	Size int64
	Err  error
}

// Store is the contract's nine verbs. Every implementation is held to the
// conformance package's RunStore.
type Store interface {
	// Get returns the bytes of one blob, or ErrNotFound.
	Get(ctx context.Context, name Name) ([]byte, error)
	// GetMany returns the blobs that are here, keyed by name; absent and
	// duplicate names add nothing.
	GetMany(ctx context.Context, names []Name) (map[Name][]byte, error)
	// Has returns which of the names are here, once each, in request order.
	Has(ctx context.Context, names []Name) ([]Name, error)
	// Size returns the byte count of each name that is here.
	Size(ctx context.Context, names []Name) (map[Name]int64, error)
	// GetRange returns the overlap of [offset, offset+length) with the blob;
	// a negative length reads to the end, and a range past the end is a short
	// read. A range cannot be verified against the name.
	GetRange(ctx context.Context, name Name, offset, length int64) ([]byte, error)
	// GetStream returns the blob's bytes incrementally; closing the reader
	// early is cancellation, and nothing else.
	GetStream(ctx context.Context, name Name) (io.ReadCloser, error)
	// Put stores the bytes under their name. It is idempotent.
	Put(ctx context.Context, b []byte) (Name, error)
	// PutMany stores each item on its own: an item refused, say for size,
	// refuses alone, and its neighbors land.
	PutMany(ctx context.Context, items [][]byte) ([]PutResult, error)
	// PutStream stores what the reader yields, committing only if it ends
	// cleanly; any error or cancellation commits nothing.
	PutStream(ctx context.Context, r io.Reader) (Name, int64, error)
}
