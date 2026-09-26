# Persistence of the filesystem store

**Status:** implemented by [store/](../store/store.go). What a test establishes
is listed in [the conformance evidence](../conformance/README.md#evidence):
acknowledged content read back after the store is reopened over the same
directory, aborted and cancelled uploads leaving nothing, staged files never
promoted, and acknowledged content read back after the executable is killed
abruptly and started again over the same directory, and after a container is
killed and restarted over the same volume. Nothing here has been tested against
power loss or a failing filesystem; those claims are the profile's, not the
tests'.

It supplements [the surface](SURFACE.md) and [the HTTP binding](HTTP.md). It
adds no verb, changes no wire meaning and imposes no layout on another
implementation. The SDK's in-memory store has no persistence guarantee.

## Layout

One data directory, chosen by the operator, holding two subdirectories on the
same local filesystem:

| path | holds |
|---|---|
| `objects/<first 2 hex digits>/<remaining 62 hex digits>` | one regular file per committed blob, its content exactly |
| `tmp/` | uploads in progress, under names no verb can reach |

A blob's file is named by its digest alone; the `sha256:` prefix is implied by
the directory. This is the layout of the `bytes` store in ULab, kept so that a
store moves between the two by copying its `objects` directory
([decision 0001](../docs/decisions/0001-independent-content-store.md)).

## Acknowledgment

For a new blob, the store:

1. reads and hashes the complete input into a new file under `tmp/`, checking
   the deployment's size cap as bytes arrive;
2. on clean end of input, flushes the file to stable storage and closes it;
3. checks that the request was not cancelled;
4. publishes the file by one rename to its content-derived path under
   `objects/`;
5. flushes the shard directory and `objects/` where the platform provides a
   directory flush;
6. returns success, after which the binding emits the name and size.

Partial input is never published. The temporary and object directories must be
on one filesystem so that step 4 is atomic.

A name already present deduplicates at commit time: the temporary file is
discarded and the object directories are flushed before success is reported.
The SDK's optional check-before-put instead returns after `has` confirms the
name, without uploading or adding a durability barrier; it relies on the
existing object's persistence.

Each `put many` item passes through the same path independently. A later
failure cannot roll back an earlier item, so a batch-level error or a missing
batch response does not mean that no item was stored.

## Restart and interrupted requests

With the same intact data directory, acknowledged blobs remain readable after
the service stops and starts again, including after abrupt termination.
Reading an existing store needs no index replay and no shutdown checkpoint.
A container keeps its data volume across replacement; an ephemeral or
different directory is a different store.

A crash before publication may leave a file in `tmp/`, which no verb can reach.
Startup neither promotes such files nor sweeps them; an operator may remove
them while the store is stopped.

A failure after publication — a failed directory flush, termination after the
rename, a lost response — may leave the complete blob visible **without** a
successful acknowledgment. An error, timeout or disconnect after dispatch is
not proof of absence: the client asks `has` for the expected name or sends the
same bytes again. An interrupted download changes nothing stored.

## Crash versus power loss

| environment | what the store does, and what stays outside its guarantee |
|---|---|
| Linux, macOS | Success follows the file flush and the object-directory flushes. This depends on the operating system, filesystem, mount and device honoring them; a filesystem that rejects a required flush fails startup or the upload. |
| Windows | The blob file is flushed before the rename, but Windows offers no portable directory flush, so the renamed entry's survival of power loss is not guaranteed. |
| any | The operator provides and keeps a durable data directory. Initialization flushes that directory where possible but not its parent's entry. The store supplies no replication, backup, media-corruption recovery or power-loss guarantee of the hardware. |

Atomic publication keeps a partial upload from ever becoming a named object. It
does not establish that every acknowledged object survives power loss or the
failure of the storage beneath it, and neither a name nor a reference held
elsewhere is a retention guarantee.
