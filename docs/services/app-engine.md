# App Engine

Lab App Engine Admin API v1 control-plane theatre for applications, services, and
versions. No runtimes are started (no host `docker.sock`, no DinD, no serving).

## Status

**lab** — create/get Application; list/get/patch services (traffic split +
`migrateTraffic` theatre); list/get/create/delete versions with runtime and env
var metadata; list instances returns empty.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/apps` |
| `GET` | `/v1/apps/{app}` |
| `GET` | `/v1/apps/{app}/services` |
| `GET` | `/v1/apps/{app}/services/{service}` |
| `PATCH` | `/v1/apps/{app}/services/{service}` (`?migrateTraffic=true` optional) |
| `GET` | `/v1/apps/{app}/services/{service}/versions` |
| `POST` | `/v1/apps/{app}/services/{service}/versions` |
| `GET` | `/v1/apps/{app}/services/{service}/versions/{version}` |
| `DELETE` | `/v1/apps/{app}/services/{service}/versions/{version}` |
| `GET` | `/v1/apps/{app}/services/{service}/versions/{version}/instances` |

Create Application body requires `id` (project id) and may set `locationId`.
Create Version body requires `id`; optional `runtime`, `env`, and `envVariables`.
Creating a version auto-creates the parent service when missing.

Patch Service body may set `split` (allocations map) and `shardBy`. Query
`migrateTraffic=true` records gradual-migration theatre metadata. Lab returns
Service JSON synchronously (GCP returns an LRO).

List instances always returns `{ "instances": [] }` (no serving).

## Authz

Checked on `projects/{app}` (application id equals project id):

- `appengine.applications.create|get`
- `appengine.services.list|get|update`
- `appengine.versions.list|get|create|delete`
- `appengine.instances.list`

## Emulator limits

- No runtime start and no HTTP serving (control-plane metadata only)
- No host `docker.sock` or DinD
- Create Application / Version / patch Service return JSON synchronously (no LRO)
- Domain mappings and firewall rules are not implemented

## Deferred depth

- Deploy / build / serve instances
- Domains, firewall rules
- Official gRPC `appengine.googleapis.com` surface

## Verification / CLI smoke

```bash
go test ./internal/services/appengine/ ./internal/server/ -run AppEngine -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:4588/v1/apps \
  -d '{"id":"noctaxris-gcp-local","locationId":"us-central"}'
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:4588/v1/apps/noctaxris-gcp-local/services/default/versions \
  -d '{"id":"v1","runtime":"python311","envVariables":{"GREETING":"hi"}}'
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -X PATCH "http://127.0.0.1:4588/v1/apps/noctaxris-gcp-local/services/default?migrateTraffic=true" \
  -d '{"split":{"allocations":{"v1":1}},"shardBy":"IP"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:4588/v1/apps/noctaxris-gcp-local/services/default/versions/v1/instances
```

```bash
gcloud config set api_endpoint_overrides/appengine http://127.0.0.1:4588/
```
