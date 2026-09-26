# The management surface

**Contract:** 2026-09-25, the day this surface last changed. Every response of
the management listener carries it; a change to what this file means moves it,
here, in [HTTP.md](HTTP.md) and in [operations.json](operations.json), and
never moves the operations contract's date.

bitstore's instance administration, on its own listener and apart from the
content verbs of [the operations surface](../SURFACE.md). This surface is the
bit-services administration baseline: it observes the instance and administers
nothing yet.

## Verbs

None. The management listener carries only the four operational routes every
listener serves — `/livez`, `/healthz`, `/describe` and `/describe/{file...}` —
which report this listener's health and publish this contract. No content verb
is served here, and no administration verb is served on the operations
listener. Inventory, configuration or any other administration is a further
decision of this service, and arrives as verbs of this surface.

## Refusals

| refusal | meaning |
|---|---|
| `not found` | no such route, or no such contract file |

## What the surface does not do

It grants no authority, proves no isolation and adds no authentication:
deployment decides who can reach each listener. It does not read or change
stored content, and a management endpoint is never derived from an operations
endpoint or the other way round.
