# Migrating from v0.1.0 to v0.2.0

The native API changes; stored artifacts and the candidate profile do not.

| v0.1.0 | v0.2.0 |
| --- | --- |
| `Data` tree / `new Data(bytes, children)` | `DataTree` type / `dataTree(bytesData(bytes), children)` |
| Go `NewData(bytes, children...)` | `NewDataTree(BytesData(bytes), children...)` |
| `own()` / `Own()` returns bytes | Returns addressless `Data` reader; invoke `read()` / `Read(ctx)` |
| Concrete tree-only structure | Generic `DeixisNode<T>` with own, children, at, decompose |
| Variadic tree `at` / `At` | One typed array/slice of exact byte keys |
| `encodeFlat(tree)` synchronous | `await encodeFlat(tree)` |
| Go `EncodeFlat/EncodeLinked(tree, limits)` | `EncodeFlat/EncodeLinked(ctx, tree, limits)` |
| `DataLimits` | `DataTreeLimits` |
| `putData/loadData/openData`, `View` | `putDataTree/loadDataTree/openDataTree`, `DataTreeView` |
| Go `PutData/LoadData/OpenData`, `DataView` | `PutDataTree/LoadDataTree/OpenDataTree`, `DataTreeView` |
| Go concrete `Child` | Generic `Child[Data]` |

`Data` now exclusively means an addressless fixed-content reader. There is no
compatibility alias retaining the old meaning. `Bytes` remains the materialized
value. `DataTree = DeixisNode<Data>` and `WireTree = DeixisNode<Wire>` share one
complete structural contract; the addressless primitives differ only by their
operation (`read` or `send`). The dated raw Store contract is unchanged.

Use a reader per node for memory, disk or remote access. Pure composition does
not read. A successful reader must always return the same bytes, in a new
buffer; it can fail, and failure does not mean its structural path is missing.
Serializing a tree reads capabilities and serializes their byte results. It
does not serialize executable readers or their transport configuration.

Use a complete DataTree when child enumeration and decomposition are needed.
The separately named lazy DataTreeView exposes child addresses and asynchronous
fetching; it does not satisfy the generic structural interface. LoadDataTree
verifies all reachable content and returns a complete tree of byte-backed Data.

Persisted v0.1.0 roots use the same explicit
`deixis-codec-v2/identity-bytes@v0.4.0` candidate profile and retain their exact
addresses. Existing release tags and artifacts are immutable. Upgrade package
versions and source callers; do not re-encode merely to rename native types.
