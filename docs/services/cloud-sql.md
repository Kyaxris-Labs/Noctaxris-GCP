# Cloud SQL

Lab Cloud SQL Admin REST for instances (PostgreSQL and MySQL engines). Default
Compose leaves `NOCTAXRIS_GCP_DOCKER_HOST` empty: instances return `state=RUNNABLE`
with theatre `host`, `port`, and `ipAddresses`. Opt-in DinD
(`compose.engine.yaml`) can start pinned `postgres:16-alpine` or `mysql:8.0`
containers on the internal `noctaxris-gcp-lab` network (no host DB port publish).

## Status

**lab** — project-scoped instance insert/get/list/delete under `/sql/v1/` (not
`/v1/projects/.../instances`, which Spanner owns).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/sql/v1/projects/{p}/instances?instanceId={id}` (Instance body) |
| `GET` | `/sql/v1/projects/{p}/instances` |
| `GET` | `/sql/v1/projects/{p}/instances/{instance}` |
| `DELETE` | `/sql/v1/projects/{p}/instances/{instance}` |

`databaseVersion` must be `POSTGRES_*` or `MYSQL_*` (lab defaults: `POSTGRES_16`,
`MYSQL_8_0`). Shorthand `POSTGRES` / `MYSQL` map to those defaults.

## Authz

Checked on `projects/{project}`:

- `cloudsql.instances.create|get|list|delete`

Seeded Service Usage: `sqladmin.googleapis.com`.

## Emulator limits

- No Cloud SQL Auth Proxy or IAM DB auth
- No backups, replicas, flags admin, or operations polling beyond delete theatre
- Nested engine is best-effort: create still succeeds with theatre host when pull/start fails
- Password for nested containers is a fixed lab value (`noctaxris-gcp-lab`); not for production

## Deferred depth

- Users/databases SSL client cert issuance
- High availability, read replicas, PITR
- Cloud SQL Auth Proxy integration

## Nested opt-in

1. Start emulator with `docker compose -f compose.yaml -f compose.engine.yaml up`
2. Create instance; when nested start succeeds, `host` is the Docker container name on `noctaxris-gcp-lab` and `port` is `5432` or `3306`
3. Connect from other containers on the same engine network only (not published on the host)

## Verification / CLI smoke

```bash
go test ./internal/services/cloudsql/ ./internal/store/ -run CloudSQL -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/sql/v1/projects/noctaxris-gcp-local/instances?instanceId=lab-pg" \
  -d '{"databaseVersion":"POSTGRES_16","region":"us-central1","settings":{"tier":"db-f1-micro"}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/sql/v1/projects/noctaxris-gcp-local/instances/lab-pg"
```

```bash
gcloud config set api_endpoint_overrides/sqladmin http://127.0.0.1:4588/
gcloud sql instances list --project=noctaxris-gcp-local
```
