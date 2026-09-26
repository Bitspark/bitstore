# Bitstore for Go

Import `github.com/Bitspark/bitstore/store/go` as `bitstore`. The root module
contains the public contract, SDK and conformance suite; it has no private or
runtime module dependencies. `Bytes` is `[]byte`, `Data` is an addressless fixed-content reader,
`DataTree = DeixisNode[Data]` is its complete immutable structure, and `Store`
supplies nine raw blob verbs. `WireTree = DeixisNode[Wire]` has the same
structure; only the own primitive differs.

```go
leaf, _ := bitstore.NewDataTree(bitstore.BytesData(nil))
value, _ := bitstore.NewDataTree(bitstore.BytesData([]byte("own")), bitstore.Child[bitstore.Data]{Key: nil, Tree: leaf})
payload, children := value.Decompose()
rebuilt, err := bitstore.Compose[bitstore.Data](payload, children...)
bytes, err := bitstore.Read(ctx, rebuilt, bitstore.TreePath{nil})
// bytes is the empty child payload; Read calls selected.Own().Read(ctx).
store := bitstore.NewMemory(0)
root, err := bitstore.PutDataTree(ctx, store, value, bitstore.DataTreeLimits{})
// Check err before using root.
view, err := bitstore.OpenDataTree(ctx, store, root, bitstore.DataTreeLimits{})
child, present, err := view.At(ctx, nil) // one empty key, not the empty path
```

For HTTP, `New(explicitOperationsURL)` returns a raw `Client`;
`DiscoverBytes(ctx, explicitOperationsURL)` additionally requires the compatible
advertised profile. `NewManagement(explicitManagementURL)` configures the other
surface independently. Prefixes are preserved and redirects refused.
`GetStream` verifies at EOF; bytes before successful EOF are provisional.

Match raw refusals with `errors.Is`: `ErrNotFound`, `ErrTooLarge`,
`ErrInvalidName`, `ErrIntegrity`. Match a `*CodecError` with `errors.As` to
inspect class, code and resource dimension. Missing child paths have a boolean
absence result; missing storage does not masquerade as an empty value.

The SDK was extracted from private bitstore-svc at `5bd2fa2`; consumers migrate
the import path to this versioned public module. Its raw content and HTTP
contract dates remain unchanged. The newly introduced DataTree codec profile is
a candidate and must be persisted alongside roots.

`EncodeFlat(ctx, tree, limits)` and `EncodeLinked(ctx, tree, limits)` may read
remote `Data` capabilities; always handle errors and cancellation. Native
constructors do not perform reads. Empty bytes use `BytesData(nil)`; a nil
reader is invalid. `At(TreePath)` and `Select` return `(node, present)`, while
the derived `Read` returns `ErrPathNotFound` for a missing key. Reader failures
propagate unchanged. The separate lazy `DataTreeView` lists child roots and is
not a full `DataTree`; `LoadDataTree` reconstructs one.
