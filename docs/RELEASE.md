# Release and evidence

The library version, raw contract date (`2026-09-24`), administration contract
date (`2026-09-25`), Bytes backend profile version (`1`), and Data codec profile
(`deixis-codec-v2/identity-bytes@v0.4.0`) have separate identities. A library
release cannot silently freeze or replace the candidate codec profile.

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
checkout installs the packed TypeScript artifact and stores/loads a Data value.
CI runs formatting, Go vet/tests on Linux and Windows, Linux race tests,
TypeScript checking/build/tests, interchange and the packed consumer.

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
