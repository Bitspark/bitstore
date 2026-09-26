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

**Status:** implementation is in progress. The existing blob service and SDK are
being migrated to this public contract. No release or completed storage
integration is claimed by this initial charter.

Read [CHARTER.md](CHARTER.md) for ownership and [LAYOUT.md](LAYOUT.md) for source
organization.
