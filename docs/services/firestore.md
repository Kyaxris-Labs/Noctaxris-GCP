# Firestore

Lab-complete Firestore v1 gRPC on the shared Noctaxris-GCP port (`127.0.0.1:4588`).

## Status

**lab** — document CRUD, list, batch get, batch write, commit (non-transactional), and simple equality `RunQuery`.

## Wire protocol

| Surface | Notes |
|---------|-------|
| gRPC `google.firestore.v1.Firestore` | Primary |
| Database | `(default)` only |

Document names:

```text
projects/{project}/databases/(default)/documents/{collection}/{docId}
```

## Implemented RPCs

| RPC | Lab behavior |
|-----|----------------|
| `GetDocument` | Load by name |
| `CreateDocument` | Create; auto id when `document_id` empty |
| `UpdateDocument` | Replace fields |
| `DeleteDocument` | Delete by name |
| `ListDocuments` | Immediate child docs in a collection |
| `BatchGetDocuments` | Server stream; missing names reported |
| `BatchWrite` | Non-transactional update/delete writes |
| `Commit` | Same as batch write; rejects transaction bytes |
| `RunQuery` | `StructuredQuery` with one `EQUAL` field filter (or no filter) |

## Not implemented

| RPC | Response |
|-----|----------|
| `BeginTransaction` / `Rollback` | `UNIMPLEMENTED` |
| `Listen` / bidirectional `Write` | `UNIMPLEMENTED` |
| Aggregations, partitions, pipelines | `UNIMPLEMENTED` (embedded default) |

## Authz

Permissions checked on `projects/{project}`:

- `datastore.entities.get`
- `datastore.entities.create`
- `datastore.entities.update`
- `datastore.entities.delete`
- `datastore.entities.list`

## Client configuration

Official Go / many SDKs honor:

```bash
export FIRESTORE_EMULATOR_HOST=127.0.0.1:4588
```

gcloud:

```bash
gcloud config set api_endpoint_overrides/firestore http://127.0.0.1:4588/
```

Bearer token required (root or registered access token). Cleartext h2c on the shared listener.

## Deferred depth

- Multi-database ids beyond `(default)`
- Transactions and snapshot listeners
- Composite indexes, `IN` / array-contains filters, collection groups
- Field masks / transform writes (`serverTimestamp`, increments)

## Verification / CLI smoke

```bash
go test ./internal/services/firestore/ ./internal/store/ -count=1
export FIRESTORE_EMULATOR_HOST=127.0.0.1:4588
# Use a client library Create/Get against projects/$PROJECT/databases/(default)
```
