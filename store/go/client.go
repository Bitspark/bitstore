package bitstore

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
)

// ErrProtocol is a response that is not what the binding documents: an answer
// from something that is not a bitstore, a malformed body, a record nobody
// asked for. It is never data and never a refusal.
var ErrProtocol = errors.New("bitstore: protocol")

// ErrBadRequest is the binding's refusal of a malformed request.
var ErrBadRequest = errors.New("bitstore: bad request")

const batchMediaType = "application/x-bitstore-blobs"

// Client is a Store over the operations listener's HTTP binding. It verifies
// what the contract lets a client verify: every response names the service
// and no other surface, every blob hashes to its name, every record was asked
// for, every upload is acknowledged under the name of the bytes sent. It never
// follows a redirect.
type Client struct{ conn }

var _ Store = (*Client)(nil)

// ManagementClient is a client of the management listener: its health, its
// description and its contract files. Every response must name the service
// and Bit-Surface: management. Its endpoint is configured on its own, never
// derived from an operations endpoint.
type ManagementClient struct{ conn }

// conn is one listener's endpoint and the surface its answers must name.
type conn struct {
	base    string
	http    *http.Client
	surface string                  // operations or management
	served  *atomic.Pointer[string] // the Bit-Contract of the last response
}

// Option configures a Client or a ManagementClient.
type Option func(*conn)

// WithHTTPClient sets the HTTP client; its redirect policy is replaced so that
// no redirect is followed.
func WithHTTPClient(h *http.Client) Option {
	return func(c *conn) {
		clone := *h
		c.http = &clone
	}
}

// New returns a client of the bitstore operations listener at endpoint, an
// absolute http or https URL with no query or fragment; its path, if any, is
// the base the binding's routes are relative to.
func New(endpoint string, opts ...Option) (*Client, error) {
	c, err := dial(endpoint, "operations", opts)
	if err != nil {
		return nil, err
	}
	return &Client{c}, nil
}

// NewManagement returns a client of the bitstore management listener at
// endpoint, under the same rules as New.
func NewManagement(endpoint string, opts ...Option) (*ManagementClient, error) {
	c, err := dial(endpoint, "management", opts)
	if err != nil {
		return nil, err
	}
	return &ManagementClient{c}, nil
}

func dial(endpoint, surface string, opts []Option) (conn, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return conn{}, fmt.Errorf("bitstore: %q is not an http or https endpoint without query, fragment or credentials", endpoint)
	}
	c := conn{base: strings.TrimSuffix(u.String(), "/"), http: &http.Client{}, surface: surface, served: &atomic.Pointer[string]{}}
	for _, opt := range opts {
		opt(&c)
	}
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c, nil
}

// ServedContract is the Bit-Contract of the last response; a caller compares
// it with Contract, or ManagementContract, and decides.
func (c *conn) ServedContract() string {
	if value := c.served.Load(); value != nil {
		return *value
	}
	return ""
}

func (c *conn) recordContract(value string) { c.served.Store(&value) }

// identify refuses an answer from anything but this service's surface, before
// its body is read: Bit-Service must be bitstore; a management answer must
// say Bit-Surface: management once, and an operations answer must omit the
// header or say operations once.
func (c *conn) identify(resp *http.Response, method, path string) error {
	if s := resp.Header.Get("Bit-Service"); s != "bitstore" {
		return fmt.Errorf("%w: %s %s answered %d as %q, not bitstore", ErrProtocol, method, path, resp.StatusCode, s)
	}
	surfaces := resp.Header.Values("Bit-Surface")
	ok := len(surfaces) == 1 && surfaces[0] == c.surface
	if c.surface == "operations" && len(surfaces) == 0 {
		ok = true
	}
	if !ok {
		return fmt.Errorf("%w: %s %s answered as surface %q, not %s", ErrProtocol, method, path, surfaces, c.surface)
	}
	return nil
}

// do sends a request and returns a successful response, or the error its
// status and envelope say; the caller closes the body.
func (c *conn) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (*http.Response, error) {
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if err := c.identify(resp, method, path); err != nil {
		resp.Body.Close()
		return nil, err
	}
	c.recordContract(resp.Header.Get("Bit-Contract"))
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	defer resp.Body.Close()
	var e Error
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if json.Unmarshal(raw, &e) != nil || e.Code == "" {
		return nil, fmt.Errorf("%w: %s %s answered %d without the error envelope", ErrProtocol, method, path, resp.StatusCode)
	}
	switch e.Code {
	case "not_found":
		return nil, fmt.Errorf("%w: %s", ErrNotFound, e.Message)
	case "too_large":
		return nil, fmt.Errorf("%w: %s", ErrTooLarge, e.Message)
	case "bad_request":
		return nil, fmt.Errorf("%w: %s", ErrBadRequest, e.Message)
	}
	return nil, fmt.Errorf("bitstore: %s %s failed, %d %s: %s", method, path, resp.StatusCode, e.Code, e.Message)
}

