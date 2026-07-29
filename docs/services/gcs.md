# Cloud Storage

**Status:** lab

JSON API v1 emulator for buckets and objects. Clients that honor
`STORAGE_EMULATOR_HOST` talk to the same host and port as the rest of
Noctaxris-GCP (`127.0.0.1:4588` by default).

## Implemented

| Area | Methods |
|------|---------|
| Buckets | `POST /storage/v1/b?project=`, `GET /storage/v1/b/{bucket}`, `GET /storage/v1/b?project=`, `PATCH /storage/v1/b/{bucket}`, `DELETE /storage/v1/b/{bucket}` |
| Retention | Bucket `retentionPolicy` (`retentionPeriod`, `isLocked`, `effectiveTime`); delete/overwrite fail closed while object age < period |
| Bucket IAM | `GET` / `PUT /storage/v1/b/{bucket}/iam`, `GET .../iam/testPermissions` |
| Notifications | `POST` / `GET` / `DELETE /storage/v1/b/{bucket}/notificationConfigs[/{id}]` (Pub/Sub `OBJECT_FINALIZE` / `OBJECT_DELETE`) |
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

Object bytes live under `$NOCTAXRIS_GCP_DATA_ROOT/gcs/{bucket}/...`. Metadata is in SQLite (`buckets`, `objects`, `gcs_notification_configs`).

### Pub/Sub notificationConfigs

Classic bucket notifications (JSON API snake_case resource fields) publish to a lab
Pub/Sub topic on object finalize and delete:

| Method | Path | Authz |
|--------|------|-------|
| insert | `POST .../b/{bucket}/notificationConfigs` | `storage.buckets.update` |
| list | `GET .../b/{bucket}/notificationConfigs` | `storage.buckets.get` |
| get | `GET .../b/{bucket}/notificationConfigs/{id}` | `storage.buckets.get` |
| delete | `DELETE .../b/{bucket}/notificationConfigs/{id}` | `storage.buckets.update` |

Request body (insert) uses Google JSON API fields: `topic` (required;
`//pubsub.googleapis.com/projects/{p}/topics/{t}` or `projects/{p}/topics/{t}`),
`payload_format` (`JSON_API_V1` or `NONE`), optional `event_types`,
`object_name_prefix`, `custom_attributes`. Empty `event_types` means all supported
events. Create/delete bumps bucket `metageneration`.

On `PutObjectBytes` finalize and `DeleteObject`, matching configs best-effort
`Publish` to the topic with standard attributes (`eventType`, `bucketId`,
`objectId`, `objectGeneration`, `notificationConfig`, …) and a `JSON_API_V1`
object metadata payload when configured. Missing topics are skipped.

**Publisher IAM theatre:** delivery does not evaluate `pubsub.topics.publish` as a
GCS service agent. Topic publish authz applies only on the Pub/Sub API path.
Eventarc finalize hooks remain separate and unchanged.

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
- Soft delete and lifecycle are not enforced
- Bucket retention is lab-lite: `retentionPeriod` is a minimum object age before delete or overwrite of the same name; locked policies reject shortening or clearing; no event-based hold, temporary hold, or per-object retention
- Multipart upload supports metadata JSON + media parts only
- Resumable uploads are single-chunk lab complete (no multi-chunk / status resume)
- Max upload body size in this lab: 64 MiB per request
- Compose is same-bucket only; max 32 sources
- Rewrite always finishes in one request (no rewriteToken continuation)
- Signed URLs use lab HMAC only (no RSA / IAM signBlob path)
- NotificationConfigs: `OBJECT_FINALIZE` + `OBJECT_DELETE` only (no ARCHIVE / METADATA_UPDATE / INITIALIZE); no GCS SA publisher IAM on deliver
- When Organization Policy constraint `storage.publicAccessPrevention` is enforced on the project (or ancestor), bucket `setIamPolicy` with `allUsers` / `allAuthenticatedUsers` returns `FAILED_PRECONDITION` (see [orgpolicy.md](orgpolicy.md))

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

TOPIC="//pubsub.googleapis.com/projects/$PROJECT/topics/gcs-events"
# Create the Pub/Sub topic first (gRPC or REST), then:
curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"topic\":\"$TOPIC\",\"payload_format\":\"JSON_API_V1\",\"event_types\":[\"OBJECT_FINALIZE\"]}" \
  "$EP/storage/v1/b/lab-bucket/notificationConfigs"
```

Also: `go test ./internal/services/gcs/ ./internal/store/ -run 'GCS|Signed|Retention|Notification' -count=1`

## Deferred depth

- RSA (GOOG4-RSA-SHA256) signed URLs via IAM signBlob
- Multi-chunk resumable resume / status queries
- User-managed HMAC key CRUD
- Autoclass, soft delete, event-based / temporary hold, per-object retention
- Object-level IAM and uniform bucket-level access edge cases
- GCS service-agent `pubsub.topics.publish` fail-closed on notification deliver
- `OBJECT_ARCHIVE` / `OBJECT_METADATA_UPDATE` / `OBJECT_INITIALIZE` notification events
