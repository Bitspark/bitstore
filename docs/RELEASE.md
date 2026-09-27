# Release and evidence

The library version, raw contract date (`2026-09-24`), administration contract
date (`2026-09-25`), Bytes backend profile version (`1`), and DataTree codec profile
(`deixis-codec-v2/identity-bytes@v0.4.0`) have separate identities. A library
release cannot silently freeze or replace the candidate codec profile.

## v0.2.1 construction fix

This patch release makes tree construction refuse input the structural contract
already forbade. Lawful trees, codecs, serialized bytes, qualified roots, the
raw contract and the codec profile are unchanged.

- `Compose`/`NewDataTree` (Go) and `compose`/`dataTree` (TypeScript) refuse a
  structural cycle reached through a child implemented outside the library, and
  missing children or duplicate keys inside such a child. They inspect it only
  through `children()`, never calling `own`, `at`, `decompose` or a reader.
  v0.2.0 accepted the cycle; it surfaced only when the tree was encoded.
- TypeScript construction refuses a key that is not a `Uint8Array`
  (`invalid_key`). v0.2.0 converted it, so the string `"a"` became the empty
  key. A plain number array such as `[97]` converted correctly and is now
  refused too; pass `Uint8Array.of(97)`.

bitwire's independently authored structural cases, which CI now runs against
DataTree, found the cycle: v0.2.0 failed `cycle-refused` in Go and TypeScript.
At this release the cases are pinned at bitwire `a13d0f8`. DataTree passes all
20 structure cases in both languages, and every deliberately unlawful
realization fails its case: 13 in Go, 14 in TypeScript. See
[TREES.md](https://github.com/Bitspark/bitstore/blob/v0.2.1/docs/TREES.md).

## v0.2.0 API migration

This breaking native release adopts `Data` for addressless reading and the
literal `DataTree = DeixisNode<Data>` specialization, symmetric with Bitwire's
`Wire` / `WireTree`. Both expose the complete Deixis structural contract.
Old `Data` tree aliases are removed. Encoders materialize fixed-content readers
with explicit failure/cancellation; serialized bytes and qualified addresses
remain identical to v0.1.0. No published release or codec profile is rewritten.

New tests exercise pure composition without reads, complete reconstruction and
selection, independent structural implementations, reader failure distinct from
missing nodes, immutable byte buffers, cycle refusal and cancellation before
persistence. The existing golden flat/linked artifacts remain unchanged.

## Validation

The v0.1.0 implementation is held to the extracted contract-authored raw
conformance suite and newly hand-authored data grammar fixtures. Negative
fixtures check truncated, non-shortest and overflowing integers, exact-key
ordering, bounded slot ids, unsupported/framing precedence, linked hash-table
canonicality and child codec mismatch. Store doubles exercise corrupt reads,
false upload acknowledgements and missing chunks; resource cases count logical
expansion of shared graphs. Cancellation and interrupted uploads commit nothing.

Go/TypeScript interchange compares complete canonical chunks and roots for the
fixed grammar corpus and twelve constructed trees. A fresh process outside the
checkout installs the packed TypeScript artifact and stores/loads a DataTree value.
CI runs formatting, Go vet/tests on Linux and Windows, Linux race tests,
TypeScript checking/build/tests, interchange and the packed consumer. After
v0.2.0 it also runs bitwire's pinned structural cases against DataTree in both
languages ([TREES.md](TREES.md)).

Local Windows Go 1.26.3 ordinary tests and vet pass. Its race runtime currently
fails before tests with a ThreadSanitizer address-allocation error; Linux is the
race-test platform. This is not reported as a passing Windows race run.

## Publishing

After a reviewed PR and passing applicable checks land, tag `vX.Y.Z` on main.
The immutable tag identifies the root Go module. The release workflow checks
the tag against `store/ts/package.json`, builds and tests, packs the public
TypeScript package, writes SHA-256 checksums, and publishes both assets on the
GitHub release. The package installs without private repositories or credentials.
Verify Go installation from the public tag and installation from the uploaded
tarball in fresh external consumers before reporting delivery.

An npmjs publication requires configured registry authority (a publish token or
trusted publisher). This repository initially ships the exact npm-compatible
tarball on GitHub; it does not claim an npmjs registry entry. Never replace an
existing version or mutate a release tag to repair a failed publication.