func (c *Client) Get(ctx context.Context, name Name) ([]byte, error) {
	r, err := c.GetStream(ctx, name)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// GetStream verifies as it reads: the bytes it yields are provisional until
// it returns io.EOF, and a body that does not hash to the name ends with
// ErrIntegrity instead.
func (c *Client) GetStream(ctx context.Context, name Name) (io.ReadCloser, error) {
	if _, err := ParseName(string(name)); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodGet, "/v1/blobs/"+string(name), nil, nil, "")
	if err != nil {
		return nil, err
	}
	return &verifying{body: resp.Body, h: sha256.New(), name: name}, nil
}

type verifying struct {
	body io.ReadCloser
	h    hash.Hash
	name Name
}

func (v *verifying) Read(p []byte) (int, error) {
	n, err := v.body.Read(p)
	v.h.Write(p[:n])
	if err == io.EOF {
		if got := Name("sha256:" + hex.EncodeToString(v.h.Sum(nil))); got != v.name {
			return n, fmt.Errorf("%w: asked for %s, received bytes of %s", ErrIntegrity, v.name, got)
		}
	}
	return n, err
}

func (v *verifying) Close() error { return v.body.Close() }

func (c *Client) GetRange(ctx context.Context, name Name, offset, length int64) ([]byte, error) {
	if _, err := ParseName(string(name)); err != nil {
		return nil, err
	}
	if offset < 0 {
		return nil, fmt.Errorf("bitstore: negative offset %d", offset)
	}
	q := url.Values{"offset": {strconv.FormatInt(offset, 10)}}
	if length >= 0 {
		q.Set("length", strconv.FormatInt(length, 10))
	}
	resp, err := c.do(ctx, http.MethodGet, "/v1/blobs/"+string(name), q, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err == nil && length >= 0 && int64(len(b)) > length {
		return nil, fmt.Errorf("%w: a range of at most %d bytes answered %d", ErrProtocol, length, len(b))
	}
	return b, err
}

func (c *Client) Size(ctx context.Context, names []Name) (map[Name]int64, error) {
	var out StatResponse
	if err := c.postNames(ctx, "/v1/stat", names, func(r io.Reader) error {
		return json.NewDecoder(r).Decode(&out)
	}); err != nil {
		return nil, err
	}
	for name, size := range out.Sizes {
		if !contains(names, name) || size < 0 {
			return nil, fmt.Errorf("%w: stat answered %s, which was not asked for or has a negative size", ErrProtocol, name)
		}
	}
	if out.Sizes == nil {
		out.Sizes = map[Name]int64{}
	}
	return out.Sizes, nil
}

func (c *Client) Has(ctx context.Context, names []Name) ([]Name, error) {
	sizes, err := c.Size(ctx, names)
	if err != nil {
		return nil, err
	}
	return Present(names, sizes), nil
}

// GetMany verifies every record: asked for, not repeated, hashing to its name.
func (c *Client) GetMany(ctx context.Context, names []Name) (map[Name][]byte, error) {
	found := map[Name][]byte{}
	err := c.postNames(ctx, "/v1/get", names, func(r io.Reader) error {
		br := bufio.NewReader(r)
		for {
			line, err := br.ReadString('\n')
			if err == io.EOF && line == "" {
				return nil
			}
			if err != nil || len(line) > 128 {
				return fmt.Errorf("%w: a malformed record header", ErrProtocol)
			}
			nameText, sizeText, ok := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
			name, nameErr := ParseName(nameText)
			size, sizeErr := strconv.ParseInt(sizeText, 10, 64)
			switch {
			case !ok || nameErr != nil || sizeErr != nil || size < 0:
				return fmt.Errorf("%w: a malformed record header %q", ErrProtocol, line)
			case !contains(names, name):
				return fmt.Errorf("%w: a record for %s, which was not asked for", ErrProtocol, name)
			case found[name] != nil:
				return fmt.Errorf("%w: %s answered twice", ErrProtocol, name)
			}
			b := make([]byte, size)
			if _, err := io.ReadFull(br, b); err != nil {
				return fmt.Errorf("%w: the record of %s is cut short", ErrProtocol, name)
			}
			if NameOf(b) != name {
				return fmt.Errorf("%w: the record of %s holds other bytes", ErrIntegrity, name)
			}
			found[name] = b
		}
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

func (c *Client) postNames(ctx context.Context, path string, names []Name, read func(io.Reader) error) error {
	raw, err := json.Marshal(NamesRequest{Names: append([]Name{}, names...)})
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPost, path, nil, bytes.NewReader(raw), "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return read(resp.Body)
}

func (c *Client) Put(ctx context.Context, b []byte) (Name, error) {
	name, _, err := c.PutStream(ctx, bytes.NewReader(b))
	return name, err
}

// PutStream hashes what it sends and holds the acknowledgment to it.
func (c *Client) PutStream(ctx context.Context, r io.Reader) (Name, int64, error) {
	h := sha256.New()
	counted := &counter{r: io.TeeReader(r, h)}
	resp, err := c.do(ctx, http.MethodPost, "/v1/blobs", nil, counted, "application/octet-stream")
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	var out PutResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, fmt.Errorf("%w: the acknowledgment is not JSON", ErrProtocol)
	}
	sent := Name("sha256:" + hex.EncodeToString(h.Sum(nil)))
	if out.Name != sent || out.Size != counted.n {
		return "", 0, fmt.Errorf("%w: sent %d bytes of %s, acknowledged %d of %s", ErrIntegrity, counted.n, sent, out.Size, out.Name)
	}
	return out.Name, out.Size, nil
}

type counter struct {
	r io.Reader
	n int64
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// PutMany sends the items as one batch and holds each result to its item. A
// batch-level error does not mean that no item was stored.
func (c *Client) PutMany(ctx context.Context, items [][]byte) ([]PutResult, error) {
	var body bytes.Buffer
	for _, item := range items {
		fmt.Fprintf(&body, "%d\n", len(item))
		body.Write(item)
	}
	resp, err := c.do(ctx, http.MethodPost, "/v1/put", nil, &body, batchMediaType)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out PutManyResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: the batch's results are not JSON: %v", ErrProtocol, err)
	}
	if len(out.Results) != len(items) {
		return nil, fmt.Errorf("%w: %d items sent, %d results", ErrProtocol, len(items), len(out.Results))
	}
	results := make([]PutResult, len(items))
	for i, r := range out.Results {
		switch {
		case r.Error == "too_large":
			results[i] = PutResult{Err: fmt.Errorf("%w: %s", ErrTooLarge, r.Message)}
		case r.Error != "":
			results[i] = PutResult{Err: fmt.Errorf("bitstore: item %d refused, %s: %s", i, r.Error, r.Message)}
		case r.Size == nil || r.Name != NameOf(items[i]) || *r.Size != int64(len(items[i])):
			return nil, fmt.Errorf("%w: item %d acknowledged under another name or size", ErrIntegrity, i)
		default:
			results[i] = PutResult{Name: r.Name, Size: *r.Size}
		}
	}
	return results, nil
}

// Health returns the /healthz envelope; a failing listener answers 503 with
// the envelope, which is returned with no error.
func (c *conn) Health(ctx context.Context) (*Health, error) { return c.health(ctx, "/healthz") }

// Livez returns the /livez envelope.
func (c *conn) Livez(ctx context.Context) (*Health, error) { return c.health(ctx, "/livez") }

func (c *conn) health(ctx context.Context, path string) (*Health, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.identify(resp, http.MethodGet, path); err != nil {
		return nil, err
	}
	c.recordContract(resp.Header.Get("Bit-Contract"))
	var h Health
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil || (resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable) {
		return nil, fmt.Errorf("%w: %s answered %d without the envelope", ErrProtocol, path, resp.StatusCode)
	}
	return &h, nil
}

// Describe returns the listener's description.
func (c *conn) Describe(ctx context.Context) (*Describe, error) {
	resp, err := c.do(ctx, http.MethodGet, "/describe", nil, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var d Describe
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("%w: /describe is not the describe document", ErrProtocol)
	}
	switch {
	case c.surface == "management" && d.Surface != "management",
		c.surface == "operations" && d.Surface != "" && d.Surface != "operations":
		return nil, fmt.Errorf("%w: /describe names surface %q, not %s", ErrProtocol, d.Surface, c.surface)
	}
	return &d, nil
}

// DescribeFile returns one served contract file, verbatim.
func (c *conn) DescribeFile(ctx context.Context, file string) ([]byte, error) {
	resp, err := c.do(ctx, http.MethodGet, "/describe/"+file, nil, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if t, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); t != "text/markdown" && t != "application/json" {
		return nil, fmt.Errorf("%w: %s served as %q", ErrProtocol, file, t)
	}
	return io.ReadAll(resp.Body)
}

func contains(names []Name, n Name) bool {
	for _, m := range names {
		if m == n {
			return true
		}
	}
	return false
}
