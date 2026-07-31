# Memorystore for Redis

Lab Memorystore Redis Admin REST for instances. Default Compose leaves
`NOCTAXRIS_GCP_DOCKER_HOST` empty: `host` / `port` are theatre metadata for
control-plane-only clients. With default Compose DinD (or any configured Docker host),
create attempts a nested `redis:7-alpine` container on the shared
`noctaxris-gcp-lab` bridge (same network as nested Cloud SQL and Managed Kafka);
the API `host` is the container DNS name `noctaxris-gcp-redis-<instanceId>`
(port `6379`). Nested Redis is not published to the operator host.

## Status

**hybrid lab** — location-scoped instance CRUD; nested Redis when DinD is
configured; metadata theatre otherwise. Create and delete return a completed
Operation (`done: true`; create also includes `response`).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/instances?instanceId={id}` (Instance body) |
| `GET` | `/v1/projects/{p}/locations/{loc}/instances` |
| `GET` | `/v1/projects/{p}/locations/{loc}/instances/{instance}` |
| `DELETE` | `/v1/projects/{p}/locations/{loc}/instances/{instance}` |
| `GET` | `/v1/projects/{p}/locations/{loc}/operations/{operation}` |

Paths are location-scoped. Do not use bare `/v1/projects/{p}/instances` — Spanner
owns that shape.

Create returns a completed Operation:

```json
{"name":"projects/.../locations/.../operations/create-{id}","done":true,"response":{"@type":"type.googleapis.com/google.cloud.redis.v1.Instance","name":"...","state":"READY",...}}
```

Delete returns a completed Operation (`done: true`) so Terraform destroy waiters
can finish; the instance is removed from store.

`GET` of the instance by resource name still returns the instance.
`GET .../operations/{operation}` returns `{name, done: true}` immediately.
The operations path is shared with Certificate Manager on the lab ServeMux
(first Mount wins; both return done theatre).

## Authz

Checked on `projects/{project}`:

- `redis.instances.create|get|list|delete`
- `redis.operations.get` (falls back to `redis.instances.get`)

Seeded Service Usage: `redis.googleapis.com`.

## Emulator limits

- Without DinD: no Redis TCP listener; `host` is `{instanceId}.{location}.redis.noctaxris-gcp.lab`, `port` `6379`
- With DinD: Redis listens only on `noctaxris-gcp-lab` (shared with SQL/Kafka; no host publish); nested ensure soft-fails back to theatre `host` when the engine is unreachable unless `NOCTAXRIS_GCP_NESTED_ENGINE_FAIL_CLOSED=1`/`true` (create returns `FAILED_PRECONDITION` and the instance row is rolled back)
- Create accepts `authEnabled` / optional `authString`; when `authEnabled` is true and `authString` is empty, a UUID AUTH string is generated. Create/get JSON echoes `authEnabled` and (when enabled) `authString` (lab convenience; GCP uses `getAuthString`)
- Nested Redis with AUTH: `REDIS_PASSWORD` env plus `redis-server --requirepass` on `redis:7-alpine`
- Create and delete return a completed LRO (`done: true`); Operations.get is immediate done theatre (no async worker)
- No import/export, failover, maintenance, or `instances.getAuthString` path yet

## Deferred depth

- Redis Cluster / Memorystore for Valkey surfaces
- Connect-mode / VPC / CMEK fidelity
- Host publish overlay for operator-loopback Redis clients (not default)
- Dedicated `GET .../instances/{id}/authString` (auth string is already on Instance JSON when AUTH is on)

## Verification / CLI smoke

```bash
go test ./internal/services/memorystore/ ./internal/store/ ./internal/compute/ -run 'Memorystore|Redis|Auth' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/instances?instanceId=lab-redis" \
  -d '{"tier":"BASIC","memorySizeGb":1,"displayName":"Lab Redis","authEnabled":true,"authString":"lab-redis-secret"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/operations/create-lab-redis"
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/instances/lab-redis"
```

With default Compose, confirm `host` is `noctaxris-gcp-redis-lab-redis` and
reach Redis from another container on `noctaxris-gcp-lab` (not from the host
unless you add a custom publish overlay). With AUTH enabled, clients must
`AUTH` with the instance `authString` (or `REDISCLI_AUTH`).

```bash
gcloud config set api_endpoint_overrides/redis http://127.0.0.1:4588/
```
