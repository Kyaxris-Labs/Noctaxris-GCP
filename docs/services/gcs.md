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
| Media PUT (lab) | `PUT /upload/storage/v1/b/{bucket}/o?uploadType=media&name=` (signed URL uploads) |
| Download | `GET .../o/{object}?alt=media` |
| V4 signed URL | `POST .../o/{object}:generateSignedUrl` + verify query signature on GET/PUT |
| Versioning | Each write creates a new generation; list/get default to latest |

Object bytes live under `$NOCTAXRIS_GCP_DATA_ROOT/gcs/{bucket}/...`. Metadata is in SQLite (`buckets`, `objects`).

### Authz

`storage.*` is evaluated against the bucket IAM resource `buckets/{name}` **or** the
project resource `projects/{projectId}` when the bucket is known (OR). Bucket IAM
documents are stored under `buckets/{name}` via get/set IAM.

### V4 signed URL theatre

`POST /storage/v1/b/{bucket}/o/{object}:generateSignedUrl` (Bearer required) body:

```json
{"method":"GET","expires":600,"alt":"media"}
```

`method` is `GET` or `PUT`. Response includes `signedUrl`, `algorithm=GOOG4-HMAC-SHA256`,
and lab `accessId=noctaxris-gcp-lab`.

Signing uses a fixed lab HMAC secret (`noctaxris-gcp-lab-hmac-secret`) and the
official V4 HMAC key-derivation / string-to-sign shape. This is not a real Cloud
Storage HMAC key or RSA service-account signature.

Requests that carry `X-Goog-Algorithm` + `X-Goog-Signature` query parameters may
omit `Authorization`. The GCS handler verifies the signature (host, path, method,
expiry) fail-closed before serving GET media or PUT media upload. Official Cloud
Storage signed URLs target the XML API; this lab verifies on the JSON/upload paths
returned by `:generateSignedUrl`.

## Emulator limits

- No object ACLs or object-level IAM documents (skipped; use bucket IAM)
- Soft delete, retention, and lifecycle are not enforced
- Multipart upload supports metadata JSON + media parts only
- Resumable uploads are single-chunk lab complete (no multi-chunk / status resume)
- Max upload body size in this lab: 64 MiB per request
- Compose is same-bucket only; max 32 sources
- Rewrite always finishes in one request (no rewriteToken continuation)
- Signed URLs use lab HMAC only (no RSA / IAM signBlob path)

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

SIGNED=$(curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"method":"GET","expires":600,"alt":"media"}' \
  "$EP/storage/v1/b/lab-bucket/o/hello.txt:generateSignedUrl" | jq -r .signedUrl)
curl -sS "$SIGNED"
```

Also: `go test ./internal/store/ ./internal/server/ -run 'TestGCS|Signed' -count=1`

## Deferred depth

- RSA (GOOG4-RSA-SHA256) signed URLs via IAM signBlob
- Multi-chunk resumable resume / status queries
- User-managed HMAC key CRUD
- Autoclass, soft delete / retention enforcement
- Object-level IAM and uniform bucket-level access edge cases
