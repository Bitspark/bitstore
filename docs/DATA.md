# Data model and persistence profile

`Bytes` is a finite sequence of octets. `Data = Deixis[Bytes]` is a finite
tree whose every node has mandatory own bytes and a finite map from exact byte
keys to whole child values. Keys need not be text. No normalization, slash
splitting, parent pointer, mutable name or application fact schema is implied.

Empty own bytes are a value. Empty path selects the current value. A path with
one empty key selects that child. Missing path is separate from an existing
empty leaf, unavailable storage, invalid encoding and corrupt content.
Reconstructing a value from its own bytes and whole children preserves it.
The native presentations are immutable and copy incoming and outgoing bytes.

## Native presentations

Go: `NewData`, `Own`, `Children`, `At`; zero `Data` is the empty leaf.
TypeScript: `new Data`, `own`, `children`, `at`. The TypeScript carrier is
`Uint8Array`; the Go carrier is `[]byte`. Child enumeration is byte-sorted.

`EncodeFlat` / `encodeFlat` exchanges one self-contained value. `EncodeLinked`
/ `encodeLinked` returns an explicitly qualified root and content-addressed
chunks. `PutData` / `putData` puts chunks leaves first and returns the root
only after every acknowledgement matches. Failure can leave immutable orphan
chunks. Keeping, publishing or replacing a root is the caller's responsibility.

`OpenData` / `openData` verifies just the root and exposes a `View`; `At` /
`at` fetches only the selected path, relative to the current view. Own values
and child roots can be observed without fetching siblings. `LoadData` /
`loadData` verifies the complete closure and reconstructs the value.
Independent operations do not share a mutable cache. Application cancellation
propagates to storage; synchronous in-memory encoding finishes its current work.

## Identity and codec status

The exact profile is `deixis-codec-v2/identity-bytes@v0.4.0`, grounded in Deixis
commit `75a84d901396739cb9942afef50682540ec0857d`. The upstream codec remains a
**candidate**. This library release neither freezes it nor promises that future
profiles produce the same addresses. Persist the complete `{profile,address}`
root. Reject unknown profiles; migrate by explicit decode and re-encode.

Bitstore has no runtime or build dependency on the private Deixis repository.
This is a data-specific implementation of its model and candidate codec, not a
publication of that repository, a generic Deixis core, or a clean-room claim.

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
a Data address is `dxl2:<lowercase hex>` paired with the exact profile. A flat
checksum is never a Data address. Equality of hashes is computational identity
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
