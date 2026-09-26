# DataTree model and persistence profile

`Bytes` is a finite sequence of octets. `Data` is an addressless reader of
fixed bytes. `DataTree = DeixisNode<Data>` is a finite acyclic tree whose every
node has a mandatory own reader and a complete finite map from exact byte keys
to whole child nodes. The peer is `WireTree = DeixisNode<Wire>`; `Wire` is an
addressless sender. Deixis owns one structural contract for both.

```ts
interface Data { read(): Promise<Bytes>; }
type DataTree = DeixisNode<Data>;
interface Wire { send(message: Message): void; }
type WireTree = DeixisNode<Wire>;
```

The common node interface has `own()`, `children()`, `at(path)` and
`decompose()`. Composition receives one own payload and whole keyed children.
Selection at an empty path returns the current node; concatenated selection
is equivalent to successive selection. Decomposition followed by composition
preserves own payload and all child observations. Structure is finite and
acyclic; shared child objects denote repeated whole subtrees, not links in the
model. Child keys are exact byte strings, including empty and non-UTF-8 keys.
No normalization, slash splitting, parent pointer or mutable name is implied.

```text
read(tree, path)          = select(tree, path).own().read()
send(tree, path, message) = select(tree, path).own().send(message)
```

These equations assume an existing path. Missing paths have their own result;
an existing reader may fail without removing its node. Empty bytes are a value.
A path containing one empty key selects that child, distinct from an empty path.
All successful reads from a `Data` return the same content in independently
owned buffers. Byte-backed helpers enforce this; custom readers must honor it.

## Native presentations

Go defines `Data.Read(context.Context) (Bytes, error)` and the literal alias
`DataTree = DeixisNode[Data]`. Generic nodes expose `Own`, `Children`, `At(TreePath)`
and `Decompose`; `Compose` reconstructs them. `NewDataTree` takes a `Data` and
`Child[Data]` children. `BytesData` constructs an immutable reader from copied
bytes. A nil tree interface is invalid, not an empty leaf.

TypeScript defines `Data.read(): Promise<Bytes>` and the literal alias
`DataTree = DeixisNode<Data>`. Use `dataTree(bytesData(bytes), children)` to build
byte-backed trees, or pass any lawful reader to `dataTree`. `compose` and
`select` are generic. `at` takes one array of byte-key segments. `decompose`
returns `{own, children}`. The same structural interface accepts independent
implementations without class identity checks. Constructors copy keys and retain
payloads and child capabilities; they never invoke readers.

`Read(ctx, tree, path)` / `read(tree, path, signal?)` performs the derived
operation. Go passes cancellation into `Data.Read`. The exact TypeScript
primitive has no cancellation argument: derived operations check an optional
AbortSignal before and after awaits, while an individual custom reader owns
its I/O cancellation. A read failure is propagated without translating it into
structural absence.

`EncodeFlat(ctx, tree, limits)` / `await encodeFlat(tree, options, signal?)`
materializes readers and exchanges a self-contained byte snapshot. Linked
encoding and `PutDataTree` / `putDataTree` likewise serialize returned bytes,
never reader functions or references. Decoding reconstructs a tree of immutable
byte-backed readers. The byte grammar, codec profile and content addresses have
not changed. Encoding applies resource bounds and checks cancellation. A failed
materialization returns no artifact and performs no Store writes.

`PutDataTree` puts chunks leaves first and returns a root only after every
acknowledgement matches. A later put failure may leave immutable orphan chunks.
Publishing or replacing a root remains the caller's responsibility.

`OpenDataTree` / `openDataTree` verifies just the root and exposes a separate
`DataTreeView`: own bytes and child roots, with asynchronous path access. This
lazy storage view does **not** claim to implement the complete `DataTree`
interface. `LoadDataTree` / `loadDataTree` verifies the full closure and returns
that complete structure. Availability and integrity failures stay distinct from
missing child paths. Independent operations do not share a mutable cache.

## Identity and codec status

The exact profile is `deixis-codec-v2/identity-bytes@v0.4.0`, grounded in Deixis
commit `75a84d901396739cb9942afef50682540ec0857d`. The upstream codec remains a
**candidate**. This library release neither freezes it nor promises that future
profiles produce the same addresses. Persist the complete `{profile,address}`
root. Reject unknown profiles; migrate by explicit decode and re-encode.

Bitstore has no runtime or build dependency on the private Deixis repository.
This is a data-specific implementation of its model and candidate codec, not a
publication of that repository, a publication of a private Deixis implementation, or a clean-room claim.
The public generic interface and small structural construction helpers are
independent native presentations of Deixis-owned laws.

## Canonical identity-bytes encoding

All integers use shortest unsigned LEB128, at most ten octets and at most
`2^64-1`. All lengths count octets. Keys sort lexicographically by unsigned
octet, shorter prefix first; duplicates and unsorted input are invalid.
The slot codec id is two octets `00 01`. Every node has a payload, even empty.

```text
flat   = "dxf2" 02 00 01 node
node   = length(payload) payload child-count (length(key) key node)*
chunk  = "dxl2" 02 00 01 link-count hash32* linked-node
linked-node = length(payload) payload child-count (length(key) key link-index)*
```

There is one node per linked chunk. The links header contains distinct child
hashes in first-use order while scanning canonical child keys. Every listed
hash must be used; indices must be in range; first new indices are `0,1,...`.
The hash is SHA-256 of the **entire raw chunk**, including magic and codec id,
without an additional domain prefix. A storage name is `sha256:<lowercase hex>`;
a DataTree address is `dxl2:<lowercase hex>` paired with the exact profile. A flat
checksum is never a DataTree address. Equality of hashes is computational identity
under SHA-256, not proof that content is available or retained.

Readers validate complete framing before judging an otherwise well-formed id
unsupported or judging a child id different from its parent's. Id syntax also
recognizes public `00 + ordinal>=1`, private `01 + namespace16 + ordinal`, and
option `02 + inner-id`; total id length is 2..32. Reserved forms are opaque
and unsupported. Only `00 01` has a payload interpretation in this package.
The committed negative vectors exercise fault precedence and canonicality.
These APIs consume complete artifacts, so partial input is invalid rather than
a resumable incremental-parser result.

## Resource policy and error observations

`CodecError` separates `invalid`, `unsupported`, and `resource-refused`.
Resource refusal uses `limit_exceeded` and a named dimension. Missing chunks
use the raw Store's `not_found`; wrong hashes and lying upload acknowledgements
use `integrity`. Missing keys have an ordinary absence result.

Default limits match the candidate's portable floors: 4,096-byte keys, 16 MiB
payloads, 65,536 children/links per node, 32 MiB chunks, 64 MiB flat artifacts,
1,000,000 unique chunks, 1 GiB unique bytes, depth 256 (root depth zero),
16,777,216 unfolded nodes and 1 GiB unfolded flat bytes. Smaller application
limits explicitly select a restricted resource profile. Go zero fields select
defaults; TypeScript omitted fields select defaults and explicit values must
be positive safe integers. Parsing uses full-width u64 before policy checks.

Reconstruction fetches each shared chunk once while computing the full logical
size and depth, so sharing cannot hide exponential expansion. Reads have a
per-chunk cap before buffering. Partial materialization is not returned.
Lazy path access bounds path depth and each chunk, and makes no closure claim.

A wide node is one chunk: changing its own bytes, any key or any child rewrites
that node and each ancestor. The intended initial scale is trees within these
explicit bounds, not unlimited-width indexed collections. Applications needing
cheaper wide updates must choose a separately specified collection profile.
