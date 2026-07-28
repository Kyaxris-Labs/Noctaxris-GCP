# Artifact Registry

Lab Artifact Registry REST v1 for repositories, packages, and versions. Metadata only: no blob/image storage, no docker push/pull, no host `docker.sock`.

## Status

**lab** — repository CRUD; package and version list/get/delete plus lab metadata create for smoke; format field theatre (`DOCKER`, etc.).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/repositories?repositoryId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/repositories` |
| `GET`/`PATCH`/`DELETE` | `/v1/projects/{p}/locations/{loc}/repositories/{repo}` |
| `POST` | `.../repositories/{repo}/packages?packageId=` (lab metadata publish) |
| `GET`/`DELETE` | `.../packages/{pkg}` |
| `GET` | `.../packages` |
| `POST` | `.../packages/{pkg}/versions?versionId=` (lab metadata publish) |
| `GET`/`DELETE` | `.../versions/{ver}` |
| `GET` | `.../versions` |

Create body may include `format` (default `DOCKER`), `description`, `labels`, and `mode`.

## Authz

Checked on `projects/{project}`:

- `artifactregistry.repositories.create|get|list|update|delete`
- `artifactregistry.packages.create|get|list|delete`
- `artifactregistry.versions.create|get|list|delete`

## Emulator limits

- No OCI/Docker blob storage or registry protocol (`docker push` / `pull`)
- No vulnerability scanning, remote/virtual repos, or cleanup policy execution
- Package IDs with embedded `/` are not multi-segment path theatre

## Verification / CLI smoke

```bash
go test ./internal/server/ -run ArtifactRegistry -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/repositories?repositoryId=lab" \
  -d '{"format":"DOCKER","description":"lab"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/repositories/lab/packages?packageId=hello" \
  -d '{"displayName":"hello"}'
```
