# The contract

**Contract:** 2026-09-24, the day this surface last changed. Every response
of every binding carries it; a change to what this file means moves it, in
this file and in each binding, in the same commit.

Nine verbs, two refusals, one name grammar. This file is the contract. A
binding ([HTTP.md](HTTP.md)) carries it and adds nothing; a binding's
operational routes are the binding's, not the contract's.

## Names

```
sha256:<64 lowercase hexadecimal digits>
```

A name is the SHA-256 digest of the bytes, prefixed by the algorithm so that a
future algorithm is visible rather than silent. The same bytes have the same
name in every store that speaks this grammar. Names compare as strings; the
grammar is the only canonical form. A string outside the grammar is not a
name: a binding rejects it as a protocol error, not as a refusal of content,
so it does not appear among the refusals.

The guarantee is about bytes and nothing more. A name says *which* bytes. It
does not say what they mean, whether they are here, or that anyone keeps them.

## Verbs

| verb | in | out |
|---|---|---|
| `get` | name | the bytes |
| `get many` | names | blobs, keyed by name, unordered |
| `has` | names | which of them are here |
| `size` | names | byte count per name that is here |
| `get range` | name, offset, length | the overlapping bytes |
| `get stream` | name | the bytes, incrementally |
| `put` | bytes | name |
| `put many` | list of bytes | names, per item |
| `put stream` | bytes, incrementally | name |

`has` and `size` are **plural**. A singular existence check pays one round
trip per blob, and the client that asks is usually deciding, for a set of
blobs it has just learned about, which to fetch whole and which to stream. A
plural `size` subsumes `has` — a name with a size is here, a name without one
is not — so a binding may carry both verbs on one operation. The singular case
is a set of one.

## Pinned behaviors

**`put stream` commits atomically, at clean end-of-stream, under the hash of
exactly what arrived.** The server only knows the hash of what it received,
and those bytes verify themselves, so integrity under the full content's name
cannot be violated. What this clause forbids is *prefix commit*: an aborted
stream's prefix also verifies itself, and committing it under `hash(prefix)`
would make a partial object visible under a name. Any abort — the client
disconnecting, or the server's size cap tripping mid-stream — commits nothing.
Content already present deduplicates at commit time; that short-circuit is the
server's and happens only at commit, unlike skipping a `put` when `has`
already says yes, which is the SDK's.

**`put many` is per item, not atomic across the batch.** Each item commits or
refuses on its own; an oversized item refuses alone and its neighbors land.
Because batches land partially, a writer of linked structures writes leaves
first, so that every stored node's children are already stored. Retries re-put
and deduplication absorbs them.

**`get range` past the end is a short read.** The store returns the overlap
of the requested range with the blob; a range entirely past the end overlaps
nothing and returns zero bytes. A refusal would be legal — size is structural,
not semantic — but it would add a third refusal. A range is the only read that
verify-on-read cannot check; immutability makes `size` a reliable clamp, and a
client that needs verification fetches whole or streams.

**`get many` is unordered, keyed by name.** Ordering is most of what the plural
form would otherwise cost. Keying is free, since verify-on-read hashes every
blob anyway, and it handles duplicate names and per-item absence: a name that
is not here is absent from the result, not an error, and a name requested
twice appears once.

**`get stream` cancellation is nothing, server side.** The client stops
reading; the server stops sending. No state, no verb, no cleanup.

## Refusals

Two, neither about content:

| refusal | meaning |
|---|---|
| `too large` | the bytes exceed what this deployment accepts |
| `not found` | the name asked for is not here |

There is no "malformed": that is a judgement about a format, and there is no
format here to judge against.

`not found` is a refusal only where one blob's bytes were demanded — `get`,
`get range`, `get stream`. In the plural verbs absence is data, an omitted
key, because a batch that fails on its most absent member is useless for the
negotiation the plural verbs exist to serve.

## Persistence, cancellation and uncertain outcomes

An acknowledged `put`, `put stream` or `put many` item is durable before it is
acknowledged: the bytes are written and flushed to stable storage and
published under their name in one atomic step. It survives a crash of the
service and a restart with the same data. How far that extends — power loss,
platform limits — is a property of the implementation, stated for the shipped
store in [CONSISTENCY.md](CONSISTENCY.md).

Reads never return bytes that were not committed: a name is either absent or
names the complete blob. A crash mid-upload leaves nothing any verb can
reach.

A caller that cancels, times out or loses the connection after sending bytes
learns nothing about the outcome: the request committed the whole blob or
nothing, never part of one. An acknowledgement can be lost after a commit, so
an error after dispatch is not proof that the blob is absent. Because names are
content-derived and `put` is idempotent, the caller resolves the uncertainty by
asking `has` for the expected name or by sending the same bytes again; both are
safe.

## What the store does not do

No deletion, garbage collection, pinning, replication, access control, type
interpretation or link following. The store never parses what it holds, so it
cannot know what is reachable; any future reclamation must be driven by
whoever holds the roots. A name held somewhere else proves neither that the
blob is available nor that it is retained, so a client keeps the store it
wrote to beside the names it wrote.

Authentication is not part of this contract. A deployment decides who can
reach a listener.

## What the SDK adds, off the wire

Verify-on-read, skipping a `put` when `has` already says yes, caches,
parallel fetch and failover across mirrors. These live in the client so that
the storage behind it does not have to be trusted — only available. Because a
client that hashes what it receives need not trust where it came from, a
mirror, a cache or a peer is as good as the origin. Nothing on the wire knows
about any of them.
