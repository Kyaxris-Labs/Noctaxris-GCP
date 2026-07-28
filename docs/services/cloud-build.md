# Cloud Build

Lab Cloud Build REST v1 for builds and triggers. CreateBuild is status theatre only: no container steps, no image pulls/pushes, no host `docker.sock`, no DinD.

## Status

**lab** — createBuild returns an unfinished Operation with `WORKING` build metadata; getBuild advances to `SUCCESS`; cancelBuild / retryBuild / project-scoped trigger `:run` theatre. Triggers CRUD lite (create/get/list/delete); no webhook execution.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/builds` |
| `GET` | `/v1/projects/{p}/builds` |
| `GET` | `/v1/projects/{p}/builds/{id}` |
| `POST` | `/v1/projects/{p}/builds/{id}:cancel` |
| `POST` | `/v1/projects/{p}/builds/{id}:retry` |
| `POST`/`GET` | `/v1/projects/{p}/locations/{loc}/builds` (+ `/{id}`, `/{id}:cancel`, `/{id}:retry`) |
| `POST`/`GET` | `/v1/projects/{p}/triggers` |
| `GET`/`DELETE` | `/v1/projects/{p}/triggers/{id}` |
| `POST` | `/v1/projects/{p}/triggers/{id}:run` |

Triggers use the classic project-scoped Cloud Build paths (`projects.triggers`). Regional
`.../locations/{loc}/triggers` is not mounted here so it does not collide with Eventarc on
the shared listener. Colon methods use `splitColonAction`.

`create` / `retry` / `:run` request body yields an Operation:

```json
{"name":"operations/...","done":false,"metadata":{"@type":"...BuildOperationMetadata","build":{"status":"WORKING",...}}}
```

`cancel` returns the Build with `status=CANCELLED` (get does not advance cancelled builds to SUCCESS).

## Authz

Checked on `projects/{project}`:

- `cloudbuild.builds.create|get|list|update` (`update` for cancel)
- `cloudbuild.triggers.create|get|list|delete`

`:run` requires `cloudbuild.builds.create`.

## Emulator limits

- Steps are never executed; images are never pulled or pushed
- `:run` creates a WORKING build theatre only (no SCM checkout, no webhook delivery)
- No worker pools, approvals, or real SCM webhooks
- Logs URL is a lab string only
- Regional `projects.locations.triggers` is omitted (Eventarc owns `.../locations/.../triggers` on this mux)

## Verification / CLI smoke

```bash
go test ./internal/services/cloudbuild/ ./internal/server/ -run CloudBuild -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
OP=$(curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/builds" \
  -d '{"steps":[{"name":"gcr.io/cloud-builders/docker","args":["build","."]}]}')
echo "$OP"
# Extract build id from metadata.build.id, then cancel or get:
curl -s -H "Authorization: Bearer $TOKEN" -X POST \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/builds/$BUILD_ID:cancel" -d '{}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/triggers" \
  -d '{"name":"lab-trigger","filename":"cloudbuild.yaml"}'
curl -s -H "Authorization: Bearer $TOKEN" -X POST \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/triggers/lab-trigger:run" \
  -d '{"branchName":"main"}'
```
