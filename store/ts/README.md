# Bitstore for TypeScript and JavaScript

Exports `Bytes` (`Uint8Array`), addressless `Data`, `DataTree`, raw `Store`,
`MemoryStore`, HTTP `Client`, and the data codec and persistence functions. Works in modern
browsers and Node.js 22+ with WebCrypto, fetch and Web Streams. No runtime
dependencies or private package access.

```ts
import { bytesData, dataTree, read, MemoryStore, putDataTree, loadDataTree } from '@bitspark/bitstore';
const store = new MemoryStore();
const tree = dataTree(bytesData(new Uint8Array([7])), [
  [new Uint8Array(), dataTree(bytesData())],
]);
const root = await putDataTree(store, tree);
const loaded = await loadDataTree(store, root);
const child = loaded.at([new Uint8Array()]);
const bytes = await read(loaded, []); // Uint8Array([7])
```

The common structure is `DeixisNode<T>`: `own(): T`, complete byte-keyed
`children()`, `at(path: TreePath)`, and `decompose(): {own, children}`. A tree is
finite and acyclic. Keys are exact bytes; empty key, empty path and missing node
remain distinct. `at([])` returns the node itself; missing selection returns
`undefined`. `compose(own, children)` reconstructs a node from its parts.

```ts
interface Data { read(): Promise<Bytes>; }
type DataTree = DeixisNode<Data>;
// Bitwire uses the same structure with addressless Wire as its own value:
// interface Wire { send(message: Message): void; }
// type WireTree = DeixisNode<Wire>;
```

For an existing path, `read(tree, path)` is exactly
`select(tree, path).own().read()`. The derived read rejects with `path_not_found`
for a missing node; backing-store `not_found` errors propagate unchanged and
remain distinguishable. Successful derived reads return a copy of the bytes.
Construction checks for missing readers and children without performing reads.
Primitive read failures propagate unchanged. Data represents
fixed content: successful reads must return the same bytes. `bytesData(bytes)`
copies input and every result, providing an immutable materialized primitive.
Other implementations may read disk or remote storage behind this same API.
Their caller-visible content must still remain fixed.

Version 0.2.0 intentionally replaces the old bytes-owning `Data` class with the
addressless `Data` interface and exact `DataTree` alias. Construct trees with
`dataTree(bytesData(bytes), children)`; use `at([key, ...])` with one path array.
`encodeFlat` is now asynchronous, because persistence must read Data primitives.
Both codecs preserve the existing candidate profile, encoded bytes and content
addresses. Capability identity and runtime closures are never encoded.

`openDataTree` returns a separate `DataTreeView` for storage access: its children
are roots, and its asynchronous `at` fetches only selected chunks. This view is
not a `DeixisNode<Data>` and does not claim the full structural contract. Use
`loadDataTree` for a complete `DataTree` with synchronous structural operations.

Use `new Client(explicitOperationsURL)` for raw HTTP access, or
`await discoverBytes(explicitOperationsURL)` to require the versioned Bytes
advertisement first. Client `getStream` bytes are provisional until EOF passes
hash verification. WebCrypto requires buffering for hashing: the default
client budget is 64 MiB per read, batch or collected streamed upload;
`maxBytes` explicitly changes it. Raw ranges cannot be hash-verified.
`AbortSignal` is the last argument of I/O methods. This first TS SDK supplies
operations only; administration stays in the Go SDK.

`DataProfile` in Go / `dataProfile` here is a **candidate** codec profile.
Persist the whole `{profile,address}` root, not just a hash. See the repository's
`docs/DATA.md` and `docs/BYTES-PROFILE.md` for semantics and limits.

Run `npm ci --ignore-scripts`, `npm run check`, `npm run build`, `npm test`.
