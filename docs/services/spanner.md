# Cloud Spanner

Lab Spanner Admin REST for instances and databases, plus session SQL/read theatre.
No Spanner server binary is embedded; SQL does not run a Spanner dialect.

## Status

**lab** — instance and database CRUD; DDL statement storage (`PATCH .../ddl`);
session create and `sessions:batchCreate`; mutation insert via `:commit` (SQLite-backed);
`:executeSql` / `:read` return inserted rows; `:partitionQuery` stub; list instance configs stub.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `GET` | `/v1/projects/{p}/instanceConfigs` |
| `POST` | `/v1/projects/{p}/instances` (`instanceId` + `instance`) |
| `GET` | `/v1/projects/{p}/instances` |
| `GET` | `/v1/projects/{p}/instances/{instance}` |
| `DELETE` | `/v1/projects/{p}/instances/{instance}` |
| `POST` | `.../instances/{instance}/databases` (`createStatement`) |
| `GET` | `.../instances/{instance}/databases` |
| `GET` | `.../instances/{instance}/databases/{database}` |
| `DELETE` | `.../instances/{instance}/databases/{database}` |
| `PATCH` | `.../databases/{database}/ddl` (`statements[]`) |
| `POST` | `.../databases/{database}/sessions` |
| `POST` | `.../databases/{database}/sessions:batchCreate` |
| `POST` | `.../sessions/{session}:executeSql` |
| `POST` | `.../sessions/{session}:read` |
| `POST` | `.../sessions/{session}:commit` |
| `POST` | `.../sessions/{session}:partitionQuery` |

Create instance/database returns the resource in `READY` state (no LRO).
`createStatement` must look like `CREATE DATABASE \`id\``.
`PATCH .../ddl` stores statements and returns a completed Operation theatre.
Colon methods are parsed from path segments (ServeMux-safe).

## Authz

Checked on `projects/{project}`:

- `spanner.instances.create|get|list|delete`
- `spanner.instanceConfigs.list`
- `spanner.databases.create|get|list|drop|updateDdl`
- `spanner.sessions.create`
- `spanner.databases.select` (executeSql)
- `spanner.databases.read`
- `spanner.databases.write` (commit mutations)
- `spanner.databases.partitionQuery`

## Emulator limits

- Control-plane + SQLite-backed mutation insert / SELECT / Read lite; no Spanner binary or dialect
- `:commit` supports `insert` mutations only (no update/delete/replace)
- `executeSql` supports `SELECT cols|* FROM Table [WHERE col = value]` over inserted rows
- `read` returns inserted rows for `keySet.all` or key equality on the first column
- `partitionQuery` returns a single lab partition token
- DDL is stored metadata only (no schema apply)
- Create / updateDdl are synchronous (completed Operation for DDL)

## Deferred depth

- Backups, streaming SQL, full transactions / update-delete mutations
- Official gRPC Spanner surface

## Verification / CLI smoke

```bash
go test ./internal/services/spanner/ ./internal/server/ -run Spanner -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/instances" \
  -d '{"instanceId":"lab","instance":{"config":"projects/noctaxris-gcp-local/instanceConfigs/regional-us-central1","displayName":"Lab","nodeCount":1}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/instances/lab/databases" \
  -d '{"createStatement":"CREATE DATABASE `app`"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/instances/lab/databases/app/sessions" \
  -d '{}'
# Extract session id, then commit insert + executeSql:
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/instances/lab/databases/app/sessions/$SESSION_ID:commit" \
  -d '{"mutations":[{"insert":{"table":"Singers","columns":["SingerId","FirstName"],"values":[["1","Marc"]]}}]}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/instances/lab/databases/app/sessions/$SESSION_ID:executeSql" \
  -d '{"sql":"SELECT SingerId, FirstName FROM Singers"}'
```

```bash
gcloud config set api_endpoint_overrides/spanner http://127.0.0.1:4588/
```
