# Cloud Datastore

Lab Datastore gRPC (`google.datastore.v1.Datastore`) with Lookup, Commit (Put/Delete), and equality RunQuery. Distinct from Firestore store tables.

## Status

**lab** — entity Lookup / Commit mutations / RunQuery with EQUAL filters.

## Wire protocol

gRPC service: `google.datastore.v1.Datastore` on the same h2c port as other gRPC services (`:4588`).

| RPC | Lab behavior |
|-----|--------------|
| `Lookup` | Key get; missing keys returned in `missing` |
| `Commit` | `insert` / `upsert` / `update` / `delete` (non-transactional) |
| `RunQuery` | Kind + EQUAL property filters (+ optional LIMIT); GQL deferred |

Incomplete numeric keys are allocated on insert/upsert.

## Authz

- `datastore.entities.get|list|write`

## Client configuration

```bash
export DATASTORE_EMULATOR_HOST=127.0.0.1:4588
```

Go clients that honor the emulator host will dial gRPC against that address. Bearer auth is still required (`authorization: Bearer <token>` metadata).

## Emulator limits

- No transactions beyond non-transactional Commit
- No ancestor queries, inequality, OR, projections, or GQL
- Properties stored as JSON scalars (string/bool/number); complex Value kinds are best-effort
- Separate SQLite tables from Firestore (`datastore_entities` vs `firestore_docs`)

## Deferred depth

- BeginTransaction / Rollback / transactional Commit
- Aggregation queries, AllocateIds / ReserveIds depth
- Indexes metadata, Admin export/import
- HTTP JSON transcoding surface

## Verification / CLI smoke

```bash
go test ./internal/server/ -run Datastore -count=1
```
