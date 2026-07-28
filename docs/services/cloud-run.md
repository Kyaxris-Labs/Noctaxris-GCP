# Cloud Run

Lab Cloud Run Admin API v2 REST for services and revisions. Invoke is an in-process mock (no container start, no host `docker.sock`, no DinD).

## Status

**lab** — services CRUD, revision metadata list, `:invoke` mock response.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v2/projects/{p}/locations/{loc}/services?serviceId=` |
| `GET` | `/v2/projects/{p}/locations/{loc}/services` |
| `GET` | `/v2/projects/{p}/locations/{loc}/services/{svc}` |
| `PATCH` | `/v2/projects/{p}/locations/{loc}/services/{svc}` |
| `DELETE` | `/v2/projects/{p}/locations/{loc}/services/{svc}` |
| `GET` | `/v2/projects/{p}/locations/{loc}/services/{svc}/revisions` |
| `POST`/`GET` | `/v2/projects/{p}/locations/{loc}/services/{svc}:invoke` |

Create body uses v2 `template` (containers/image/env). Optional lab fields:

- `template.labResponseBody` — static JSON string returned on invoke
- env `RESPONSE_BODY` — same, if `labResponseBody` unset

Otherwise invoke returns `{"ok":true,"service":"...","env":{...}}` from template env. Last invoke request is stored (Authorization header omitted).

## Authz

Checked on `projects/{project}`:

- `run.services.create|get|list|update|delete`
- `run.routes.invoke`

## Deferred depth

- Nested container / DinD invoke
- Traffic splitting, jobs, domain mappings, IAM on service resources
- Official gRPC `run.googleapis.com` surface

## Verification / CLI smoke

```bash
go test ./internal/server/ -run CloudRun -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/services?serviceId=demo" \
  -d '{"template":{"containers":[{"image":"demo"}],"labResponseBody":"{\"ok\":true}"}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/services/demo:invoke" \
  -d '{}'
```
