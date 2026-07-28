# Cloud Functions

Lab Cloud Functions v2 REST control plane. Create without `storageSource` is
`ACTIVE` immediately. Create with `buildConfig.source.storageSource` starts
`DEPLOYING` until the generateUploadUrl path accepts a source zip, then flips to
`ACTIVE`. No Cloud Build; optional HTTP `:invoke` stub returns configured JSON.

## Status

**lab** — functions CRUD, patch merge, `generateUploadUrl` + source upload accept
theatre, function IAM get/set, `:invoke` stub.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v2/projects/{p}/locations/{loc}/functions?functionId=` |
| `POST` | `/v2/projects/{p}/locations/{loc}/functions:generateUploadUrl` |
| `PUT`/`POST` | `/v2/projects/{p}/locations/{loc}/functions:upload/{uploadId}` |
| `GET` | `/v2/projects/{p}/locations/{loc}/functions` |
| `GET` | `/v2/projects/{p}/locations/{loc}/functions/{fn}` |
| `PATCH` | `/v2/projects/{p}/locations/{loc}/functions/{fn}` |
| `DELETE` | `/v2/projects/{p}/locations/{loc}/functions/{fn}` |
| `POST`/`GET` | `/v2/projects/{p}/locations/{loc}/functions/{fn}:invoke` |
| `GET`/`POST` | `.../functions/{fn}:getIamPolicy` / `:setIamPolicy` |

Optional request field `labResponse` (object or string) sets the invoke body. `buildConfig` / `serviceConfig` / `labels` / `description` are echoed on get when present. Patch merges JSON fields into stored config.

`generateUploadUrl` returns a lab `uploadUrl` and `storageSource` object names.
`PUT`/`POST` to `uploadUrl` accepts body bytes (theatre; not executed) and
activates matching `DEPLOYING` functions.

## Authz

Checked on `projects/{project}`:

- `cloudfunctions.functions.create|get|list|update|delete|invoke|getIamPolicy|setIamPolicy`

## Emulator limits

- Upload accepts bytes only; no zip extract, no build, no runtime start
- Eventarc / Pub/Sub triggers are not wired from Functions create

## Deferred depth

- Real Cloud Build packaging and Eventarc triggers
- 1st gen API

## Verification / CLI smoke

```bash
go test ./internal/server/ -run CloudFunctions -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
UP=$(curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/functions:generateUploadUrl" \
  -d '{}')
echo "$UP"
# PUT body to uploadUrl from the response, then create with matching storageSource
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/functions?functionId=fn1" \
  -d '{"labResponse":{"result":"ok"}}'
```
