# Workflows

Lab Cloud Workflows REST for workflow CRUD (including patch) and execution theatre. Source is stored as a string; there is no workflow engine.

## Status

**lab** — workflows create/get/list/patch/delete; executions create/get/list/cancel with `ACTIVE`→`SUCCEEDED` on get (or `:cancel` → `CANCELLED`); argument must be valid JSON; list supports `pageSize`.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/workflows?workflowId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/workflows` (`pageSize`, `pageToken`) |
| `GET` | `/v1/projects/{p}/locations/{loc}/workflows/{workflow}` |
| `PATCH` | `/v1/projects/{p}/locations/{loc}/workflows/{workflow}` (`updateMask` optional) |
| `DELETE` | `/v1/projects/{p}/locations/{loc}/workflows/{workflow}` |
| `POST` | `.../workflows/{workflow}/executions` |
| `GET` | `.../workflows/{workflow}/executions` (`pageSize`, `pageToken`) |
| `GET` | `.../workflows/{workflow}/executions/{execution}` |
| `POST` | `.../workflows/{workflow}/executions/{execution}:cancel` |

Create body fields used: `sourceContents`, `description`, `serviceAccount`, `labels`. PATCH may bump `revisionId` when `sourceContents` changes. Execution body: `argument` (JSON string; rejected if not valid JSON). Create returns the Workflow / Execution resource directly (no LRO). Colon methods use `splitAction`.

## Authz

Checked on `projects/{project}`:

- `workflows.workflows.create|get|list|update|delete`
- `workflows.executions.create|get|list|cancel`

## Emulator limits

- No YAML/JSON workflow interpreter; get advances `ACTIVE` executions to `SUCCEEDED` with lab result JSON
- Cancel only applies to `ACTIVE`/`QUEUED` executions
- Patch returns the Workflow resource (real API returns a long-running Operation)
- No listRevisions / callbacks / connectors

## Verification / CLI smoke

```bash
go test ./internal/services/workflows/ ./internal/server/ -run Workflows -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/workflows?workflowId=demo" \
  -d '{"sourceContents":"main:\n  steps:\n    - done:\n        return: ok\n"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X PATCH "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/workflows/demo?updateMask=description" \
  -d '{"description":"updated"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/workflows/demo/executions" \
  -d '{"argument":"{}"}'
```

```bash
gcloud config set api_endpoint_overrides/workflows http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/workflowexecutions http://127.0.0.1:4588/
```
