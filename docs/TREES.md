# Shared structural evidence for DataTree

`DataTree = DeixisNode<Data>` and bitwire's `WireTree = DeixisNode<Wire>` claim
one structural contract ([bitwire decision 0012](https://github.com/Bitspark/bitwire/blob/a13d0f89ccb015f22efc64b39e56cd42333ebe67/docs/decisions/0012-explicit-data-and-wire-trees.md)).
This repository's CI holds the DataTree in this checkout to the same
independently authored cases that bitwire holds WireTree to, in Go and
TypeScript, on every pull request.

```console
node scripts/trees.mjs              # offline: verify the pin, build, run
node scripts/trees.mjs --upstream   # also check the pin against GitHub
```

The script needs Go, Node 24 and `npm ci` in `store/ts`. It builds the
TypeScript package and a Go driver from this checkout's source.

## Evidence decision

The evidence is the `structure` family of bitwire's
[`conformance/wiretree/cases.json`](https://github.com/Bitspark/bitwire/blob/a13d0f89ccb015f22efc64b39e56cd42333ebe67/conformance/wiretree/cases.json),
pinned at an immutable commit and run here against the source being changed.

- **Why this case set.** Its expectations were written from the contract, not
  recorded from an implementation. The runner withholds them from drivers and
  gives drivers keys only as hex. It covers the observations of bitwire's older
  `conformance/trees` set, which keeps its tree shape in driver code, and adds
  construction faults, reconstruction cuts, edits and shared state.
- **Why here.** [bittheory TREE-001](https://github.com/Bitspark/bittheory/pull/12)
  checks released versions, so a regression in an unreleased change here would
  reach it only after release. TREE-001 remains released-version evidence.
- **One case set.** The case file and bitwire's comparison library are copied
  byte-identical, never edited here. [`SOURCE.json`](../vectors/bitwire/wiretree/SOURCE.json)
  records their commit, git blobs and SHA-256 digests; the script refuses to run
  if either differs. Changes to the cases belong in bitwire.

| Pin | Value |
| --- | --- |
| Repository | [Bitspark/bitwire](https://github.com/Bitspark/bitwire) |
| Commit | `a13d0f89ccb015f22efc64b39e56cd42333ebe67` (`cases.json` last changed here, `wiretree-lib.mjs` in `e8cc923`) |
| `conformance/wiretree/cases.json` | sha256 `32f0caf66f69066259d038bcfa50777ace2d7621f67d8bd6a875e09653ddf221` |
| `scripts/wiretree-lib.mjs` | sha256 `7ce39ae3db4322aa6a1b1bb109539d6039229ef5437270681e3c8992b61acce0` |

## From Wire to Data

The cases are written for `Wire`. The drivers
([Go](../conformance/go/trees/main.go), [TypeScript](../conformance/ts/trees.mjs))
give each named primitive a driver-instrumented `Data` reader and emit the
cases' own vocabulary, so the oracle is compared unmodified.

| Case vocabulary | For DataTree |
| --- | --- |
| step `send` | The derived read `read(tree, path)` (Go `Read`) |
| `["delivered", name, n]` | The named reader was invoked and read. `n` counts that reader instance's reads. |
| own `null`, `["refused"]` | A present node whose reader fails. Its reader was invoked and failed. |
| `["missing"]` | Selection found no node, no reader ran, and the derived read reported structural absence (`path_not_found`, `ErrPathNotFound`). |
| `unchanged` | Every derived read gave its caller exactly the bytes its reader produced, in a buffer the reader does not share, and every failed read surfaced the reader's own error. |
| `partsExact` | Every construction kept exactly its own reader and children after the caller's input and the returned parts were altered. |
| `same`, rendered `own` | Identity of the own reader. |

Any other outcome is recorded under its own name and fails the case:
`fallback`, `missingAdmitted`, `missingMisreported`, `failureSwallowed`,
`failedAfterRead`, `readWithoutOwn`, `refusedWithoutOwn`, `readMoreThanOnce`,
`selectionDiffers`.

Each reader invocation returns bytes unique to that invocation. The driver keeps
its own copy and compares what the caller received with it. It never compares
against the object under test. (bitwire's WireTree driver made that mistake,
[bitwire#65](https://github.com/Bitspark/bitwire/issues/65).)

**Excluded families.** The 1 `bridge` and 8 `carrier` cases are not run. The
bridge maps a WireTree onto bitwire's addressed carrier paths; the carrier cases
compose trees across bitruntime carriers and dispatchers. Neither has a
counterpart in this library. Remote data trees belong to service integration
([bitstore-svc#13](https://github.com/Bitspark/bitstore-svc/issues/13)).

## Covered observations

The 20 structure cases cover:

- complete children, including the empty key;
- exact keys: binary `ff`, U+FFFD, the literal `a/b`, both spellings of é, and
  `a` beside U+FEFF `a` (bytes `ef bb bf 61`), where `ef bb bf` alone names no child;
- empty, staged and missing selection, and chains that fail part-way;
- own reader identity and shared children;
- key copying on construction, `children()` and `decompose()`;
- both reconstruction directions and complete cuts;
- fresh and copied readers;
- edits: omit, rename, add, replace;
- a child implemented outside the library;
- refusal of duplicate keys, missing children and cycles;
- missing structure kept distinct from a present failing reader.

A reader invoked by construction, selection, rendering or reconstruction would
appear in the trace, so structural operations are shown not to read.

## Unlawful realizations are rejected

Each driver also runs deliberately unlawful realizations. The script requires
each to fail the case aimed at it.

| Realization | Violation | Must fail |
| --- | --- | --- |
| `fallback` | A missing descendant is read from its deepest present ancestor. | `missing-never-falls-back` |
| `fabricating` | A missing path selects a fabricated node whose reader fails. | `refusing-versus-missing` |
| `empty-for-missing` | A missing path reads as empty content. | `missing-never-falls-back` |
| `missing-as-failure` | A missing path is reported as a failed read. | `refusing-versus-missing` |
| `swallowing` | A reader's failure is reported as empty content. | `own-and-descendants` |
| `corrupting` | The derived read alters its bytes. | `own-and-descendants` |
| `aliasing` | The derived read hands out the reader's own buffer. | `own-and-descendants` |
| `failing-after-read` | The derived read fails after its reader succeeded. | `own-and-descendants` |
| `wrapping-own` | The own reader is replaced by a forwarding wrapper. | `root-cut-reconstruction` |
| `incomplete-children` | `children()` omits the empty key. | `own-and-descendants` |
| `normalizing` (TypeScript) | UTF-8 keys are NFC-normalized. | `own-and-descendants` |
| `lossy-keys` | Keys pass through lossy text decoding. | `own-and-descendants` |
| `cycle-accepting` | Construction never looks for a cycle through a foreign child. | `cycle-refused` |
| `bom-stripping` | A leading U+FEFF is stripped from keys, as a byte order mark. | `keys-are-exact-bytes` |

Go has no standard-library NFC normalization, so `normalizing` runs in
TypeScript only.

**Gaps.** The script also keeps a list of unlawful realizations that the
pinned cases cannot detect. Each must pass every case, so a re-pin that starts
rejecting one fails until it moves to the table above. There are none at this
pin. At the previous pin, `3b237aa` (19 structure cases), `bom-stripping`
passed every case because no key began with U+FEFF. bitwire added
`keys-are-exact-bytes` for it ([bitwire#65](https://github.com/Bitspark/bitwire/issues/65),
[#67](https://github.com/Bitspark/bitwire/pull/67)).

## What the cases found

Released v0.2.0 failed `cycle-refused` in both languages. Its construction
checked keys and missing children but accepted a child implemented outside the
library that contains itself. Its documentation called acyclic children a caller
precondition; the contract says construction refuses cycles. All other 18
structure cases passed.

Construction now walks the complete `children()` graph of children implemented
outside the library, allowing shared children. It refuses cycles, missing
children and duplicate keys (`ErrInvalidTree`/`ErrInvalidDataTree`,
`cyclic_tree`). Nodes the library built itself were validated when they were
built. Construction never calls `own`, `at`, `decompose` or a reader.
TypeScript construction also refuses a key that is not a `Uint8Array`
(`invalid_key`), rather than converting it: before, the string `"a"` became the
empty key. These fixes shipped in v0.2.1.

## Limits

- Only Go and TypeScript run.
- The drivers are test-only. They exercise public construction, selection and
  derived reading, not the codecs, decoded snapshots or `DataTreeView`, which
  have their own fixtures ([DATA.md](DATA.md)).
- Schedules are serial.
- Fault injection (the duplicate key, missing child, cycle and foreign child) is
  defined by the drivers, as in bitwire, because the case notation names only the fault.
- In Go, a cycle is found by node identity. A non-comparable node
  implementation must itself be finite, as in bitruntime.
- A child implemented outside the library that changes its `children()` after
  being validated is outside the contract. Construction does not detect it.

## Re-pinning

1. Copy both files from a newer bitwire commit.
2. Update `SOURCE.json`: commit, git blobs and digests.
3. Run `node scripts/trees.mjs --upstream`.
4. Record the new revision here and in bitwire's
   [language matrix](https://github.com/Bitspark/bitwire/blob/main/docs/languages.md).

A changed case that fails against this library is either a defect here or a
question for bitwire's owners. Never a local edit to the copy.
