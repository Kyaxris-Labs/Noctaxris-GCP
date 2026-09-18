# Cloud Build

Emulator Cloud Build REST v1 for builds and triggers. createBuild stores
`WORKING` and runs steps on the nested engine (same family as Cloud Run
`:invoke`). getBuild returns the stored status. It does not flip WORKING to
SUCCESS by itself.

## Nested engine vs missing engine

| Mode | When | Behavior |
|------|------|----------|
| Nested | `NOCTAXRIS_GCP_DOCKER_HOST` set (Compose default) and step images pass `AllowImagePull` | `compute.Client.RunBuildStep` on TLS DinD. Host `docker.sock` / `unix://` / `npipe://` refused. |
| Missing engine | `NOCTAXRIS_GCP_DOCKER_HOST` empty and no test-injected runner | Status stays `WORKING` with `statusDetail` `nested engine not configured`. Never `SUCCESS`. Cancel still works. |
| Private pool `NO_PUBLIC_EGRESS` | `options.pool.name` set (default annotation `true` on pool create) | Step `args` / `env` / `script` http(s) URLs go through `httpegress.Validate`. Public WAN URLs fail the build. Loopback `:4588` (in-emulator GCS) is allowed. |

Step images must be pinned lab bases (`alpine:3.23` and the other entries in
`AllowImagePull`) or listed in
`NOCTAXRIS_GCP_IMAGE_PULL_ALLOWLIST`. Unallowlisted images fail the step.

## Status

**lab**: createBuild returns an unfinished Operation with `WORKING` build
metadata; getBuild echoes stored status; cancelBuild / retryBuild / project-scoped
trigger `:run`; triggers CRUD lite; worker pools with host-project
`cloudbuild.workerpools.use` and enforced `NO_PUBLIC_EGRESS`.

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
| `POST`/`GET`/`DELETE` | `/v1/projects/{p}/locations/{loc}/triggers[/{id}]` (shared mux with Eventarc; body shape selects Cloud Build vs Eventarc on create) |
| `POST` | `/v1/projects/{p}/locations/{loc}/triggers/{id}:run` |
| `POST`/`GET` | `/v1/projects/{p}/locations/{loc}/workerPools[/{pool}]` |

Triggers use classic project-scoped paths and regional `.../locations/.../triggers`.
Regional create dispatches by body: Eventarc-shaped (`eventFilters` /
`destination` / `transport` / `channel`) goes to Eventarc; otherwise Cloud Build.
Colon methods use `splitColonAction`.

`create` / `retry` / `:run` request body yields an Operation:

```json
{"name":"operations/...","done":false,"metadata":{"@type":"...BuildOperationMetadata","build":{"status":"WORKING","steps":[{"status":"WORKING",...}],...}}}
```

The Operation is always `WORKING` when returned. After the runner finishes,
getBuild is `SUCCESS` or `FAILURE` (or still `WORKING` if the nested engine is
missing). `cancel` returns `status=CANCELLED` with step statuses marked
`CANCELLED` (get does not advance cancelled builds). Request `substitutions`,
`logs`, `logUrl`, and `availableSecrets` persist on the build JSON and echo on
get.

## Authz

Checked on `projects/{project}`:

- `cloudbuild.builds.create|get|list|update` (`update` for cancel)
- `cloudbuild.triggers.create|get|list|delete`

`:run` requires `cloudbuild.builds.create`.

Worker pools live in a host project. Create stores `NO_PUBLIC_EGRESS=true` on
the pool. `createBuild` and `retryBuild` with `options.pool.name` require
`cloudbuild.workerpools.use` on that host project (not the caller project).
`:retry` copies the original build request (`BuildJSON`); a pooled retry whose
pool is missing is fail closed (`FailedPrecondition`). Private-pool public
egress is denied unless `NO_PUBLIC_EGRESS` is explicitly `false`. Default
httpegress already allows loopback `:4588` and denies the open internet unless
`NOCTAXRIS_GCP_HTTP_EGRESS=1` plus an exact allowlist.

## Emulator limits

- Missing nested engine never yields `SUCCESS`
- Nested step images are allowlisted only (no arbitrary `gcr.io/cloud-builders/*` pull unless listed)
- Host `docker.sock` is never mounted
- `:run` does not check out SCM; it creates a WORKING build and runs the same step path
- Logs URL is stored and echoed; there is no live stream
- Regional create shares the path with Eventarc (body-shape dispatch); list merges both inventories when authorized

## Deferred depth

- SCM webhooks, GitHub/GitLab triggers, and source fetch
- Build attestations and SLSA/provenance
- Build approvals
- Live log streaming

## Verification / CLI smoke

```bash
go test ./internal/services/cloudbuild/ ./internal/compute/ ./internal/kernel/httpegress/ ./internal/store/ -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
OP=$(curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/builds" \
  -d '{"steps":[{"name":"alpine:3.23","args":["true"]}]}')
echo "$OP"
# Extract build id from metadata.build.id. Without NOCTAXRIS_GCP_DOCKER_HOST, get stays WORKING:
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/builds/$BUILD_ID"
curl -s -H "Authorization: Bearer $TOKEN" -X POST \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/builds/$BUILD_ID:cancel" -d '{}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/triggers" \
  -d '{"name":"demo-trigger","filename":"cloudbuild.yaml"}'
curl -s -H "Authorization: Bearer $TOKEN" -X POST \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/triggers/demo-trigger:run" \
  -d '{"branchName":"main"}'
```
