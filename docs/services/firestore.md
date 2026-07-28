# Firestore

Lab-complete Firestore v1 gRPC on the shared Noctaxris-GCP port (`127.0.0.1:4588`).

## Status

**lab** — document CRUD with field masks, list, batch get, batch write, commit with lab transaction tokens and FieldTransforms, collection-group `RunQuery` with ORDER BY / LIMIT / inequality, and PartitionQuery stub.

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
| `UpdateDocument` | Replace fields, or merge top-level paths from `update_mask` |
| `DeleteDocument` | Delete by name |
| `ListDocuments` | Immediate child docs in a collection |
| `BatchGetDocuments` | Server stream; missing names reported |
| `BatchWrite` | Non-transactional update/delete writes; honors `update_mask` |
| `Commit` | Applies writes; `serverTimestamp` / `increment` FieldTransforms; accepts BeginTransaction UUID token (consumed once); no isolation |
| `BeginTransaction` | Returns a lab UUID token for the database |
| `Rollback` | Clears a lab transaction token |
| `RunQuery` | Collection or collection-group (`all_descendants`); `EQUAL` / `IN` / `ARRAY_CONTAINS` / single-field inequality; `ORDER BY` + `LIMIT` |
| `PartitionQuery` | Returns one empty cursor partition (single logical partition stub) |

## Not implemented

| RPC | Response |
|-----|----------|
| `Listen` / bidirectional `Write` | `UNIMPLEMENTED` |
| Aggregations, pipelines | `UNIMPLEMENTED` (embedded default) |

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
- Real transaction isolation, conflict detection, and snapshot listeners (`Listen`)
- Composite indexes, nested field-path masks, multi-field inequalities
- Additional transforms (`arrayUnion`, `maximum` / `minimum`)

## Verification / CLI smoke

```bash
go test ./internal/services/firestore/ ./internal/store/ -count=1
export FIRESTORE_EMULATOR_HOST=127.0.0.1:4588
# Use a client library Create/Get against projects/$PROJECT/databases/(default)
```
