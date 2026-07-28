# BigQuery

Lab BigQuery REST v2 for datasets, tables, streaming inserts, tabledata list, jobs get, and a limited query engine.

## Status

**lab** — datasets/tables CRUD, `insertAll` (max 500 rows, optional `skipInvalidRows`), `tabledata.list`, `jobs.query` / `jobs.get`, dryRun, CREATE TABLE via query, JOIN lite.

## Wire protocol

| Method | Path |
|--------|------|
| `POST` | `/bigquery/v2/projects/{project}/datasets` |
| `GET` | `/bigquery/v2/projects/{project}/datasets` |
| `GET` | `/bigquery/v2/projects/{project}/datasets/{dataset}` |
| `DELETE` | `/bigquery/v2/projects/{project}/datasets/{dataset}` |
| `POST` | `/bigquery/v2/projects/{project}/datasets/{dataset}/tables` |
| `GET` | `/bigquery/v2/projects/{project}/datasets/{dataset}/tables` |
| `GET` | `/bigquery/v2/projects/{project}/datasets/{dataset}/tables/{table}` |
| `DELETE` | `/bigquery/v2/projects/{project}/datasets/{dataset}/tables/{table}` |
| `POST` | `/bigquery/v2/projects/{project}/datasets/{dataset}/tables/{table}/insertAll` |
| `GET` | `/bigquery/v2/projects/{project}/datasets/{dataset}/tables/{table}/data` |
| `POST` | `/bigquery/v2/projects/{project}/queries` |
| `GET` | `/bigquery/v2/projects/{project}/jobs/{job}` |

Rows from `insertAll` are stored as JSON in SQLite. Jobs from `jobs.query` are stored for `jobs.get`.

### Query engine

```sql
SELECT col|* FROM dataset.table [WHERE col = value] [LIMIT n]
SELECT a.x, b.y FROM dataset.t1 a JOIN dataset.t2 b ON a.id = b.id [LIMIT n]
CREATE TABLE dataset.table (col TYPE [REQUIRED|NULLABLE], ...)
```

`dryRun: true` validates/parses and returns schema without rows (or without creating the table for CREATE TABLE).

`insertAll` with `skipInvalidRows: true` skips rows missing REQUIRED schema fields and reports `insertErrors`.

## Authz

Checked on `projects/{project}`:

- `bigquery.datasets.*`
- `bigquery.tables.*`
- `bigquery.jobs.create` / `bigquery.jobs.get`

## Client configuration

No official Go emulator env var. Point the client at the lab:

```go
option.WithEndpoint("127.0.0.1:4588")
option.WithHTTPClient(...) // path prefix /bigquery/v2
```

gcloud:

```bash
gcloud config set api_endpoint_overrides/bigquery http://127.0.0.1:4588/
```

## Deferred depth

- Load jobs, extract jobs, copy jobs
- Partitioned/clustered tables, views, routines
- Full SQL / BI Engine
- Storage Read/Write API

## Verification / CLI smoke

```bash
go test ./internal/server/ -run BigQuery -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
PROJECT=noctaxris-gcp-local
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"datasetReference":{"datasetId":"demo"}}' \
  http://127.0.0.1:4588/bigquery/v2/projects/$PROJECT/datasets
```
