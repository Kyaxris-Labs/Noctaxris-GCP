# Filestore

Lab Cloud Filestore Admin REST for instances. No NFS server is started;
`tier`, `fileShares`, and `networks` are theatre metadata only.

## Status

**lab** — location-scoped instance CRUD under a `/file/v1/` path prefix (no LRO).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/file/v1/projects/{p}/locations/{loc}/instances?instanceId={id}` (Instance body) |
| `GET` | `/file/v1/projects/{p}/locations/{loc}/instances` |
| `GET` | `/file/v1/projects/{p}/locations/{loc}/instances/{instance}` |
| `DELETE` | `/file/v1/projects/{p}/locations/{loc}/instances/{instance}` |

### Path prefix (Memorystore conflict)

Memorystore Redis already owns `/v1/projects/{p}/locations/{loc}/instances` on the
shared ServeMux. Identical patterns cannot share handlers, so Filestore mounts at
`/file/v1/...` (same idea as GCS `/storage/v1/`).

Point clients at a base that includes `/file/v1/` (hashicorp/google:
`filestore_custom_endpoint`; default BaseUrl is `https://file.googleapis.com/v1/`):

```bash
# Lab-specific: override must include /file/v1/ (not bare :4588/)
# filestore_custom_endpoint = "http://127.0.0.1:4588/file/v1/"
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/file/v1/projects/noctaxris-gcp-local/locations/us-central1-a/instances"
```

Official `file.googleapis.com/v1/...` shape is preserved after the `/file` prefix.

## Authz

Checked on `projects/{project}`:

- `file.instances.create|get|list|delete`

Seeded Service Usage: `file.googleapis.com`.

## Emulator limits

- No NFS binary / TCP listener; file share capacity and network modes are stored JSON only
- Create is synchronous (resource returned `READY`; no long-running Operation)
- No backups, snapshots, or replication APIs

## Deferred depth

- Backup / snapshot / restore surfaces
- Performance config and deletion-protection fidelity

## Verification / CLI smoke

```bash
go test ./internal/services/filestore/ ./internal/server/ -run Filestore -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/file/v1/projects/noctaxris-gcp-local/locations/us-central1-a/instances?instanceId=lab-nfs" \
  -d '{"tier":"BASIC_HDD","fileShares":[{"name":"share1","capacityGb":"1024"}],"networks":[{"network":"default","modes":["MODE_IPV4"]}]}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/file/v1/projects/noctaxris-gcp-local/locations/us-central1-a/instances/lab-nfs"
```

```bash
# Prefer filestore_custom_endpoint ending in /file/v1/ (Terraform LRO skip; see tests/terraform/README.md)
# gcloud api_endpoint_overrides/file alone (bare host) will miss /file/v1 on this lab.
```
