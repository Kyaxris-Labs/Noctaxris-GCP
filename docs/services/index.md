# Services

Implemented lab surface on `127.0.0.1:4588`. Status **lab** means CLI/SDK-usable
with honest emulator limits on each page.

| Service | Status | Doc | Protocol |
|---------|--------|-----|----------|
| Cloud Resource Manager | lab | [resourcemanager.md](resourcemanager.md) | REST v3 projects + project IAM |
| IAM | lab | [iam.md](iam.md) | REST v1 service accounts and keys |
| Service Usage | lab | [serviceusage.md](serviceusage.md) | REST v1 enable / disable / list / batchEnable |
| Cloud Storage | lab | [gcs.md](gcs.md) | JSON API v1 (`STORAGE_EMULATOR_HOST`) |
| Pub/Sub | lab | [pubsub.md](pubsub.md) | gRPC + REST `/v1/.../topics\|subscriptions` (`PUBSUB_EMULATOR_HOST`) |
| Secret Manager | lab | [secret-manager.md](secret-manager.md) | REST + gRPC SecretManagerService |
| Firestore | lab | [firestore.md](firestore.md) | gRPC Firestore v1 (`FIRESTORE_EMULATOR_HOST`) |
| Cloud KMS | lab | [kms.md](kms.md) | REST v1 symmetric encrypt/decrypt |
| Cloud Logging | lab | [logging.md](logging.md) | REST v2 entries write/list, logs list/delete |
| Cloud Run | lab | [cloud-run.md](cloud-run.md) | REST Admin API v2 services + mock `:invoke` |
| Cloud Functions | lab | [cloud-functions.md](cloud-functions.md) | REST Functions v2 control plane + `:invoke` stub |
| Cloud Scheduler | lab | [cloud-scheduler.md](cloud-scheduler.md) | REST v1 jobs + best-effort HTTP/PubSub fire |
| Cloud Tasks | lab | [cloud-tasks.md](cloud-tasks.md) | REST v2 queues/tasks + best-effort HTTP dispatch |
| BigQuery | lab | [bigquery.md](bigquery.md) | REST v2 datasets/tables, insertAll, limited query |
| Firebase Auth | lab | [firebase-auth.md](firebase-auth.md) | Identity Toolkit REST (`FIREBASE_AUTH_EMULATOR_HOST`) |
| Cloud Monitoring | lab | [monitoring.md](monitoring.md) | REST v3 metric descriptors + time series |
| Cloud Datastore | lab | [datastore.md](datastore.md) | gRPC Datastore v1 (`DATASTORE_EMULATOR_HOST`) |
| Eventarc | lab | [eventarc.md](eventarc.md) | REST v1 triggers; Pub/Sub and GCS delivery |

Default project id: `noctaxris-gcp-local` (`NOCTAXRIS_GCP_PROJECT`).

## Emulator limits (summary)

Per-service deferred depth lives on each page. Shared gaps:

- Single seeded project; no orgs/folders
- Bearer required on API paths (health/ready/version are public; Identity Toolkit
  `/identitytoolkit.googleapis.com/v1/accounts*` client methods are also public)
- Root principal bypasses IAM evaluation (lab operator)
- No host `docker.sock`; Compose publishes loopback only

## Client smoke

Soft-skip SDK and Terraform suites under `tests/` when
`NOCTAXRIS_GCP_ENDPOINT` is unset or `/_noctaxris-gcp/ready` fails.

```bash
export NOCTAXRIS_GCP_ENDPOINT=http://127.0.0.1:4588
export NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN="$ROOT_TOKEN"
go test ./tests/sdk/go/ -count=1
# node --test tests/sdk/nodejs/*.test.mjs
# pytest tests/sdk/python/
# bash tests/terraform/run.sh
```

## gcloud `api_endpoint_overrides`

Point selected command groups at the lab (then use
`CLOUDSDK_AUTH_ACCESS_TOKEN` with the root Bearer):

```bash
gcloud config set api_endpoint_overrides/cloudresourcemanager http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/iam http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/serviceusage http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/storage http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/pubsub http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/secretmanager http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/firestore http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/cloudkms http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/logging http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/run http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/cloudfunctions http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/cloudscheduler http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/cloudtasks http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/bigquery http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/monitoring http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/eventarc http://127.0.0.1:4588/
```

Firebase Auth and Datastore prefer emulator host env vars
(`FIREBASE_AUTH_EMULATOR_HOST`, `DATASTORE_EMULATOR_HOST`) rather than gcloud
endpoint overrides. See [configuration.md](../configuration.md).

## Verification

```bash
go test ./...
curl http://127.0.0.1:4588/_noctaxris-gcp/health
curl http://127.0.0.1:4588/_noctaxris-gcp/ready
curl -H "Authorization: Bearer $ROOT_TOKEN" \
  http://127.0.0.1:4588/v3/projects/noctaxris-gcp-local
```
