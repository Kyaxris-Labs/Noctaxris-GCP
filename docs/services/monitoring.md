# Cloud Monitoring

Lab Monitoring MetricService REST v3 for metric descriptors, time series, and alert policy metadata.

## Status

**lab** — Create/Get/List metric descriptors; CreateTimeSeries; ListTimeSeries with aligners; DeleteTimeSeries; alertPolicies CRUD (metadata theatre; no notification delivery).

## Wire protocol

| Method | Path |
|--------|------|
| `POST` | `/v3/projects/{project}/metricDescriptors` |
| `GET` | `/v3/projects/{project}/metricDescriptors` |
| `GET` | `/v3/projects/{project}/metricDescriptors/{type...}` |
| `POST` | `/v3/projects/{project}/timeSeries` |
| `GET` | `/v3/projects/{project}/timeSeries` |
| `POST` | `/v3/projects/{project}/timeSeries:delete` |
| `POST` | `/v3/projects/{project}/alertPolicies` (`?alertPolicyId=` optional) |
| `GET` | `/v3/projects/{project}/alertPolicies` |
| `GET` | `/v3/projects/{project}/alertPolicies/{policy}` |
| `PATCH` | `/v3/projects/{project}/alertPolicies/{policy}` |
| `DELETE` | `/v3/projects/{project}/alertPolicies/{policy}` |

### ListTimeSeries

Query params used:

| Param | Behavior |
|-------|----------|
| `filter` | `metric.type="..."` substring parse |
| `interval.startTime` / `interval.endTime` | Inclusive bounds on point `endTime` |
| `aggregation.perSeriesAligner` | `ALIGN_NONE`, `ALIGN_MEAN`, `ALIGN_SUM`, `ALIGN_MAX`, `ALIGN_MIN` |

Aligners collapse a series to one point (mean/sum/max/min of `doubleValue` / `int64Value`).

### DeleteTimeSeries

`filter` via query or JSON body must include `metric.type="..."`. Deletes matching stored points. Response includes lab field `labDeletedPoints`.

### Alert policies

Stored fields: `displayName`, `enabled`, `combiner`, `conditions` (opaque JSON), `documentation`, `userLabels`. No evaluation, incidents, or notification channels.

## Authz

- `monitoring.metricDescriptors.create|get|list`
- `monitoring.timeSeries.create|list|delete`
- `monitoring.alertPolicies.create|get|list|update|delete`

## Emulator limits

- Time series and descriptors persist in SQLite; no sampling agent or Stackdriver backend
- Alert policies are metadata only; no evaluation, incidents, or notification delivery
- ListTimeSeries supports a subset of filters and aligners (see wire protocol)

## Client configuration

```go
option.WithEndpoint("127.0.0.1:4588")
```

gcloud:

```bash
gcloud config set api_endpoint_overrides/monitoring http://127.0.0.1:4588/
```

## Deferred depth

- Notification channels, SLOs, uptime checks, incident lifecycle
- Cross-series aggregation / groupBy / secondary aligners
- Monitored resource descriptors catalog
- gRPC MetricService (REST is the lab default)

## Verification / CLI smoke

```bash
go test ./internal/services/monitoring/ ./internal/server/ -run Monitoring -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"type":"custom.googleapis.com/lab/x","metricKind":"GAUGE","valueType":"DOUBLE"}' \
  http://127.0.0.1:4588/v3/projects/noctaxris-gcp-local/metricDescriptors
```
