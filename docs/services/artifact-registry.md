# Artifact Registry

REST v1 repository and package metadata plus Docker Registry HTTP API V2 on the
same loopback listener (`http://127.0.0.1:4588`). Blobs live in SQLite. There is
no host `docker.sock` and no nested engine requirement for pull.

## Status

**lab** — repository CRUD with label patch and IAM get/set; package and version
list/get/delete; `files.list` / `tags.list`; Docker Registry V2 ping, monolithic
blob upload, and manifest get/put.

## Wire protocol

REST v1 on `:4588`:

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/repositories?repositoryId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/repositories` |
| `GET`/`PATCH`/`DELETE` | `/v1/projects/{p}/locations/{loc}/repositories/{repo}` |
| `GET` or `POST` | `.../repositories/{repo}:getIamPolicy` |
| `POST` | `.../repositories/{repo}:setIamPolicy` |
| `GET` | `.../repositories/{repo}/files` |
| `POST` | `.../repositories/{repo}/packages?packageId=` |
| `GET`/`DELETE` | `.../packages/{pkg}` |
| `GET` | `.../packages` |
| `GET` | `.../packages/{pkg}/tags` (from version `relatedTags`) |
| `POST` | `.../packages/{pkg}/versions?versionId=` |
| `GET`/`DELETE` | `.../versions/{ver}` |
| `GET` | `.../versions` |

Create body may include `format` (default `DOCKER`), `description`, `labels`, and
`mode`. PATCH accepts `updateMask` for `description` and `labels`. Colon methods
use `splitColonAction`.

### Registry V2 (same `:4588`)

Go ServeMux `{name...}` is only legal as the last path element, so handlers are
`GET /v2` and `GET|POST|PUT /v2/{name...}` (empty `{name...}` is the `/v2/` ping).
GET patterns also match HEAD. The service splits `blobs` / `manifests` off the
remainder. `name` may be `project/repo/image` (several segments). Cloud Logging
keeps `/v2/entries:*` and `/v2/projects/...`; those literal routes win over the
registry catch-all.

| Method | Path | Result |
|--------|------|--------|
| `GET`/`HEAD` | `/v2/` | `200` `{}` plus `Docker-Distribution-API-Version: registry/2.0` |
| `POST` | `/v2/{name...}/blobs/uploads/` | `202` `Location` (uuid) |
| `PUT` | `/v2/{name...}/blobs/uploads/{uuid}?digest=sha256:...` | `201`; blob stored by digest |
| `HEAD`/`GET` | `/v2/{name...}/blobs/{digest}` | blob bytes |
| `PUT` | `/v2/{name...}/manifests/{reference}` | `201` plus `Docker-Content-Digest` |
| `HEAD`/`GET` | `/v2/{name...}/manifests/{reference}` | tag or digest |

A manifest PUT upserts v1 package/version rows so `listPackages` /
`listVersions` / `listFiles` show the digest. `files.list` `sizeBytes` is the
stored blob length when that digest exists; otherwise `"0"`.

## Authz

v1 methods are checked on `projects/{project}`:

- `artifactregistry.repositories.create|get|list|update|delete|getIamPolicy|setIamPolicy`
- `artifactregistry.packages.create|get|list|delete`
- `artifactregistry.versions.create|get|list|delete`
- `artifactregistry.files.list`
- `artifactregistry.tags.list`

Registry V2 uses the same Bearer principal as v1 (`PrincipalFromContext`). Root
skips IAM. Missing Bearer is `401` with `WWW-Authenticate: Bearer`. Denied is
`403`.

The first path segment of `name` is the project when it matches a CRM project
row; otherwise the default project is `noctaxris-gcp-local`.

| Access | Permissions (OR) |
|--------|------------------|
| pull (`GET`/`HEAD` `/v2/`, manifests, blobs) | `artifactregistry.repositories.downloadArtifacts` or `artifactregistry.dockerimages.get` |
| push (`POST` uploads, `PUT` blob/manifest) | `artifactregistry.repositories.uploadArtifacts` or `artifactregistry.dockerimages.create` |

`GET /v2/` with a valid Bearer returns `200` on an empty registry (docker login
ping) when pull is allowed.

## Emulator limits

- No vulnerability scanning
- No garbage collection
- No chunked `PATCH` blob uploads (monolithic `PUT` after `POST` only)
- No remote or virtual repositories
- Maven/npm/Python formats stay metadata-only
- Blobs are capped at 32 MiB

## Deferred depth

- Chunked uploads and cross-repo blob mounts
- Cleanup policies and vulnerability scanning
- Maven/npm/Python push/pull beyond metadata create

## Verification / CLI smoke

```bash
go test ./internal/services/artifactregistry/ ./internal/store/ -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/repositories?repositoryId=lab" \
  -d '{"format":"DOCKER","description":"lab","labels":{"env":"lab"}}'
curl -sI -H "Authorization: Bearer $TOKEN" http://127.0.0.1:4588/v2/
BLOB=hello-layer
DIGEST="sha256:$(printf %s "$BLOB" | sha256sum | awk '{print $1}')"
LOC=$(curl -sI -X POST -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v2/noctaxris-gcp-local/lab/hello/blobs/uploads/" \
  | awk -F': ' 'tolower($1)=="location"{gsub("\r","",$2); print $2}')
curl -s -X PUT -H "Authorization: Bearer $TOKEN" \
  --data-binary "$BLOB" "${LOC}?digest=$DIGEST"
curl -s -H "Authorization: Bearer $TOKEN" \
  -X PUT "http://127.0.0.1:4588/v2/noctaxris-gcp-local/lab/hello/manifests/latest" \
  -H "Content-Type: application/vnd.docker.distribution.manifest.v2+json" \
  -d "{\"schemaVersion\":2}"
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/repositories/lab/files"
```
