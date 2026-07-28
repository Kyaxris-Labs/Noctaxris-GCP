# Cloud Storage

**Status:** lab

JSON API v1 emulator for buckets and objects. Clients that honor
`STORAGE_EMULATOR_HOST` talk to the same host and port as the rest of
Noctaxris-GCP (`127.0.0.1:4588` by default).

## Implemented

| Area | Methods |
|------|---------|
| Buckets | `POST /storage/v1/b?project=`, `GET /storage/v1/b/{bucket}`, `GET /storage/v1/b?project=`, `PATCH /storage/v1/b/{bucket}`, `DELETE /storage/v1/b/{bucket}` |
| Bucket IAM | `GET` / `PUT /storage/v1/b/{bucket}/iam`, `GET .../iam/testPermissions` |
| Objects | `GET` / `PATCH` / `DELETE /storage/v1/b/{bucket}/o/{object}`, `GET /storage/v1/b/{bucket}/o` |
| List | `prefix` and `delimiter` (common prefixes / directory theatre) |
| Preconditions | `ifGenerationMatch` on object GET and DELETE |
| Compose | `POST .../o/{dest}/compose` (max 32 sources) |
| Copy | `POST .../o/{src}/copyTo/b/{dstBucket}/o/{dstObject}` |
| Rewrite | `POST .../o/{src}/rewriteTo/b/{dstBucket}/o/{dstObject}` (single-shot `done=true`) |
| Upload | `POST /upload/storage/v1/b/{bucket}/o` (`uploadType=media`, `multipart`, `resumable`) |
| Resumable | Initiate returns `Location`; `PUT` that URI completes a lab single-chunk upload; `DELETE` cancels |
| Download | `GET .../o/{object}?alt=media` |
| Versioning | Each write creates a new generation; list/get default to latest |

Object bytes live under `$NOCTAXRIS_GCP_DATA_ROOT/gcs/{bucket}/...`. Metadata is in SQLite (`buckets`, `objects`).

### Authz

`storage.*` is evaluated against the bucket IAM resource `buckets/{name}` **or** the
project resource `projects/{projectId}` when the bucket is known (OR). Bucket IAM
documents are stored under `buckets/{name}` via get/set IAM.

## Emulator limits

- No object ACLs or object-level IAM documents (skipped; use bucket IAM)
- Soft delete, retention, and lifecycle are not enforced
- Multipart upload supports metadata JSON + media parts only
- Resumable uploads are single-chunk lab complete (no multi-chunk / status resume)
- Max upload body size in this lab: 64 MiB per request
- Compose is same-bucket only; max 32 sources
- Rewrite always finishes in one request (no rewriteToken continuation)

## Pointing clients

```bash
export STORAGE_EMULATOR_HOST=127.0.0.1:4588
export GOOGLE_CLOUD_PROJECT=noctaxris-gcp-local
```

gcloud:

```bash
gcloud config set api_endpoint_overrides/storage http://127.0.0.1:4588/
```

Terraform (Google provider):

```hcl
provider "google" {
  storage_custom_endpoint = "http://127.0.0.1:4588/storage/v1/"
}
```

## Verification / CLI smoke

```bash
export TOKEN="$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN"
export EP=http://127.0.0.1:4588
export PROJECT=noctaxris-gcp-local

curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"lab-bucket","location":"US"}' \
  "$EP/storage/v1/b?project=$PROJECT"

curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: text/plain" \
  --data-binary 'hello' \
  "$EP/upload/storage/v1/b/lab-bucket/o?uploadType=media&name=hello.txt"

curl -sS -H "Authorization: Bearer $TOKEN" \
  "$EP/storage/v1/b/lab-bucket/o/hello.txt?alt=media"

curl -sS -H "Authorization: Bearer $TOKEN" \
  "$EP/storage/v1/b/lab-bucket/o?delimiter=/"

curl -sS -i -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"contentType":"text/plain"}' \
  "$EP/upload/storage/v1/b/lab-bucket/o?uploadType=resumable&name=resumable.txt"
```

Also: `go test ./internal/store/ ./internal/server/ -run 'TestGCS'`

## Deferred depth

- Signed URL RSA (V2/V4) generation and verification
- Multi-chunk resumable resume / status queries
- HMAC keys, Autoclass, soft delete / retention enforcement
- Object-level IAM and uniform bucket-level access edge cases
