# Cloud Audit Logs

Lab theatre for Cloud Audit Logs–shaped entries: env-gated inject plus list via
Cloud Logging `entries:list` when the filter targets `cloudaudit.googleapis.com`
log names. Kernel `audit.jsonl` remains the file sink; live `audit.Writer`
events optionally mirror into the same SQLite table.

## Status

**lab (honest theatre)** — inject + listable `protoPayload` lite; not a full
Cloud Audit Logs / Admin Activity export pipeline.

## Wire protocol

| Method | Path | Notes |
|--------|------|-------|
| `POST` | `/_noctaxris-gcp/lab/auditLogs:inject` | Lab-only; env-gated; Bearer root |
| `POST` | `/v2/entries:list` | Filter `logName="projects/.../logs/cloudaudit.googleapis.com%2Factivity"` (also `data_access` / `system_event`) |
| `GET` | `/v2/projects/{project}/logs` | Includes distinct CAL log names |

Default inject log name when omitted:

`projects/{project}/logs/cloudaudit.googleapis.com%2Factivity`

### Inject body

```json
{
  "projectId": "noctaxris-gcp-local",
  "entries": [
    {
      "logName": "projects/noctaxris-gcp-local/logs/cloudaudit.googleapis.com%2Factivity",
      "severity": "NOTICE",
      "protoPayload": {
        "@type": "type.googleapis.com/google.cloud.audit.AuditLog",
        "serviceName": "storage.googleapis.com",
        "methodName": "storage.objects.get",
        "resourceName": "projects/_/buckets/lab/objects/o",
        "authenticationInfo": { "principalEmail": "alice@example.com" }
      }
    }
  ]
}
```

Flat lite fields are accepted when `protoPayload` is omitted: `serviceName`,
`methodName`, `resourceName`, `principalEmail`, optional `permission` /
`granted` / `statusCode`. Cap 50 entries per request. Single `entry` is also
accepted.

### Fail-closed gates

Inject is refused unless **both**:

1. `NOCTAXRIS_GCP_AUDIT_INJECT=1` (or `true`) on the API process
2. Bearer root (`IsRoot`)

Unset env or non-root Bearer returns `PERMISSION_DENIED`.

### Live audit mirror

When `registerCloudAudit` wires `audit.Writer.SetSink`, each successful
`audit.Writer.Write` also inserts an activity-log CAL row for
`NOCTAXRIS_GCP_PROJECT` (best-effort forensic theatre).

## Authz

- Inject: root only (after env gate); no IAM permission string
- List: existing Logging `logging.logEntries.list` on `projects/{project}`

## Emulator limits

- No real GCP Audit Logs router, org sinks, or Data Access auto-generation
- `protoPayload` is a lite JSON object (not full AuditLog protobuf)
- Kernel `audit.jsonl` and SQLite CAL table are separate stores; inject writes
  SQLite only (unless you also write through `audit.Writer`)
- Filter language for CAL list is the Logging subset; CAL rows are returned when
  the filter names a `cloudaudit.googleapis.com` log

## Client configuration

Same Logging endpoint overrides as [logging.md](logging.md). Enable inject:

```bash
NOCTAXRIS_GCP_AUDIT_INJECT=1
```

## Deferred depth

- Full Admin / Data Access / System Event generation from every API call
- Org-level and folder-scoped audit log names
- Log-based metrics / sink export of CAL
- gRPC Logging + Audit Logs Admin APIs

## Verification / CLI smoke

```bash
go test ./internal/store/ ./internal/services/logging/ ./internal/server/ \
  -run 'CloudAudit|AuditInject|RegisterCloudAudit' -count=1

TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
PROJECT=noctaxris-gcp-local
LOG="projects/$PROJECT/logs/cloudaudit.googleapis.com%2Factivity"

# Requires NOCTAXRIS_GCP_AUDIT_INJECT=1 on the API process
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"projectId\":\"$PROJECT\",\"entries\":[{\"serviceName\":\"storage.googleapis.com\",\"methodName\":\"storage.buckets.get\",\"principalEmail\":\"alice@example.com\"}]}" \
  http://127.0.0.1:4588/_noctaxris-gcp/lab/auditLogs:inject

curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"resourceNames\":[\"projects/$PROJECT\"],\"filter\":\"logName=\\\"$LOG\\\"\"}" \
  http://127.0.0.1:4588/v2/entries:list
```
