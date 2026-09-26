# Bitstore for TypeScript and JavaScript

Exports `Bytes` (`Uint8Array`), immutable `Data`, raw `Store`, `MemoryStore`,
HTTP `Client`, and the data codec and persistence functions. Works in modern
browsers and Node.js 22+ with WebCrypto, fetch and Web Streams. No runtime
dependencies or private package access.

```ts
import { Data, MemoryStore, putData, openData } from '@bitspark/bitstore';
const store = new MemoryStore();
const data = new Data(new Uint8Array([7]), [[new Uint8Array(), new Data()]]);
const root = await putData(store, data);
const view = await openData(store, root);
const child = await view.at([new Uint8Array()]);
```

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
