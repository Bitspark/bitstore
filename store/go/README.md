# Bitstore for Go

Import `github.com/Bitspark/bitstore/store/go` as `bitstore`. The root module
contains the public contract, SDK and conformance suite; it has no private or
runtime module dependencies. `Bytes` is `[]byte`, `Data` is an immutable own
value plus exact byte-keyed children, and `Store` supplies nine raw blob verbs.

```go
leaf, _ := bitstore.NewData(nil)
value, _ := bitstore.NewData([]byte("own"), bitstore.Child{Key: nil, Data: leaf})
store := bitstore.NewMemory(0)
root, err := bitstore.PutData(ctx, store, value, bitstore.DataLimits{})
// Check err before using root.
view, err := bitstore.OpenData(ctx, store, root, bitstore.DataLimits{})
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
contract dates remain unchanged. The newly introduced Data codec profile is
a candidate and must be persisted alongside roots.
