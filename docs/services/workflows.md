# Workflows

Lab Cloud Workflows REST for workflow CRUD and execution theatre. Source is stored as a string; there is no workflow engine.

## Status

**lab** — workflows create/get/list/delete; executions create/get/list with immediate `SUCCEEDED` result JSON.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/workflows?workflowId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/workflows` |
| `GET` | `/v1/projects/{p}/locations/{loc}/workflows/{workflow}` |
| `DELETE` | `/v1/projects/{p}/locations/{loc}/workflows/{workflow}` |
| `POST` | `.../workflows/{workflow}/executions` |
| `GET` | `.../workflows/{workflow}/executions` |
| `GET` | `.../workflows/{workflow}/executions/{execution}` |

Create body fields used: `sourceContents`, `description`, `serviceAccount`, `labels`. Execution body: `argument` (JSON string). Create returns the Workflow / Execution resource directly (no LRO).

## Authz

Checked on `projects/{project}`:

- `workflows.workflows.create|get|list|delete`
- `workflows.executions.create|get|list`

## Emulator limits

- No YAML/JSON workflow interpreter; executions always finish as `SUCCEEDED` with lab result JSON
- No patch / listRevisions / cancel / callbacks / connectors
- Create is synchronous (no long-running Operation)

## Verification / CLI smoke

```bash
go test ./internal/services/workflows/ ./internal/server/ -run Workflows -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/workflows?workflowId=demo" \
  -d '{"sourceContents":"main:\n  steps:\n    - done:\n        return: ok\n"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/workflows/demo/executions" \
  -d '{"argument":"{}"}'
```

```bash
gcloud config set api_endpoint_overrides/workflows http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/workflowexecutions http://127.0.0.1:4588/
```
