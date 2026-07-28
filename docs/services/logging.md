# Cloud Logging

Lab-complete Cloud Logging v2 REST for writing and listing log entries.

## Status

**lab** — `entries:write` and `entries:list` with a small filter subset.

## Wire protocol

| Method | Path |
|--------|------|
| `POST` | `/v2/entries:write` |
| `POST` | `/v2/entries:list` |

### Write

Body fields used: `logName`, optional default `resource`, `entries[]` with `logName`, `textPayload` / `jsonPayload`, `severity`, `timestamp`, `insertId`.

Missing `insertId` / `timestamp` are filled by the emulator.

### List

Body fields used: `resourceNames` (or deprecated `projectIds`), `filter`, `pageSize`, `pageToken`.

| Limit | Value |
|-------|-------|
| Default page size | 50 |
| Max page size | 1000 |
| Page token | numeric offset (lab) |

### Filter subset

| Filter | Behavior |
|--------|----------|
| `logName="projects/.../logs/..."` | Exact log name match |
| `textPayload:"needle"` | Substring match against stored payload JSON |

Combined filters in one string are parsed when both patterns appear. Other Logging query language operators are deferred.

## Authz

Checked on `projects/{project}`:

- `logging.logEntries.create`
- `logging.logEntries.list`

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

- Sinks, metrics, buckets/views, exclusions
- Full query language, histogram APIs, tail
- gRPC `LoggingServiceV2`

## Verification / CLI smoke

```bash
go test ./internal/services/logging/ ./internal/server/ -run Logging -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"logName":"projects/noctaxris-gcp-local/logs/app","entries":[{"textPayload":"hi"}]}' \
  http://127.0.0.1:4588/v2/entries:write
```
