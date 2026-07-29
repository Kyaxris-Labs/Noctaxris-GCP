# Memorystore for Redis

Lab Memorystore Redis Admin REST for instances. Default Compose leaves
`NOCTAXRIS_GCP_DOCKER_HOST` empty: `host` / `port` are theatre metadata for
control-plane-only clients. With the opt-in DinD engine (`compose.engine.yaml`),
create attempts a nested `redis:7-alpine` container on the internal
`noctaxris-gcp-data` network; the API `host` is the container DNS name
`noctaxris-gcp-redis-<instanceId>` (port `6379`). Nested Redis is not published
to the operator host.

## Status

**hybrid lab** — location-scoped instance CRUD; nested Redis when DinD is
configured; metadata theatre otherwise.

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

- Without DinD: no Redis TCP listener; `host` is `{instanceId}.{location}.redis.noctaxris-gcp.lab`, `port` `6379`
- With DinD: Redis listens only on `noctaxris-gcp-data` (no host publish); nested ensure soft-fails back to theatre `host` when the engine is unreachable
- Create is synchronous (resource returned ready; no long-running Operation)
- No AUTH, import/export, failover, or maintenance APIs

## Deferred depth

- Redis Cluster / Memorystore for Valkey surfaces
- Connect-mode / VPC / CMEK fidelity
- Host publish overlay for operator-loopback Redis clients (not default)

## Verification / CLI smoke

```bash
go test ./internal/services/memorystore/ ./internal/store/ -run 'Memorystore|BigtableAndMemorystore' -count=1
go test ./internal/compute/ -run 'AllowImagePull|MemorystoreRedis' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/instances?instanceId=lab-redis" \
  -d '{"tier":"BASIC","memorySizeGb":1,"displayName":"Lab Redis"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/instances/lab-redis"
```

With `compose.engine.yaml`, confirm `host` is `noctaxris-gcp-redis-lab-redis` and
reach Redis from another container on `noctaxris-gcp-data` (not from the host
unless you add a custom publish overlay).

```bash
gcloud config set api_endpoint_overrides/redis http://127.0.0.1:4588/
```
