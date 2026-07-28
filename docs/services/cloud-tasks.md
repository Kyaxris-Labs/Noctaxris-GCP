# Cloud Tasks

Lab Cloud Tasks v2 REST for queues and tasks. OIDC/OAuth token fields are stripped and not stored. Dispatch is best-effort HTTP to `httpRequest.url`, on create when `scheduleTime` is due, or via `:run` (forced).

## Status

**lab** — queues/tasks CRUD, rate limits + retry config stored, App Engine HTTP theatre fields, `:run` dispatch.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v2/projects/{p}/locations/{loc}/queues?queueId=` |
| `GET` | `/v2/projects/{p}/locations/{loc}/queues` |
| `GET`/`PATCH` | `/v2/projects/{p}/locations/{loc}/queues/{q}` |
| `DELETE` | `/v2/projects/{p}/locations/{loc}/queues/{q}` |
| `POST` | `/v2/projects/{p}/locations/{loc}/queues/{q}/tasks` |
| `GET` | `/v2/projects/{p}/locations/{loc}/queues/{q}/tasks` |
| `GET` | `/v2/projects/{p}/locations/{loc}/queues/{q}/tasks/{t}` |
| `DELETE` | `/v2/projects/{p}/locations/{loc}/queues/{q}/tasks/{t}` |
| `POST` | `.../tasks/{t}:run` |

Queue body may include `rateLimits`, `retryConfig`, and `appEngineRoutingOverride` (stored and echoed).

Create task body (Google shape): `{"task":{"httpRequest":{...},"appEngineHttpRequest":{...},"scheduleTime":"..."},"taskId":"..."}`.

App Engine HTTP fields are stored for theatre; remote App Engine routing is not executed. `:run` always increments `dispatchCount` / `responseCount` and attempts HTTP when `httpRequest.url` is set. Failed HTTP targets are ignored.

## Authz

Checked on `projects/{project}`:

- `cloudtasks.queues.create|get|list|update|delete`
- `cloudtasks.tasks.create|get|list|delete|run`

## Deferred depth

- Lease/pull queues, timed retry backoff enforcement
- gRPC `google.cloud.tasks.v2`

## Verification / CLI smoke

```bash
go test ./internal/server/ -run CloudTasks -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/queues?queueId=default" \
  -d '{"rateLimits":{"maxDispatchesPerSecond":10},"retryConfig":{"maxAttempts":5}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/queues/default/tasks" \
  -d '{"taskId":"t1","task":{"httpRequest":{"url":"http://127.0.0.1:9/hook","httpMethod":"POST"}}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/queues/default/tasks/t1:run"
```
