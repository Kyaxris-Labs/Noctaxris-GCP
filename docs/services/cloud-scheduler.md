# Cloud Scheduler

Lab Cloud Scheduler v1 REST for jobs. Cron string is stored. Targets fire best-effort on `:run`, and an in-process ticker for every-N-minute cron theatre (`* * * * *` / `*/N * * * *`).

## Status

**lab** — jobs CRUD, `:run` / `:pause` / `:resume`, HTTP and Pub/Sub targets.

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

Job body fields used: `schedule`, `timeZone`, `httpTarget` (`uri`, `httpMethod`, `headers`, `body`), `pubsubTarget` (`topicName`, `data`). Body/data may be base64 (Google JSON bytes) or plain text.

Pub/Sub publish uses the existing store when the topic exists; missing topics fail silently on fire.

## Authz

Checked on `projects/{project}`:

- `cloudscheduler.jobs.create|get|list|update|delete|run`

## Deferred depth

- Full cron expression evaluation beyond minute tickers
- App Engine targets, OIDC on HTTP targets, gRPC surface

## Verification / CLI smoke

```bash
go test ./internal/server/ -run Scheduler -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/jobs?jobId=daily" \
  -d '{"schedule":"0 9 * * 1","httpTarget":{"uri":"http://127.0.0.1:9/hook","httpMethod":"POST"}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/jobs/daily:run"
```
