# Cloud Bigtable

Lab Bigtable Admin for instances and tables. Control-plane theatre only:
no Bigtable server binary, and no row mutate/read path.

## Status

**lab** — instance and table CRUD (REST); Instance Admin gRPC lite for
Create/Get/List/Delete instance; optional cluster metadata stored on create;
REST create returns the resource in `READY` (no LRO). gRPC CreateInstance
returns a done Operation with the Instance in `response` (no Operations.get).

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

gRPC on the same listener (gRPC-over-HTTP/2):
`google.bigtable.admin.v2.BigtableInstanceAdmin` —
`CreateInstance`, `GetInstance`, `ListInstances`, `DeleteInstance`.

## Authz

Checked on `projects/{project}`:

- `bigtable.instances.create|get|list|delete`
- `bigtable.tables.create|get|list|delete`

Seeded Service Usage: `bigtableadmin.googleapis.com`.

## Emulator limits

- Control-plane only; no data plane (no ReadRows / MutateRows)
- Cluster map is stored on create as JSON metadata but omitted from get/list responses; no real cluster capacity
- REST create is synchronous (resource returned ready; no long-running Operation)
- gRPC CreateInstance returns a done Operation immediately; there is no
  Operations service to poll. Table Admin gRPC, app profiles, clusters CRUD,
  and backups are not implemented.

## Deferred depth

- Cluster CRUD endpoints, backups, app profiles
- Table Admin gRPC and data API / Bigtable data-plane gRPC

## Verification / CLI smoke

```bash
go test ./internal/services/bigtable/ ./internal/server/ -run 'Bigtable|BT' -count=1
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
# Instance Admin gRPC lite is registered for Create/Get/List/Delete instance.
# Terraform google_bigtable_* may still need Table Admin / app profiles /
# cluster APIs that are not implemented; no dedicated TF stack yet.
```
