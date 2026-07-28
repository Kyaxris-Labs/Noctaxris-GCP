# Dataflow

Lab Cloud Dataflow REST for job create/get/list theatre. There are no workers,
Flex Templates, streaming runners, or pipeline execution.

## Status

**lab** — regional jobs create/get/list plus project-level jobs list.
Create returns `currentState=JOB_STATE_RUNNING`. Get advances
`JOB_STATE_RUNNING` → `JOB_STATE_DONE` (subsequent gets stay `DONE`).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1b3/projects/{p}/locations/{loc}/jobs` |
| `GET` | `/v1b3/projects/{p}/locations/{loc}/jobs` |
| `GET` | `/v1b3/projects/{p}/locations/{loc}/jobs/{jobId}` |
| `GET` | `/v1b3/projects/{p}/jobs` (all locations) |

Create body fields used: `name`, `type` (default `JOB_TYPE_BATCH`); other JSON
fields are stored and returned. Server sets `id`, `projectId`, `location`,
`currentState`, `currentStateTime`, `createTime`, `startTime`.
Colon methods use `splitAction` (none mounted).

## Authz

Checked on `projects/{project}`:

- `dataflow.jobs.create|get|list`

Seeded Service Usage: `dataflow.googleapis.com`.

## Emulator limits

- No worker VMs, Flex Templates, streaming, snapshots, or metrics
- No `jobs.update` cancel/drain; state advances only via get theatre
- Steps / environment are stored as opaque JSON when present; never executed

## Deferred depth

- `jobs.update` requestedState transitions, templates launch, job messages/metrics
- Streaming and Flex Template execution paths

## Verification / CLI smoke

```bash
go test ./internal/services/dataflow/ ./internal/store/ ./internal/server/ -run Dataflow -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
JOB=$(curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1b3/projects/noctaxris-gcp-local/locations/us-central1/jobs" \
  -d '{"name":"lab-batch","type":"JOB_TYPE_BATCH"}')
echo "$JOB"
# Extract id, then get (advances RUNNING → DONE):
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1b3/projects/noctaxris-gcp-local/locations/us-central1/jobs/$JOB_ID"
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1b3/projects/noctaxris-gcp-local/jobs"
```

```bash
gcloud config set api_endpoint_overrides/dataflow http://127.0.0.1:4588/
```
