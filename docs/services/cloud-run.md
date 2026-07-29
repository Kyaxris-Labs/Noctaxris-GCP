# Cloud Run

Lab Cloud Run Admin API v2 REST for services, revisions, and jobs. Service
create/update returns a completed Operation (`done: true` + `response`); GET
returns the service. Terraform:
`cloud_run_v2_custom_endpoint = "http://127.0.0.1:4588/v2/"` (see
`tests/terraform/stacks/lab-run`).

## Mock vs nested `:invoke`

| Mode | When | Behavior |
|------|------|----------|
| Mock (default) | `NOCTAXRIS_GCP_DOCKER_HOST` empty | In-process theatre only; no container start; no host `docker.sock` |
| Mock (forced) | `template.labResponseBody` set (or `RESPONSE_BODY` env) | Nested path skipped even if Docker host is configured |
| Nested (opt-in) | `NOCTAXRIS_GCP_DOCKER_HOST` + `NOCTAXRIS_GCP_DOCKER_CERT_PATH` set; no `labResponseBody` | `DockerInvoker` one-shot via TLS DinD; host `docker.sock` / `unix://` / `npipe://` refused |
| Nested soft-fail | Nested dial/run fails; `NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED` unset | Falls back to mock; response may include `engine` detail |
| Nested fail-closed | Nested dial/run fails; `NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED=1` or `true` | Hard error; no mock fallback |

## Status

**lab** — services CRUD with traffic metadata, jobs CRUD theatre, revision list,
service IAM get/set, `:invoke` mock with status/delay theatre and opt-in nested
DinD one-shot (`NOCTAXRIS_GCP_DOCKER_HOST`).

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

| Field | Effect on `:invoke` |
|-------|---------------------|
| `template.labResponseBody` | Static JSON body |
| env `RESPONSE_BODY` | Same, if `labResponseBody` unset |
| `template.labStatusCode` | HTTP status (default 200) |
| env `RESPONSE_STATUS` | Same as `labStatusCode` |
| `template.labDelayMs` | Sleep theatre before respond (capped at 5000) |
| env `RESPONSE_DELAY_MS` | Same as `labDelayMs` |

Otherwise invoke returns `{"ok":true,"service":"...","env":{...}}`. Last invoke stores method, path, query, headers (Authorization omitted), and body.

Jobs are control-plane theatre only (template stored; no execution).

## Authz

Checked on `projects/{project}`:

- `run.services.create|get|list|update|delete|getIamPolicy|setIamPolicy`
- `run.routes.invoke`
- `run.jobs.create|get|list|update|delete`

## Emulator limits

- Default invoke never starts a container (`NOCTAXRIS_GCP_DOCKER_HOST` empty)
- Nested DinD is opt-in via allowlisted `tcp://` host + TLS cert dir only; host
  `docker.sock` / `unix://` / `npipe://` are rejected
- Nested dial/run failures soft-fail to mock (`engine.detail`) unless
  `NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED` is `1`/`true` (hard error, no mock)
- `template.labResponseBody` forces mock even when the engine is configured
- Domain mappings and worker pools are not implemented

## Deferred depth

- Traffic percent enforcement beyond stored metadata
- Official gRPC `run.googleapis.com` surface

## Verification / CLI smoke

```bash
go test ./internal/services/cloudrun/ ./internal/compute/ ./internal/server/ -run 'CloudRun|MockInvoker' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
# Mock path (labResponseBody skips nested even if DOCKER_HOST is set):
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/services?serviceId=demo" \
  -d '{"template":{"containers":[{"image":"demo"}],"labResponseBody":"{\"ok\":true}","labStatusCode":200},"traffic":[{"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST","percent":100}]}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/services/demo:invoke" \
  -d '{}'
# Nested path: compose.engine.yaml + service without labResponseBody; see docs/configuration.md
```
