# The HTTP binding

One binding of [the surface](SURFACE.md). It carries the nine verbs on five
routes and adds nothing to them.

Application routes live under `/v1`. The version belongs to this binding, not
to the service: the contract is worth something because it does not change,
and the prefix is what lets that claim survive being wrong. Every response —
application, operational and error alike — carries the family's two headers:

```
Bit-Service: bitstore
Bit-Contract: 2026-09-24
```

`Bit-Contract` is the date on the contract line of [SURFACE.md](SURFACE.md).
A client treats a response whose `Bit-Service` is missing or names another
service as a protocol error, before reading the body and whatever the status:
whatever answered is not a bitstore — a proxy's error page, another service
behind the same address. A served `Bit-Contract` other than the client's own is
reported, not refused.

This binding follows the
[bit-services standard](https://github.com/Bitspark/bit-services-contract/blob/main/standard/README.md)
of 2026-09-24; its table is [operations.json](operations.json).

## Routes

| route | verbs |
|---|---|
| `GET /v1/blobs/{name}` | `get`, `get range`, `get stream` |
| `POST /v1/stat` | `has`, `size` |
| `POST /v1/blobs` | `put`, `put stream` |
| `POST /v1/get` | `get many` |
| `POST /v1/put` | `put many` |

### `GET /v1/blobs/{name}` — get, get range, get stream

Returns the bytes: `200`, `Content-Type: application/octet-stream`, with
`Content-Length` when known. Whether the client treats the body as a whole
blob or a stream is the client's business; the wire is identical.

The query parameters `offset` and `length`, non-negative decimal integers,
select a range. As the contract pins it, the response is the overlap: `length`
is clamped to the end, and an `offset` at or past the end yields an empty
`200`. The HTTP `Range` header is not part of this binding; its
unsatisfiable-range answer, `416`, would smuggle in a third refusal.

Cancellation is the client closing the connection. The server sends nothing
further and keeps nothing.

Errors: `404 not_found` if the name is not here.

### `POST /v1/stat` — has, size

Request, `application/json`:

```json
{"names": ["sha256:…", "sha256:…"]}
```

Response, `200 application/json`:

```json
{"sizes": {"sha256:…": 1024}}
```

`sizes` holds an entry for every requested name that is here and none for a
name that is not. The one operation is both plural verbs: the presence of a key
is `has`, its value is `size`.

### `POST /v1/blobs` — put, put stream

The request body is the bytes, raw. The server hashes what arrives and commits
at clean end of body. For a streamed (chunked) request this is the atomic
commit the contract pins: an incomplete body, whoever aborted it, commits
nothing. Content already present short-circuits at commit time; the response
does not distinguish that case, because the verb is idempotent and the
distinction is not the client's business.

Response, `200 application/json`:

```json
{"name": "sha256:…", "size": 1024}
```

Errors: `413 too_large` if the bytes exceed the deployment's cap, including
when the cap trips mid-stream, in which case nothing was committed.

### `POST /v1/get` — get many

Request, `application/json`:

```json
{"names": ["sha256:…", "sha256:…"]}
```

Response, `200 application/x-bitstore-blobs`: a sequence of records, one per
requested blob that is here, in any order. Each record is an ASCII header line

```
<name> <size>\n
```

followed by exactly `<size>` raw bytes. The sequence ends at end of body.
Requested names that are not here produce no record; a name requested twice
produces one. Records are written one blob at a time as each is read; the
response is never held whole.

### `POST /v1/put` — put many

Request, `Content-Type: application/x-bitstore-blobs` (any other is
`400 bad_request`), with anonymous records: the header line is just
`<size>\n`, since the name is what the server is asked to compute, and it is
followed by exactly `<size>` raw bytes. A size is decimal digits with no sign
and no leading zero (`0` itself excepted), and a header line is at most 128
bytes, newline included.

Response, `200 application/json`, one entry per record **in request order** —
the items have no names until now, so order is the only key there is:

```json
{"results": [
  {"name": "sha256:…", "size": 3},
  {"error": "too_large", "message": "the body exceeds the per-blob limit"}
]}
```

Each item commits independently, as the contract pins it: an oversized item
refuses alone, its bytes skipped, and its neighbors land; a batch cut off
mid-body commits the records that arrived whole and nothing of the one that did
not. The server reads one record and writes its result at a time, so neither
the batch nor its results are ever held whole; the `200` is sent before the
first record is read. A framing error — a header that is not a size, or one
longer than 128 bytes — and a body that ends inside a record each end the
results with one `bad_request` entry, where the client is still reading.

## Operational routes

The four routes of the standard's
[operational profile](https://github.com/Bitspark/bit-services-contract/blob/main/standard/OPERATIONS.md),
at the root, unversioned, outside the surface and without credentials. Each is
a row of kind `operational` in [operations.json](operations.json).

| route | answers | status |
|---|---|---|
| `GET /livez` | the process is up | `200` whenever it answers |
| `GET /healthz` | the store can serve: its data directory exists and a temporary file can be created where commits stage | `200`, or `503` with the failing check |
| `GET /describe` | the service's description | `200` |
| `GET /describe/{file...}` | one file `/describe` lists, byte for byte | `200`, `404 not_found` otherwise |

`/livez` and `/healthz` answer the family's health envelope, with one check,
`store`, on `/healthz` and none on `/livez`:

```json
{"status": "ok", "service": "bitstore", "contract": "2026-09-24",
 "build": {"revision": "948bc51e0c2f4a7d3e9b8a6f1c5d2e4b7a9c0f13", "dirty": false, "go": "go1.26.3"},
 "time": "2026-09-24T09:00:00Z",
 "checks": [{"name": "store", "status": "ok", "detail": "data directory writable"}]}
```

`build` comes from the executable's build information or, when the build
carried none, from the link-time variables
`github.com/Bitspark/bitstore-svc/server.Revision` and
`github.com/Bitspark/bitstore-svc/server.Dirty`; it is `"unknown"` with `dirty`
true only when neither exists. A store has no instance identity, so neither the
envelope nor the description carries one.

`/describe` answers the family's describe document: `bindings` is
`{"primary": "http", "http": {"prefix": "/v1"}}`; `operations` and `refusals`
are those of [operations.json](operations.json), unchanged; `limits` carries
`max_blob_bytes`, the deployment's per-blob cap, or `0` for none; and `files`
lists `SURFACE.md`, `HTTP.md`, `CONSISTENCY.md`, `operations.json`, `openapi.json` and every
`schema/<Type>.schema.json`, each served byte for byte by
`/describe/{file...}` as `text/markdown` or `application/json`. The repository
holds the embedded copies equal to the committed ones.

## Validation

The binding refuses what it does not document. On `GET /v1/blobs/{name}` any
query parameter other than `offset` and `length`, a repeated one or an empty
one is `400 bad_request`. A JSON body for `/v1/stat` or `/v1/get` is one object
whose only member is `names`, given once, followed by nothing but whitespace;
an unknown or repeated member, trailing data or a `names` that is not an array
of names is `400 bad_request`. The `POST` routes take no query parameter; any
is `400 bad_request`. A client that misspells a parameter hears about it
instead of receiving the whole blob.

A path that is not its own clean form — containing `.`, `..` or an empty
segment — is `404 not_found` and is never redirected: a client that followed a
redirect would be served for a URL this binding never named. An undeclared
route is `404 not_found`; a declared route with an undeclared method is
`400 bad_request` with an `Allow` header. Neither is a refusal of content.

## Limits

| what | bound |
|---|---|
| a JSON request body | 4 MiB, else `400 bad_request` |
| a batch record's header line | 128 bytes, newline included |
| a blob | the deployment's cap, `limits.max_blob_bytes` in `/describe` (0 for none), else `413 too_large` |
| a size, an offset, a length | a decimal integer below 2^63; sizes in JSON are exact integers, so a cap is below 2^53 |
| an error body | the envelope, with a short fixed message |
| a batch | no item-count cap; memory is bounded by one blob and one record header, and a response grows by one record or one result per item |

Streaming limits, the per-blob cap and these metadata limits are separate; a
`stat` response is bounded by its request.

## Errors

Every error carries `application/json` and the envelope:

```json
{"error": "not_found", "message": "no such object or route"}
```

| status | error | where |
|---|---|---|
| `404` | `not_found` | `GET /v1/blobs/{name}`; an unclean path or undeclared route |
| `413` | `too_large` | `POST /v1/blobs`; per item in `POST /v1/put` |
| `400` | `bad_request` | a malformed name, parameter or body; an undeclared method — protocol errors of this binding, not refusals of content |
| `500` | `internal` | the server failed |

`not_found` and `too_large` are the contract's refusals. `5xx` means the server
failed, not that it refused; a client retries or fails over, which content
addressing makes safe.
