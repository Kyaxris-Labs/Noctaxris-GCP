# Memorystore for Redis

Lab Memorystore Redis Admin REST for instances. No Redis process is started;
`host` / `port` are theatre fields for clients that only need control-plane shape.

## Status

**lab** — location-scoped instance CRUD; stores `tier`, `memorySizeGb`, theatre
`host`, and `state=READY` (no LRO).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/instances?instanceId={id}` (Instance body) |
| `GET` | `/v1/projects/{p}/locations/{loc}/instances` |
| `GET` | `/v1/projects/{p}/locations/{loc}/instances/{instance}` |
| `DELETE` | `/v1/projects/{p}/locations/{loc}/instances/{instance}` |

Paths are location-scoped. Do not use bare `/v1/projects/{p}/instances` — Spanner
owns that shape.

## Authz

Checked on `projects/{project}`:

- `redis.instances.create|get|list|delete`

Seeded Service Usage: `redis.googleapis.com`.

## Emulator limits

- No Redis binary / TCP listener; `host` is a lab string, `port` defaults to `6379`
- Create is synchronous (resource returned ready; no long-running Operation)
- No AUTH, import/export, failover, or maintenance APIs

## Deferred depth

- Redis Cluster / Memorystore for Valkey surfaces
- Connect-mode / VPC / CMEK fidelity

## Verification / CLI smoke

```bash
go test ./internal/services/memorystore/ ./internal/server/ -run Memorystore -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/instances?instanceId=lab-redis" \
  -d '{"tier":"BASIC","memorySizeGb":1,"displayName":"Lab Redis"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/instances/lab-redis"
```

```bash
gcloud config set api_endpoint_overrides/redis http://127.0.0.1:4588/
```
