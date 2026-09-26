# Bitstore

The public library and contract for bytes, structured data, and content-addressed
storage.

| Public type | Role |
| --- | --- |
| `Bytes` | A finite byte sequence |
| `Data` | `Deixis[Bytes]`: own bytes and a finite map of exact byte keys to child Data |
| `Store` | Raw content-addressed persistence |

The model name **Bitdata** denotes `Data`. It is the data counterpart of
Bitwire's addressed `Wire` over the addressless `End` primitive.

Go and TypeScript implement the model, canonical flat and linked codecs, lazy
verified reads, bounded reconstruction, raw Store interfaces, memory backends,
HTTP clients and explicit Bytes backend discovery. The deployed service and
filesystem backend remain in private `bitstore-svc`.

The first release is **v0.1.0**. Its data codec is an explicitly pinned
**candidate** profile, not a frozen identity standard. Save the complete
`{profile,address}` root. Raw blob names and the dated raw contract are unchanged.

```sh
go get github.com/Bitspark/bitstore@v0.1.0
npm install https://github.com/Bitspark/bitstore/releases/download/v0.1.0/bitspark-bitstore-0.1.0.tgz
```

The npm-compatible artifact is distributed by the GitHub release. An npmjs
registry publication is a separate delivery and is not claimed here.

- [Go usage](store/go/README.md) and [TypeScript usage](store/ts/README.md)
- [Model, codec profile and limits](docs/DATA.md)
- [Raw Store contract](docs/raw/SURFACE.md) and [HTTP binding](docs/raw/HTTP.md)
- [Bytes backend discovery](docs/BYTES-PROFILE.md)
- [Reusable conformance](conformance/go/README.md) and [release evidence](docs/RELEASE.md)

Read [CHARTER.md](CHARTER.md) for ownership and [LAYOUT.md](LAYOUT.md) for source
organization.
