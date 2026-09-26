# Charter

## Decisions owned here

Bitstore owns the public `Bytes`, `Data`, `DataTree`, and `Store` interfaces, data-specific
structural and encoding bindings, content identity, and independently specified
conformance cases. Pure data construction and codecs are library facilities.
The deployed blob service, filesystem persistence, and instance administration
remain service responsibilities in the private bitstore-svc repository.

`Data` is an addressless fixed-content reader. `DataTree = DeixisNode<Data>`
realizes the Deixis mandatory-own-value model: every node has its own reader
and whole byte-keyed children. The generic contract includes own, complete
children, selection and decomposition, equally for `WireTree = DeixisNode<Wire>`.
Pure structure does not invoke readers or senders. Serialized snapshots contain
bytes read from capabilities, never executable capabilities. Empty payload, empty key, empty path, a missing
node, and unavailable or corrupt storage remain distinct observations.

## Consumer promise and versions

The public library builds and installs without private repository access.
Native package versions, the raw blob contract, and the data codec profile have
distinct identities. Immutable releases are never rewritten. Candidate codec
use records its exact profile; a package release does not freeze that codec.

The first delivery targets Go and TypeScript. Additional language presentations
are separate work and must report their own evidence.

## Independent evidence

Contract-authored fixtures and reusable conformance cases test public interfaces.
They cover exact keys, own values, reconstruction, storage integrity, absence,
bounded parsing, streaming outcomes, and Go/TypeScript interchange. Deliberately
unlawful stores establish that integrity and absence checks detect violations.
The private service exercises the released public contract over memory,
filesystem, HTTP, and container boundaries, including restart.

## Why this repository is separate

Programs can use and exchange structured data and implement a Store without
depending on the private deployed service. A service implementation can change
without changing the data or storage contract. Public consumers can verify the
same observations against independent implementations.
