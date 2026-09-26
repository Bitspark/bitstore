# Raw Store conformance

`RunStore(t, newSubject)` evaluates a fresh implementation through the public
Store interface. The SHA-256 vectors and behavioral cases were extracted from
bitstore-svc commit `5bd2fa2` with their expected observations intact. Naming
vectors are immutable; additions are allowed. The suite covers raw identity,
idempotence, plural absence, ranges, streaming, failed-upload atomicity,
per-item batch outcomes, cancellation and reopening durable subjects.

Specify `Subject.Reopen` only for a durable implementation and `MaxBytes` only
for a capped implementation. Skips record those boundaries. Process-kill,
filesystem, HTTP listener-pair and container evidence belongs to each service
implementation and is not established by passing this suite alone.

```go
conformance.RunStore(t, func(t *testing.T) conformance.Subject {
    return conformance.Subject{Store: bitstore.NewMemory(2 << 20), MaxBytes: 2 << 20}
})
```

Data grammar fixtures live in `vectors/data.json`, outside either native
implementation. Go/TypeScript tests check expected bytes and failures, lazy
paths, exact keys, reconstruction and deliberately lying stores. The native
interchange script exchanges complete artifacts between independently compiled
presentations; it is agreement evidence, not clean-room independence.
