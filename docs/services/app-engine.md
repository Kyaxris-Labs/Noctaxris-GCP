# App Engine

Lab App Engine Admin API v1 control-plane theatre for applications, services, and
versions. No runtimes are started (no host `docker.sock`, no DinD, no serving).

## Status

**lab** — create/get Application; list/get services; list/get/create/delete
versions with runtime and env var metadata only.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/apps` |
| `GET` | `/v1/apps/{app}` |
| `GET` | `/v1/apps/{app}/services` |
| `GET` | `/v1/apps/{app}/services/{service}` |
| `GET` | `/v1/apps/{app}/services/{service}/versions` |
| `POST` | `/v1/apps/{app}/services/{service}/versions` |
| `GET` | `/v1/apps/{app}/services/{service}/versions/{version}` |
| `DELETE` | `/v1/apps/{app}/services/{service}/versions/{version}` |

Create Application body requires `id` (project id) and may set `locationId`.
Create Version body requires `id`; optional `runtime`, `env`, and `envVariables`.
Creating a version auto-creates the parent service when missing.

Lab returns Application / Version JSON synchronously (GCP returns LROs).

## Authz

Checked on `projects/{app}` (application id equals project id):

- `appengine.applications.create|get`
- `appengine.services.list|get`
- `appengine.versions.list|get|create|delete`

## Emulator limits

- No runtime start and no HTTP serving (control-plane metadata only)
- No host `docker.sock` or DinD
- Create Application / Version returns JSON synchronously (no LRO)

## Deferred depth

- Deploy / build / serve instances
- Traffic split updates, domains, firewall rules
- Official gRPC `appengine.googleapis.com` surface

## Verification / CLI smoke

```bash
go test ./internal/server/ -run AppEngine -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:4588/v1/apps \
  -d '{"id":"noctaxris-gcp-local","locationId":"us-central"}'
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:4588/v1/apps/noctaxris-gcp-local/services/default/versions \
  -d '{"id":"v1","runtime":"python311","envVariables":{"GREETING":"hi"}}'
```

```bash
gcloud config set api_endpoint_overrides/appengine http://127.0.0.1:4588/
```
