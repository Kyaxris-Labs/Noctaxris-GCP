# Cloud Spanner

Lab Spanner Admin REST for instances and databases, plus session SQL/read theatre.
No Spanner server binary is embedded; SQL does not run a Spanner dialect.

## Status

**lab** — instance and database CRUD; DDL statement storage (`PATCH .../ddl`);
session create and `sessions:batchCreate`; `:executeSql` / `:read` empty ResultSet;
`:partitionQuery` stub; list instance configs stub.

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
- `spanner.databases.partitionQuery`

## Emulator limits

- Control-plane + SQL theatre only; no real Spanner query engine, mutations, or transactions
- `executeSql` / `read` return empty ResultSet-shaped JSON (no mutation insert theatre)
- `partitionQuery` returns a single lab partition token
- DDL is stored metadata only (no schema apply)
- Create / updateDdl are synchronous (completed Operation for DDL)

## Deferred depth

- Backups, streaming SQL, real transactions / mutations
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
curl -s -H "Authorization: Bearer $TOKEN" -X PATCH \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/instances/lab/databases/app/ddl" \
  -d '{"statements":["CREATE TABLE T (id INT64) PRIMARY KEY(id)"]}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/instanceConfigs"
# Session + ExecuteSql theatre (empty ResultSet-shaped reply):
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/instances/lab/databases/app/sessions" \
  -d '{}'
# Extract session id from response name (.../sessions/{id}), then:
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/instances/lab/databases/app/sessions/$SESSION_ID:executeSql" \
  -d '{"sql":"SELECT 1"}'
```

```bash
gcloud config set api_endpoint_overrides/spanner http://127.0.0.1:4588/
```
