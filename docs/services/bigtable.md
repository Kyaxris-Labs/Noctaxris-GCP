# Cloud Bigtable

Lab Bigtable Admin API v2 for instances and tables. Control-plane theatre only:
no Bigtable server binary, and no row mutate/read path.

## Status

**lab** — instance and table CRUD; optional cluster metadata stored on create;
create returns the resource in `READY` (no LRO).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v2/projects/{p}/instances` (`instanceId` + `instance` + optional `clusters`) |
| `GET` | `/v2/projects/{p}/instances` |
| `GET` | `/v2/projects/{p}/instances/{instance}` |
| `DELETE` | `/v2/projects/{p}/instances/{instance}` |
| `POST` | `.../instances/{instance}/tables` (`tableId` + `table`) |
| `GET` | `.../instances/{instance}/tables` |
| `GET` | `.../instances/{instance}/tables/{table}` |
| `DELETE` | `.../instances/{instance}/tables/{table}` |

Uses `/v2/...` so it does not collide with Spanner Admin `/v1/.../instances`.

## Authz

Checked on `projects/{project}`:

- `bigtable.instances.create|get|list|delete`
- `bigtable.tables.create|get|list|delete`

Seeded Service Usage: `bigtableadmin.googleapis.com`.

## Emulator limits

- Control-plane only; no data plane (no ReadRows / MutateRows)
- Cluster map is stored on create as JSON metadata but omitted from get/list responses; no real cluster capacity
- Create is synchronous (resource returned ready; no long-running Operation)

## Deferred depth

- Cluster CRUD endpoints, backups, app profiles
- Data API / official gRPC Bigtable surface

## Verification / CLI smoke

```bash
go test ./internal/services/bigtable/ ./internal/server/ -run Bigtable -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/instances" \
  -d '{"instanceId":"lab","instance":{"displayName":"Lab","type":"PRODUCTION"},"clusters":{"c1":{"location":"us-central1-b","serveNodes":1}}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/instances/lab/tables" \
  -d '{"tableId":"users","table":{"columnFamilies":{"cf1":{}}}}'
```

```bash
gcloud config set api_endpoint_overrides/bigtableadmin http://127.0.0.1:4588/
# No Terraform stack: hashicorp/google google_bigtable_* uses gRPC
# InstanceAdminClient; lab exposes REST Admin /v2/ only. bigtable_custom_endpoint
# alone is not enough for apply against this lab.
```
