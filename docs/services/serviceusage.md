# Service Usage

Lab-complete enable, disable, batchEnable, batchDisable, batchGet, get, and list
for project services.

## Lab actions

| Action | Method | Path |
|--------|--------|------|
| List services | `GET` | `/v1/projects/{project}/services` |
| Get service | `GET` | `/v1/projects/{project}/services/{service}` |
| Batch get | `GET` | `/v1/projects/{project}/services:batchGet` |
| Enable | `POST` | `/v1/projects/{project}/services/{service}:enable` |
| Disable | `POST` | `/v1/projects/{project}/services/{service}:disable` |
| Batch enable | `POST` | `/v1/projects/{project}/services:batchEnable` |
| Batch disable | `POST` | `/v1/projects/{project}/services:batchDisable` |

Service names look like `storage.googleapis.com`. Resource names are
`projects/{project}/services/{service}`.

List accepts `filter=state:ENABLED` or `filter=state:DISABLED`.

Batch enable / batch disable body: `{"serviceIds":["storage.googleapis.com",...]}`
(max 20). The store update is atomic.

Batch get accepts `names` query values (resource names or bare service ids,
max 30). POST with body `{"names":[...]}` is also accepted.

Get/list/batchGet include `config.name`, `config.apis` (one entry matching the
service id), and for known seeded APIs `config.title` plus
`config.documentation.summary`.

Permissions: `serviceusage.services.list|get|enable|disable` on
`projects/{project}`.

EnsureRoot seeds known lab APIs as `ENABLED` for the default project
(CRM, IAM, Service Usage, Storage, Pub/Sub, Secret Manager, Firestore, KMS,
Logging, Run, Functions, Scheduler, Tasks, BigQuery, Identity Toolkit,
Monitoring, Datastore, Eventarc, App Engine, Artifact Registry, Cloud Build,
Workflows, Spanner, Compute Engine, Cloud DNS, Dataflow, Bigtable Admin,
Memorystore Redis, Cloud SQL Admin, Certificate Manager, Filestore, Vertex AI,
GKE Container, Managed Kafka).

Primary create mutators refuse with `FAILED_PRECONDITION` when the matching API
is DISABLED (same shape as IAM create service account):

| Mutator | Service Usage name |
|---------|-------------------|
| IAM create service account | `iam.googleapis.com` |
| Cloud Storage create bucket | `storage.googleapis.com` |
| Pub/Sub create topic | `pubsub.googleapis.com` |
| Cloud SQL create instance | `sqladmin.googleapis.com` |
| GKE create cluster | `container.googleapis.com` |
| Managed Kafka create cluster | `managedkafka.googleapis.com` |

Handlers call `store.IsServiceEnabled` via `internal/kernel/restlab.RequireServiceEnabled`
(REST) or `CheckServiceEnabled` (gRPC).

## Emulator limits

- Enable/disable/batchEnable/batchDisable return a completed Operation immediately (no async LRO worker).
- Get of an unknown service returns `DISABLED` rather than only catalog hits.
- Config payloads are static lab metadata (`name`, `apis`, optional `title` / `documentation`); not a full Service Config.
- gRPC `ServiceUsage` is not registered yet; use REST.

## Deferred depth

- Async LRO worker for enable/disable (lab returns a completed Operation immediately)
- Full Service Config beyond static lab metadata (`name`, `apis`, optional `title` / `documentation`)
- gRPC ServiceUsage registration

## Verification / CLI smoke

```bash
go test ./internal/services/serviceusage/ ./internal/server/ -run ServiceUsage -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/services"
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/services/storage.googleapis.com:enable"
```

```bash
gcloud config set api_endpoint_overrides/serviceusage http://127.0.0.1:4588/
gcloud services list --enabled --project=noctaxris-gcp-local
gcloud services enable storage.googleapis.com --project=noctaxris-gcp-local
gcloud services disable storage.googleapis.com --project=noctaxris-gcp-local
```

## SDK note

Go client `cloud.google.com/go/serviceusage/apiv1` has no official emulator host
env var. Use `option.WithEndpoint("127.0.0.1:4588")` against the lab listener.
