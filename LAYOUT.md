# Repository layout

Use component first, then an exactly two-letter language directory:

```text
store/go/                 store/ts/
conformance/go/           conformance/ts/
cmd/<command>/go/         cmd/<command>/ts/
docs/                    vectors/
```

Create a directory only when it holds a real component. Native source, tests and
language-specific package metadata belong with their language implementation.
Root workspace manifests and maintenance scripts may keep normal tooling paths.
Shared specifications, vectors and documentation stay language-neutral.

Language codes include `go`, `ts`, `py`, `rs`, `hs`, `cc`, `jv`, `sw`, `rb`, `kt`,
`cs`, and `sh`. Service SDKs also follow the service standard's code registry.
Module and package boundaries follow coherent semantic commitments; the layout
does not require a separate module for every directory or type.

The family policy is maintained in
[Bitwire](https://github.com/Bitspark/bitwire/blob/main/LAYOUT.md).
