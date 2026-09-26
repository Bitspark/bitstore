# The management HTTP binding

One binding of [the management surface](SURFACE.md), on a listener of its own,
configured independently of the operations listener
([the operations binding](../HTTP.md)). The two never share a listener, and a
failure to bind either fails startup.

Every response of this listener — operational, success and refusal alike —
carries three headers:

```
Bit-Service: bitstore
Bit-Surface: management
Bit-Contract: 2026-09-25
```

`Bit-Contract` is the date on the contract line of [SURFACE.md](SURFACE.md),
independent of the operations contract's. A client of this surface treats a
response without `Bit-Service: bitstore` and `Bit-Surface: management` as a
protocol error, before reading its body: an operations listener, or anything
else, answered.

## Routes

There are no application routes yet; `/v1` is reserved for administration
verbs this surface may add. The four operational routes, at the root:

| route | answers | status |
|---|---|---|
| `GET /livez` | the process is up | `200` whenever it answers |
| `GET /healthz` | this listener can serve its contract | `200`, or `503` with the failing check |
| `GET /describe` | this surface's description | `200` |
| `GET /describe/{file...}` | one file `/describe` lists, byte for byte | `200`, `404 not_found` otherwise |

`/livez` checks nothing and answers an empty `checks` array. `/healthz` answers
one check, `listener`: the management contract needs no dependency beyond the
listener answering, so readiness reads no store and scans nothing.

`/describe` answers the family's describe document with `surface` set to
`management`: `bindings` is `{"primary": "http", "http": {"prefix": "/v1"}}`,
`operations` and `refusals` are those of [operations.json](operations.json),
and `files` lists `SURFACE.md`, `HTTP.md`, `operations.json`, `openapi.json`
and every `schema/<Type>.schema.json` of this directory — named relative to
it, so this listener's `/describe/SURFACE.md` is this surface's, and no file
of the operations contract is served here. `build` is the same executable's
build the operations listener reports.

## Validation and errors

A path that is not its own clean form, an undeclared route, and every
operations route are `404 not_found`, never redirected; a declared route with
an undeclared method is `400 bad_request` with an `Allow` header. Errors are
the family's envelope, `application/json`:

```json
{"error": "not_found", "message": "no such route"}
```

| status | error | where |
|---|---|---|
| `404` | `not_found` | an unclean path, an undeclared route, an unlisted file |
| `400` | `bad_request` | an undeclared method |
| `500` | `internal` | the server failed |
