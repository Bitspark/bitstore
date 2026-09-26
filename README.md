# Bitstore

The public library and contract for bytes, structured data, and content-addressed
storage.

| Public type | Role |
| --- | --- |
| `Bytes` | A finite byte sequence |
| `Data` | Addressless reading of fixed bytes |
| `DataTree` | `DeixisNode<Data>`: own reader and exact byte-keyed whole children |
| `Store` | Raw content-addressed persistence |

The family uses **Data / DataTree** and **Wire / WireTree**. `Wire` sends a
message without a path; `Data` reads bytes without a path. Both trees have the
same complete generic structure: own value, children, path selection and
decomposition. Addressing belongs to the tree. Deixis owns the structural laws.

```text
read(tree, path)          = select(tree, path).own().read()
send(tree, path, message) = select(tree, path).own().send(message)
```

Missing paths are structural absence, separate from read failures or message
refusals at existing nodes. Byte keys include empty and non-UTF-8 values.

Go and TypeScript implement the model, canonical flat and linked codecs, lazy
verified reads, bounded reconstruction, raw Store interfaces, memory backends,
HTTP clients and explicit Bytes backend discovery. The deployed service and
filesystem backend remain in private `bitstore-svc`.

The current native API is **v0.2.0**, a breaking replacement of v0.1.0's
`Data` tree class with the primitive and explicit `DataTree`. Its data codec is an explicitly pinned
**candidate** profile, not a frozen identity standard. Save the complete
`{profile,address}` root. Raw blob names and the dated raw contract are unchanged.

```sh
go get github.com/Bitspark/bitstore@v0.2.0
npm install https://github.com/Bitspark/bitstore/releases/download/v0.2.0/bitspark-bitstore-0.2.0.tgz
```

The npm-compatible artifact is distributed by the GitHub release. An npmjs
registry publication is a separate delivery and is not claimed here.

- [Go usage](store/go/README.md) and [TypeScript usage](store/ts/README.md)
- [Breaking API migration](docs/MIGRATION-0.2.md)
- [Model, codec profile and limits](docs/DATA.md)
- [Raw Store contract](docs/raw/SURFACE.md) and [HTTP binding](docs/raw/HTTP.md)
- [Bytes backend discovery](docs/BYTES-PROFILE.md)
- [Reusable conformance](conformance/go/README.md) and [release evidence](docs/RELEASE.md)

Read [CHARTER.md](CHARTER.md) for ownership and [LAYOUT.md](LAYOUT.md) for source
organization.
