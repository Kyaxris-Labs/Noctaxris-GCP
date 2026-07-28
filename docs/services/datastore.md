# Cloud Datastore

Lab Datastore gRPC (`google.datastore.v1.Datastore`) with Lookup, Commit, AllocateIds, lab transaction tokens, structured AND filters, and a GQL subset. Distinct from Firestore store tables.

## Status

**lab** — entity Lookup / Commit mutations / RunQuery (structured AND or GQL) / AllocateIds / BeginTransaction + Rollback.

## Wire protocol

gRPC service: `google.datastore.v1.Datastore` on the same h2c port as other gRPC services (`:4588`).

| RPC | Lab behavior |
|-----|--------------|
| `Lookup` | Key get; missing keys returned in `missing` |
| `Commit` | `insert` / `upsert` / `update` / `delete`; TRANSACTIONAL mode consumes BeginTransaction token once (no isolation) |
| `RunQuery` | Kind + EQUAL filters with AND composites, optional LIMIT; or GQL subset |
| `AllocateIds` | Incomplete numeric keys; allocates and reserves ids |
| `BeginTransaction` | Lab UUID token |
| `Rollback` | Clears a lab transaction token |

### GQL subset

```text
SELECT * FROM Kind [WHERE a = lit AND b = lit] [LIMIT n]
```

Requires `allow_literals: true` when literals appear. Structured `Query` with `CompositeFilter` AND is also supported.

Incomplete numeric keys are allocated on insert/upsert.

## Authz

- `datastore.entities.get|list|write`

## Client configuration

```bash
export DATASTORE_EMULATOR_HOST=127.0.0.1:4588
```

Go clients that honor the emulator host will dial gRPC against that address. Bearer auth is still required (`authorization: Bearer <token>` metadata).

## Emulator limits

- Transaction tokens only (no isolation or conflict detection)
- No ancestor queries, inequality, OR, or projections
- Properties stored as JSON scalars (string/bool/number); complex Value kinds are best-effort
- Separate SQLite tables from Firestore (`datastore_entities` vs `firestore_docs`)

## Deferred depth

- Aggregation queries, ReserveIds depth
- Indexes metadata, Admin export/import
- HTTP JSON transcoding surface

## Verification / CLI smoke

```bash
go test ./internal/server/ -run Datastore -count=1
```
