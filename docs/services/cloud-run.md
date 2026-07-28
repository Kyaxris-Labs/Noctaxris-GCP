# Cloud Run

Lab Cloud Run Admin API v2 REST for services, revisions, and jobs. Invoke is an in-process mock (no container start, no host `docker.sock`, no DinD). Service create/update returns a completed Operation (`done: true` + `response`); GET returns the service. Terraform: `cloud_run_v2_custom_endpoint = "http://127.0.0.1:4588/v2/"` (see `tests/terraform/stacks/lab-run`).

## Status

**lab** — services CRUD with traffic metadata, jobs CRUD theatre, revision list, service IAM get/set, `:invoke` mock (headers recorded).

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
| `GET`/`POST` | `.../services/{svc}:getIamPolicy` / `:setIamPolicy` |
| `POST` | `/v2/projects/{p}/locations/{loc}/jobs?jobId=` |
| `GET` | `/v2/projects/{p}/locations/{loc}/jobs` |
| `GET`/`PATCH`/`DELETE` | `/v2/projects/{p}/locations/{loc}/jobs/{job}` |

Create/patch may include `traffic` (percent allocation to latest/revision). Optional lab fields:

- `template.labResponseBody` — static JSON string returned on invoke
- env `RESPONSE_BODY` — same, if `labResponseBody` unset

Otherwise invoke returns `{"ok":true,"service":"...","env":{...}}`. Last invoke stores method, path, query, headers (Authorization omitted), and body.

Jobs are control-plane theatre only (template stored; no execution).

## Authz

Checked on `projects/{project}`:

- `run.services.create|get|list|update|delete|getIamPolicy|setIamPolicy`
- `run.routes.invoke`
- `run.jobs.create|get|list|update|delete`

## Deferred depth

- Nested container / DinD invoke
- Domain mappings, worker pools
- Official gRPC `run.googleapis.com` surface

## Verification / CLI smoke

```bash
go test ./internal/server/ -run CloudRun -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/services?serviceId=demo" \
  -d '{"template":{"containers":[{"image":"demo"}],"labResponseBody":"{\"ok\":true}"},"traffic":[{"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST","percent":100}]}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/services/demo:invoke" \
  -d '{}'
```
