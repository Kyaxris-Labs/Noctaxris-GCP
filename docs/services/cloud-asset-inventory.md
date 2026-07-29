# Cloud Asset Inventory

Lab Cloud Asset Inventory v1 REST that searches and lists resources already
stored by other lab services (projects, buckets, Pub/Sub topics, service
accounts). Export and feeds are theatre (no real GCS write or Pub/Sub fanout).

## Status

**lab (theatre)** — `searchAllResources`, `listAssets`, `exportAssets` (done LRO),
`batchGetAssetsHistory` (rows recorded on export), feeds CRUD lite.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`). Colon methods live in the
last path segment (ServeMux cannot embed `:`).

| Method | Path |
|--------|------|
| `GET` | `/v1/projects/{project}:searchAllResources` |
| `GET` | `/v1/folders/{folder}:searchAllResources` |
| `GET` | `/v1/organizations/{organization}:searchAllResources` |
| `GET` | `/v1/projects/{project}/assets` |
| `GET` | `/v1/folders/{folder}/assets` |
| `GET` | `/v1/organizations/{organization}/assets` |
| `POST` | `/v1/projects/{project}:exportAssets` |
| `POST` | `/v1/folders/{folder}:exportAssets` |
| `POST` | `/v1/organizations/{organization}:exportAssets` |
| `GET` | `/v1/projects/{project}:batchGetAssetsHistory` |
| `POST` | `/v1/projects/{project}/feeds?feedId=` |
| `GET` | `/v1/projects/{project}/feeds` |
| `GET` | `/v1/projects/{project}/feeds/{feed}` |
| `DELETE` | `/v1/projects/{project}/feeds/{feed}` |

Project-scoped `POST ...:exportAssets` is dispatched from Cloud Resource
Manager's existing `POST /v1/projects/{project}` colon handler (same pattern as
`getAncestry`) to avoid a ServeMux pattern conflict.

### Inventory sources

| Asset type | Source |
|------------|--------|
| `cloudresourcemanager.googleapis.com/Project` | CRM projects |
| `storage.googleapis.com/Bucket` | GCS buckets |
| `pubsub.googleapis.com/Topic` | Pub/Sub topics |
| `iam.googleapis.com/ServiceAccount` | IAM service accounts |

Folder/org scope searches all seeded projects (lab union). Query subset:
free-text substring, `name:` / `name=` field queries, and `assetTypes` (exact or
RE2 full-match). Page tokens are numeric offsets (lab).

### Export

`exportAssets` requires `outputConfig` and returns a completed Operation
(`done: true`) with echoed config and theatre `gcsResult.uris`. Matching assets
are appended to the feeds history table for `batchGetAssetsHistory`. No bytes
are written to Cloud Storage.

### Feeds

Store `name`, `assetTypes`, `contentType`, optional Pub/Sub topic. No live
change notifications.

## Authz

Checked on `projects/{project}` (or folder/org parent string for non-project
scopes):

- `cloudasset.assets.searchAllResources`
- `cloudasset.assets.listResource`
- `cloudasset.assets.exportResource`
- `cloudasset.feeds.create|get|list|delete`

`roles/viewer` covers search/list/feeds get+list. Export and feed mutate need
`roles/editor` / `roles/owner` (or a custom / `roles/cloudasset.*` grant).

Service Usage title: `cloudasset.googleapis.com` (not required to call these
routes in the lab).

## Emulator limits

- Inventory is synthesized from a fixed set of store tables (not a full CAI index)
- No real GCS/BigQuery export bytes; LRO completes immediately
- Feeds do not push to Pub/Sub
- No `searchAllIamPolicies`, `analyzeIamPolicy`, or `queryAssets`
- Folder/org scope does not walk CRM ancestry filters beyond union of projects

## Deferred depth

- Additional asset types (Secrets, Run, Functions, Compute, …)
- `searchAllIamPolicies` / IAM analyze
- Feed delivery workers
- Seed `cloudasset.googleapis.com` in EnsureRoot

## Verification / CLI smoke

```bash
go test ./internal/services/cloudasset/ ./internal/store/ -run 'CloudAsset|Inventory|SearchListExport' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local:searchAllResources"
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/assets"
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local:exportAssets" \
  -d '{"outputConfig":{"gcsDestination":{"uri":"gs://lab/export.json"}}}'
```

```bash
gcloud config set api_endpoint_overrides/cloudasset http://127.0.0.1:4588/
```
