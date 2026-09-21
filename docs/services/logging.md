# Cloud Logging

Lab-complete Cloud Logging v2 REST for writing and listing log entries, sink metadata, and light theatre APIs.

## Status

**lab** — `entries:write`, `entries:list`, list/delete logs, filter subset, sinks CRUD (metadata only; no export), one-shot `entries:tail`, `entries:copy` LRO theatre.

## Wire protocol

Colon custom methods use literal path segments (`entries:write`, `entries:list`, `entries:tail`, `entries:copy`) because ServeMux wildcards cannot embed `:` inside a segment.

| Method | Path |
|--------|------|
| `POST` | `/v2/entries:write` |
| `POST` | `/v2/entries:list` |
| `POST` | `/v2/entries:tail` |
| `POST` | `/v2/entries:copy` |
| `GET` | `/v2/projects/{project}/logs` |
| `DELETE` | `/v2/projects/{project}/logs/{log}` |
| `POST` | `/v2/projects/{project}/sinks?sinkId=` |
| `GET` | `/v2/projects/{project}/sinks` |
| `GET` | `/v2/projects/{project}/sinks/{sink}` |
| `PUT` / `PATCH` | `/v2/projects/{project}/sinks/{sink}` |
| `DELETE` | `/v2/projects/{project}/sinks/{sink}` |
| `GET` / `POST` | `/v2/projects/{project}/exclusions` |
| `GET` / `DELETE` | `/v2/projects/{project}/exclusions/{exclusion}` |
| `GET` | `/v2/projects/{project}/locations/{location}/buckets` |
| `GET` | `/v2/projects/{project}/locations/{location}/buckets/{bucket}` |
| `GET` / `POST` | `.../buckets/{bucket}/views` |
| `GET` | `.../buckets/{bucket}/views/{view}` |
| `POST` | `/_noctaxris-gcp/lab/logs:inject` |

`{log}` is the log id (URL-decoded by the server). Full log name is `projects/{project}/logs/{log}`.

### Write

Body fields used: `logName`, optional default `resource`, `entries[]` with `logName`, `textPayload` / `jsonPayload`, `severity`, `timestamp`, `insertId`.

Missing `insertId` / `timestamp` are filled by the emulator.

### List / Tail

Body fields used: `resourceNames` (or deprecated `projectIds`), `filter`, `pageSize`, `pageToken`.

| Limit | Value |
|-------|-------|
| Default page size | 50 |
| Max page size | 1000 |
| Page token | numeric offset (lab) |

`entries:tail` is **one-shot**: returns currently matching entries (same filter subset as list). No streaming / long-poll.

Unscoped `entries:list` (no exact `logName=`) applies enabled exclusions and can omit those log names. An exact `logName="projects/.../logs/..."` filter does not apply exclusions.

### Copy

`entries:copy` returns a completed LRO (`done: true`) with destination/filter echoed. No bytes are exported.

### Sinks

Store `name`, `destination`, `filter`, `disabled`, theatre `writerIdentity`, timestamps.
A sink with `disabled: true` is persisted and listed but is omitted from matching
(no real export to GCS/BigQuery/Pub/Sub either way). `_Required` still cannot be patched or deleted.

### Filter subset

| Filter | Behavior |
|--------|----------|
| `logName="projects/.../logs/..."` | Exact log name match |
| `textPayload:"needle"` | Substring match against stored payload JSON |
| `severity=ERROR` / `severity="ERROR"` | Exact severity (case-insensitive) |
| `timestamp>="..."` / `timestamp>"..."` | Inclusive/exclusive lower bound (string compare on stored RFC3339) |
| `timestamp<"..."` / `timestamp<="..."` | Upper bound (`<=` treated as exclusive `<` in lab) |
| `resource.type="http_load_balancer"` | Exact `resource.type` (also unquoted). Lab types include `http_load_balancer` (Armor `enforcedSecurityPolicy` / `previewSecurityPolicy` in `jsonPayload`), `cloud_run_revision`, `gce_subnetwork` (VPC Flow `connection` 5-tuple + `bytes_sent`), `cloudsql_database` (`PgAuditEntry.statement`), `dns_query` |

`POST /_noctaxris-gcp/lab/logs:inject` writes non-CAL entries when `NOCTAXRIS_GCP_LOGS_INJECT=1` (Bearer root). CAL names must use `auditLogs:inject`. Cap 50. Sensitive JSON keys redact.

Seeded routing: `_Required` sink keeps Admin Activity and cannot be patched or deleted. `_Default` can drop Data Access via an exclusion (`LOG_ID("cloudaudit.googleapis.com/data_access")`).

Combined filters in one string are parsed when patterns appear. Other Logging query language operators are deferred.

### Delete / list logs

`DELETE` removes all stored entries for that log name. `GET .../logs` returns distinct `logNames` seen in the project.

## Authz

Checked on `projects/{project}`:

- `logging.logEntries.create`
- `logging.logEntries.list`
- `logging.entries.copy`
- `logging.logs.delete`
- `logging.logs.list`
- `logging.sinks.create|get|list|update|delete`
- `logging.views.list` on `projects/{project}`
- `logging.views.get` on the view resource
  `projects/{project}/locations/{location}/buckets/{bucket}/views/{view}`
  (`roles/logging.viewAccessor` grants get/list; project inheritance still
  applies). Missing `views.get` on that view denies get while `views.list` of
  other views can still succeed.

## Emulator limits

- Entries and sinks persist in SQLite; sinks do not export to destinations
- `entries:tail` is one-shot (no streaming); `entries:copy` is a completed LRO with no byte export
- Filter language is the documented subset only
- `_Required` / `_Default` buckets and views are metadata theatre (no real export pipeline)

## Client configuration

No official Go emulator env var. Use `WithEndpoint` / custom HTTP base:

```go
option.WithEndpoint("127.0.0.1:4588")
```

gcloud:

```bash
gcloud config set api_endpoint_overrides/logging http://127.0.0.1:4588/
```

Send `Authorization: Bearer <token>` on every call.

## Related

Cloud Audit Logs inject and `cloudaudit.googleapis.com` list filters: [cloud-audit-logs.md](cloud-audit-logs.md).

## Deferred depth

- Real sink export and log-based metrics (buckets/views/exclusions are metadata theatre)
- Full query language, histogram APIs, streaming TailLogEntries
- gRPC `LoggingServiceV2` (REST is the lab path; protos not wired in this module)

## Verification / CLI smoke

```bash
go test ./internal/services/logging/ ./internal/server/ -run Logging -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"logName":"projects/noctaxris-gcp-local/logs/app","entries":[{"textPayload":"hi"}]}' \
  http://127.0.0.1:4588/v2/entries:write
```
