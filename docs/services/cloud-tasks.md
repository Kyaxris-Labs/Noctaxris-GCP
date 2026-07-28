# Cloud Tasks

Lab Cloud Tasks v2 REST for queues and tasks. OIDC/OAuth token fields are stripped and not stored. Dispatch is best-effort HTTP to `httpRequest.url`, on create when `scheduleTime` is due, or via `:run`.

## Status

**lab** — queues/tasks CRUD, `:run` dispatch, schedule-time storage.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v2/projects/{p}/locations/{loc}/queues?queueId=` |
| `GET` | `/v2/projects/{p}/locations/{loc}/queues` |
| `GET` | `/v2/projects/{p}/locations/{loc}/queues/{q}` |
| `DELETE` | `/v2/projects/{p}/locations/{loc}/queues/{q}` |
| `POST` | `/v2/projects/{p}/locations/{loc}/queues/{q}/tasks` |
| `GET` | `/v2/projects/{p}/locations/{loc}/queues/{q}/tasks` |
| `GET` | `/v2/projects/{p}/locations/{loc}/queues/{q}/tasks/{t}` |
| `DELETE` | `/v2/projects/{p}/locations/{loc}/queues/{q}/tasks/{t}` |
| `POST` | `.../tasks/{t}:run` |

Create task body (Google shape): `{"task":{"httpRequest":{...},"scheduleTime":"..."},"taskId":"..."}`.

App Engine HTTP tasks are not implemented. Failed HTTP targets are ignored (dispatch count still increments).

## Authz

Checked on `projects/{project}`:

- `cloudtasks.queues.create|get|list|delete`
- `cloudtasks.tasks.create|get|list|delete|run`

## Deferred depth

- Rate limits, retries, lease/pull queues
- App Engine routing, gRPC `google.cloud.tasks.v2`

## Verification / CLI smoke

```bash
go test ./internal/server/ -run CloudTasks -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/queues?queueId=default" \
  -d '{}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/queues/default/tasks" \
  -d '{"taskId":"t1","task":{"httpRequest":{"url":"http://127.0.0.1:9/hook","httpMethod":"POST"}}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/queues/default/tasks/t1:run"
```
