# Cloud Functions

Lab Cloud Functions v2 REST control plane. State is always `ACTIVE` theatre. No Cloud Build; optional HTTP `:invoke` stub returns configured JSON. Source upload is URL theatre only.

## Status

**lab** — functions CRUD, patch merge, `generateUploadUrl` theatre, function IAM get/set, `:invoke` stub.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v2/projects/{p}/locations/{loc}/functions?functionId=` |
| `POST` | `/v2/projects/{p}/locations/{loc}/functions:generateUploadUrl` |
| `GET` | `/v2/projects/{p}/locations/{loc}/functions` |
| `GET` | `/v2/projects/{p}/locations/{loc}/functions/{fn}` |
| `PATCH` | `/v2/projects/{p}/locations/{loc}/functions/{fn}` |
| `DELETE` | `/v2/projects/{p}/locations/{loc}/functions/{fn}` |
| `POST`/`GET` | `/v2/projects/{p}/locations/{loc}/functions/{fn}:invoke` |
| `GET`/`POST` | `.../functions/{fn}:getIamPolicy` / `:setIamPolicy` |

Optional request field `labResponse` (object or string) sets the invoke body. `buildConfig` / `serviceConfig` / `labels` / `description` are echoed on get when present. Patch merges JSON fields into stored config.

`generateUploadUrl` returns a lab `uploadUrl` and `storageSource` object names (no real GCS upload required for control-plane tests).

## Authz

Checked on `projects/{project}`:

- `cloudfunctions.functions.create|get|list|update|delete|invoke|getIamPolicy|setIamPolicy`

## Deferred depth

- Real source upload / build / Eventarc triggers
- 1st gen API

## Verification / CLI smoke

```bash
go test ./internal/server/ -run CloudFunctions -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/functions:generateUploadUrl" \
  -d '{}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/functions?functionId=fn1" \
  -d '{"labResponse":{"result":"ok"}}'
```
