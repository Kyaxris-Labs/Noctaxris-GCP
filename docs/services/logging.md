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

### Copy

`entries:copy` returns a completed LRO (`done: true`) with destination/filter echoed. No bytes are exported.

### Sinks

Store `name`, `destination`, `filter`, theatre `writerIdentity`, timestamps. No real export to GCS/BigQuery/Pub/Sub.

### Filter subset

| Filter | Behavior |
|--------|----------|
| `logName="projects/.../logs/..."` | Exact log name match |
| `textPayload:"needle"` | Substring match against stored payload JSON |
| `severity=ERROR` / `severity="ERROR"` | Exact severity (case-insensitive) |
| `timestamp>="..."` / `timestamp>"..."` | Inclusive/exclusive lower bound (string compare on stored RFC3339) |
| `timestamp<"..."` / `timestamp<="..."` | Upper bound (`<=` treated as exclusive `<` in lab) |

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

## Deferred depth

- Real sink export, log-based metrics, buckets/views, exclusions
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
