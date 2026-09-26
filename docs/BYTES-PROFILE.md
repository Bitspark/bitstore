# Bytes backend profile, version 1

A compatible operations endpoint advertises this optional extension in its
existing `/describe` document:

```json
{"profiles":[{"id":"bitstore/bytes","version":"1","binding":"bitstore-http/2026-09-24"}]}
```

This backend-specific extension uses the standard's open describe envelope.
It adds no route, changes no raw verb, and does not change the raw contract date.
It is not a newly mandatory shared service-standard field. The deployed service
publishes this document under its existing `/describe/{file...}` facility.

The advertisement commits to all nine raw Store operations and the contract
and HTTP binding in `docs/raw/`, including content identity, plural absence,
stream commit rules, per-item batch outcomes, and integrity-verifiable full
reads. It does not claim that the service interprets or retains a Data graph.
Data clients store their canonical chunks as ordinary opaque blobs.

Discovery starts from an explicitly configured operations base URL, preserving
its deployment prefix. It retrieves `/describe`, selects a supported profile
by `id`, and requires exactly one declaration with this version and binding.
The envelope and response contract must agree on raw contract `2026-09-24`,
service `bitstore`, operations surface (omitted or explicit), primary `http`,
native application prefix `/v1`. Missing, duplicate, unsupported and
contradictory declarations fail before data I/O. Redirects are refused.
Management endpoints are configured independently and never derived by port
or path guessing; they cannot satisfy this profile.

`DiscoverBytes` / `discoverBytes` implements this adapter selection. Raw users
may instantiate `Client` directly against an older, unadvertised compatible
service. No discovery request is needed for an in-process Store.

Shared discovery of both End and Bytes by one generic Deixis service remains
the cross-backend service-contract integration. This document implements and
versions the storage adapter seam; it does not claim that the generic service
or the End adapter has shipped.
