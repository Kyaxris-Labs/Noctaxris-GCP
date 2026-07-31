# Cloud SQL

Lab Cloud SQL Admin REST for instances, users, and databases (PostgreSQL and
MySQL engines). Default Compose leaves `NOCTAXRIS_GCP_DOCKER_HOST` empty:
instances return `state=RUNNABLE` with theatre `host`, `port`, and
`ipAddresses`. Default Compose DinD can start pinned
`postgres:16-alpine` or `mysql:8.0` containers on the internal
`noctaxris-gcp-lab` network (no host DB port publish).

## Status

**lab** — project-scoped instance, user, and database CRUD under `/sql/v1/`
and `/sql/v1beta4/` (not `/v1/projects/.../instances`, which Spanner owns).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/sql/v1/projects/{p}/instances?instanceId={id}` (Instance body → `sql#operation`) |
| `GET` | `/sql/v1/projects/{p}/instances` |
| `GET` | `/sql/v1/projects/{p}/instances/{instance}` |
| `DELETE` | `/sql/v1/projects/{p}/instances/{instance}` (`sql#operation`) |
| `GET` | `/sql/v1/projects/{p}/operations` |
| `GET` | `/sql/v1/projects/{p}/operations/{operation}` |
| `POST` | `/sql/v1/projects/{p}/instances/{instance}/users` (User body) |
| `GET` | `/sql/v1/projects/{p}/instances/{instance}/users` |
| `GET` | `/sql/v1/projects/{p}/instances/{instance}/users/{name}?host=` |
| `DELETE` | `/sql/v1/projects/{p}/instances/{instance}/users?name=&host=` |
| `POST` | `/sql/v1/projects/{p}/instances/{instance}/databases` (Database body) |
| `GET` | `/sql/v1/projects/{p}/instances/{instance}/databases` |
| `GET` | `/sql/v1/projects/{p}/instances/{instance}/databases/{database}` |
| `DELETE` | `/sql/v1/projects/{p}/instances/{instance}/databases/{database}` |

The same paths are also mounted under `/sql/v1beta4/...` for Terraform
`google_sql_database_instance` (`sql_custom_endpoint`).

`databaseVersion` must be `POSTGRES_*` or `MYSQL_*` (lab defaults: `POSTGRES_16`,
`MYSQL_8_0`). Shorthand `POSTGRES` / `MYSQL` map to those defaults.

Instance create/delete and user/database create/delete return a synchronous
`sql#operation` (`kind: sql#operation`, `status: DONE`) with a synthesised
`name`. `GET .../operations/{operation}` returns DONE immediately (Filestore-style
LRO theatre for TF waiters). Poll then `GET .../instances/{id}` for the Instance.

## Authz

Checked on `projects/{project}`:

- `cloudsql.instances.create|get|list|delete`
- `cloudsql.operations.get|list`
- `cloudsql.users.create|get|list|delete`
- `cloudsql.databases.create|get|list|delete`

Seeded Service Usage: `sqladmin.googleapis.com`.

## Emulator limits

- No Cloud SQL Auth Proxy or IAM DB auth
- No backups, replicas, or flags admin; Operations.get is immediate DONE theatre (no async worker)
- Nested engine is best-effort by default: create still succeeds with theatre host when pull/start fails
- Set `NOCTAXRIS_GCP_NESTED_ENGINE_FAIL_CLOSED=1` (or `true`) so create returns `FAILED_PRECONDITION` when the engine is enabled but start fails (instance row is rolled back)
- Nested container root/postgres password is the fixed lab value `noctaxris-gcp-lab` (not for production)
- User/database create persists in SQLite; when `container_id` is set, best-effort `CREATE USER` / `CREATE DATABASE` via docker exec (soft-fail without engine or on exec error)
- User passwords are write-only on the wire (not returned on get/list)

## Deferred depth

- SSL client cert issuance
- High availability, read replicas, PITR
- Cloud SQL Auth Proxy integration

## Nested opt-in

1. Start emulator with `docker compose -f compose.yaml --env-file .env up --build`
2. Create instance; when nested start succeeds, `host` is the Docker container name on `noctaxris-gcp-lab` and `port` is `5432` or `3306`
3. Connect from other containers on the same engine network only (not published on the host); use password `noctaxris-gcp-lab`
4. Create users/databases via Admin REST; nested `CREATE` runs when the instance has a `container_id`
5. Optional: `NOCTAXRIS_GCP_NESTED_ENGINE_FAIL_CLOSED=1` to refuse create when nested start fails instead of keeping theatre metadata

## Verification / CLI smoke

```bash
go test ./internal/services/cloudsql/ ./internal/store/ -count=1 -run 'CloudSQL|SQL|Operation|v1beta4'
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/sql/v1/projects/noctaxris-gcp-local/instances?instanceId=lab-pg" \
  -d '{"databaseVersion":"POSTGRES_16","region":"us-central1","settings":{"tier":"db-f1-micro"}}'
# Response is sql#operation (status=DONE); poll then get instance:
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/sql/v1/projects/noctaxris-gcp-local/operations/create-lab-pg"
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/sql/v1/projects/noctaxris-gcp-local/instances/lab-pg/users" \
  -d '{"name":"appuser","password":"lab-pass"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/sql/v1/projects/noctaxris-gcp-local/instances/lab-pg/databases" \
  -d '{"name":"appdb"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/sql/v1/projects/noctaxris-gcp-local/instances/lab-pg"
```

```bash
gcloud config set api_endpoint_overrides/sqladmin http://127.0.0.1:4588/
gcloud sql instances list --project=noctaxris-gcp-local
```
