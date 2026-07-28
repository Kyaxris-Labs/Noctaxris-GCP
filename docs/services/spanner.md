# Cloud Spanner

Lab Spanner Admin REST for instances and databases, plus session `executeSql` theatre. No Spanner server binary is embedded; SQL does not run a Spanner dialect.

## Status

**lab** — instance and database CRUD; session create; `:executeSql` returns an empty ResultSet-shaped reply.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/instances` (`instanceId` + `instance`) |
| `GET` | `/v1/projects/{p}/instances` |
| `GET` | `/v1/projects/{p}/instances/{instance}` |
| `DELETE` | `/v1/projects/{p}/instances/{instance}` |
| `POST` | `.../instances/{instance}/databases` (`createStatement`) |
| `GET` | `.../instances/{instance}/databases` |
| `GET` | `.../instances/{instance}/databases/{database}` |
| `DELETE` | `.../instances/{instance}/databases/{database}` |
| `POST` | `.../databases/{database}/sessions` |
| `POST` | `.../sessions/{session}:executeSql` |

Create instance/database returns the resource in `READY` state (no LRO). `createStatement` must look like `CREATE DATABASE \`id\``. `:executeSql` is parsed from the session path segment (ServeMux-safe colon handling).

## Authz

Checked on `projects/{project}`:

- `spanner.instances.create|get|list|delete`
- `spanner.databases.create|get|list|drop`
- `spanner.sessions.create`
- `spanner.databases.select` (executeSql)

## Emulator limits

- Control-plane + SQL theatre only; no real Spanner query engine, mutations, or transactions
- No instance configs, backups, DDL apply, or streaming SQL
- Create is synchronous (no long-running Operation)

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
