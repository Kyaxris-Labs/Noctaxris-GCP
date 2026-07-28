# Cloud Monitoring

Lab Monitoring MetricService REST v3 for metric descriptors and time series.

## Status

**lab** — Create/Get/List metric descriptors; CreateTimeSeries; ListTimeSeries with simple aligners. No alerts or SLOs.

## Wire protocol

| Method | Path |
|--------|------|
| `POST` | `/v3/projects/{project}/metricDescriptors` |
| `GET` | `/v3/projects/{project}/metricDescriptors` |
| `GET` | `/v3/projects/{project}/metricDescriptors/{type...}` |
| `POST` | `/v3/projects/{project}/timeSeries` |
| `GET` | `/v3/projects/{project}/timeSeries` |

### ListTimeSeries

Query params used:

| Param | Behavior |
|-------|----------|
| `filter` | `metric.type="..."` substring parse |
| `interval.startTime` / `interval.endTime` | Inclusive bounds on point `endTime` |
| `aggregation.perSeriesAligner` | `ALIGN_NONE`, `ALIGN_MEAN`, `ALIGN_SUM`, `ALIGN_MAX`, `ALIGN_MIN` |

## Authz

- `monitoring.metricDescriptors.create|get|list`
- `monitoring.timeSeries.create|list`

## Client configuration

```go
option.WithEndpoint("127.0.0.1:4588")
```

gcloud:

```bash
gcloud config set api_endpoint_overrides/monitoring http://127.0.0.1:4588/
```

## Deferred depth

- Alert policies, notification channels, SLOs, uptime checks
- Cross-series aggregation / groupBy / secondary aligners
- Monitored resource descriptors catalog
- gRPC MetricService (REST is the lab default)

## Verification / CLI smoke

```bash
go test ./internal/server/ -run Monitoring -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"type":"custom.googleapis.com/lab/x","metricKind":"GAUGE","valueType":"DOUBLE"}' \
  http://127.0.0.1:4588/v3/projects/noctaxris-gcp-local/metricDescriptors
```
