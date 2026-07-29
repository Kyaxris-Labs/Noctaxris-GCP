# Firestore

Lab-complete Firestore v1 gRPC on the shared Noctaxris-GCP port (`127.0.0.1:4588`).

## Status

**lab** — document CRUD with field masks, list, batch get, batch write, atomic Commit with lab transaction tokens / FieldTransforms / `current_document` exists preconditions, collection-group `RunQuery` with ORDER BY / LIMIT / inequality, and PartitionQuery stub.

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
| `BatchWrite` | Non-transactional update/delete writes; honors `update_mask`; per-write status |
| `Commit` | Applies writes atomically (SQLite all-or-nothing); `serverTimestamp` / `increment` FieldTransforms; `current_document` exists / not-exists; accepts BeginTransaction UUID token (consumed once) |
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

- Named database ids beyond `(default)` (only `(default)` is wired; other database paths are not supported)
- `Listen` bidirectional streaming (real-time snapshots and writes); use polling `GetDocument` / `RunQuery` in the lab
- Real conflict detection / optimistic concurrency beyond atomic Commit and exists preconditions
- Composite indexes, nested field-path masks, multi-field inequalities
- Additional transforms (`arrayUnion`, `maximum` / `minimum`)

## Verification / CLI smoke

```bash
go test ./internal/services/firestore/ -count=1
export FIRESTORE_EMULATOR_HOST=127.0.0.1:4588
export TOKEN="$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN"
# gRPC CreateDocument / GetDocument against projects/$PROJECT/databases/(default)
# (official clients honor FIRESTORE_EMULATOR_HOST; Bearer still required)
go test ./tests/sdk/go/ -run FirestoreCreateGet -count=1
```
