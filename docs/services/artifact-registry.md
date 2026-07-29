# Artifact Registry

Lab Artifact Registry REST v1 for repositories, packages, versions, file/tag metadata theatre, and repository IAM. Metadata only: no blob/image storage, no docker push/pull, no host `docker.sock`.

## Status

**lab** — repository CRUD with label patch + IAM get/set; package and version list/get/delete plus lab metadata create; listFiles and listTags derived from version metadata.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/repositories?repositoryId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/repositories` |
| `GET`/`PATCH`/`DELETE` | `/v1/projects/{p}/locations/{loc}/repositories/{repo}` |
| `GET` or `POST` | `.../repositories/{repo}:getIamPolicy` |
| `POST` | `.../repositories/{repo}:setIamPolicy` |
| `GET` | `.../repositories/{repo}/files` (metadata theatre from versions) |
| `POST` | `.../repositories/{repo}/packages?packageId=` (lab metadata publish) |
| `GET`/`DELETE` | `.../packages/{pkg}` |
| `GET` | `.../packages` |
| `GET` | `.../packages/{pkg}/tags` (from version `relatedTags`) |
| `POST` | `.../packages/{pkg}/versions?versionId=` (lab metadata publish) |
| `GET`/`DELETE` | `.../versions/{ver}` |
| `GET` | `.../versions` |

Create body may include `format` (default `DOCKER`), `description`, `labels`, and `mode`. PATCH accepts `updateMask` for `description` and `labels`. Colon methods use `splitColonAction` (never `{id}:action` ServeMux patterns).

## Authz

Checked on `projects/{project}`:

- `artifactregistry.repositories.create|get|list|update|delete|getIamPolicy|setIamPolicy`
- `artifactregistry.packages.create|get|list|delete`
- `artifactregistry.versions.create|get|list|delete`
- `artifactregistry.files.list`
- `artifactregistry.tags.list`

## Emulator limits

- No OCI/Docker blob storage or registry protocol (`docker push` / `pull`)
- `files.list` and `tags.list` are metadata theatre only (sizeBytes always `"0"`; no real layers)
- No vulnerability scanning, remote/virtual repos, or cleanup policy execution
- Package IDs with embedded `/` are not multi-segment path theatre

## Deferred depth

- Docker/OCI registry protocol, blob upload/download, and image layers
- Remote/virtual repositories, cleanup policies, and vulnerability scanning
- Maven/npm/Python format push/pull beyond metadata create

## Verification / CLI smoke

```bash
go test ./internal/services/artifactregistry/ ./internal/server/ -run ArtifactRegistry -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/repositories?repositoryId=lab" \
  -d '{"format":"DOCKER","description":"lab","labels":{"env":"lab"}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X PATCH "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/repositories/lab?updateMask=labels" \
  -d '{"labels":{"env":"prod"}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/repositories/lab:getIamPolicy"
```
