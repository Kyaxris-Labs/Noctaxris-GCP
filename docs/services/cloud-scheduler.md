# Cloud Scheduler

Lab Cloud Scheduler v1 REST for jobs. Cron is a 5-field expression (`minute hour dom mon dow`) with best-effort `scheduleTime` next-run. Targets fire best-effort on `:run`, and an in-process ticker for every-N-minute cron theatre (`* * * * *` / `*/N * * * *`).

## Status

**lab** — jobs CRUD, `:run` / `:pause` / `:resume`, HTTP and Pub/Sub targets, OIDC audience stored (token stripped), next-run metadata.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/jobs?jobId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/jobs` |
| `GET` | `/v1/projects/{p}/locations/{loc}/jobs/{job}` |
| `PATCH` | `/v1/projects/{p}/locations/{loc}/jobs/{job}` |
| `DELETE` | `/v1/projects/{p}/locations/{loc}/jobs/{job}` |
| `POST` | `.../jobs/{job}:run` |
| `POST` | `.../jobs/{job}:pause` / `:resume` |

Job body fields used: `schedule`, `timeZone`, `httpTarget` (`uri`, `httpMethod`, `headers`, `body`, optional `oidcToken`), `pubsubTarget` (`topicName`, `data`). Body/data may be base64 (Google JSON bytes) or plain text.

`httpTarget.oidcToken` is stripped before persistence; `audience` is retained as `oidcTokenAudience` on the job resource. `scheduleTime` is the computed next run (best-effort).

Pub/Sub publish uses the existing store when the topic exists; missing topics fail silently on fire.

## Authz

Checked on `projects/{project}`:

- `cloudscheduler.jobs.create|get|list|update|delete|run`

## Emulator limits

- HTTP `httpTarget.uri` must pass the lab HTTP egress gate (catcher on loopback `:4588` by default; see security-defaults)
- Cron ticker runs in-process for `* * * * *` and `*/N * * * *` only; other schedules rely on `:run` or stored `scheduleTime`
- Pub/Sub publish on fire is best-effort; missing topics fail silently
- App Engine HTTP targets are not implemented

## Deferred depth

- App Engine HTTP targets
- gRPC surface

## Verification / CLI smoke

```bash
go test ./internal/services/scheduler/ ./internal/server/ -run Scheduler -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/jobs?jobId=daily" \
  -d '{"schedule":"0 9 * * 1","httpTarget":{"uri":"http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/sched-smoke","httpMethod":"POST"}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/jobs/daily:run"
```
