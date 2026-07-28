# Services

Implemented lab surface on `127.0.0.1:4588`. Status **lab** means CLI/SDK-usable
with honest emulator limits on each page.

| Service | Status | Doc | Protocol |
|---------|--------|-----|----------|
| Cloud Resource Manager | lab | [resourcemanager.md](resourcemanager.md) | REST v3 projects, org seed, folders CRUD lite |
| IAM | lab | [iam.md](iam.md) | REST v1 service accounts and keys |
| Service Usage | lab | [serviceusage.md](serviceusage.md) | REST v1 enable / disable / list / batchEnable |
| Cloud Storage | lab | [gcs.md](gcs.md) | JSON API v1 (`STORAGE_EMULATOR_HOST`) |
| Pub/Sub | lab | [pubsub.md](pubsub.md) | gRPC + REST topics/subscriptions (`PUBSUB_EMULATOR_HOST`) |
| Secret Manager | lab | [secret-manager.md](secret-manager.md) | REST + gRPC SecretManagerService |
| Firestore | lab | [firestore.md](firestore.md) | gRPC Firestore v1 (`FIRESTORE_EMULATOR_HOST`) |
| Cloud KMS | lab | [kms.md](kms.md) | REST v1 symmetric + RSA_SIGN_PSS sign/verify |
| Cloud Logging | lab | [logging.md](logging.md) | REST v2 entries, sinks, one-shot tail, copy theatre |
| Cloud Run | lab | [cloud-run.md](cloud-run.md) | REST Admin API v2 services/jobs, traffic, IAM, mock `:invoke` |
| Cloud Functions | lab | [cloud-functions.md](cloud-functions.md) | REST Functions v2, upload URL theatre, IAM, `:invoke` stub |
| Cloud Scheduler | lab | [cloud-scheduler.md](cloud-scheduler.md) | REST v1 jobs, 5-field cron next-run, pause/resume, OIDC audience |
| Cloud Tasks | lab | [cloud-tasks.md](cloud-tasks.md) | REST v2 queues/tasks, rate limits, retry, App Engine fields, `:run` |
| BigQuery | lab | [bigquery.md](bigquery.md) | REST v2 datasets/tables, insertAll, tabledata.list, jobs.query/get |
| Firebase Auth | lab | [firebase-auth.md](firebase-auth.md) | Identity Toolkit REST, OOB reset, claims, verifyIdToken |
| Cloud Monitoring | lab | [monitoring.md](monitoring.md) | REST v3 descriptors, time series, alertPolicies theatre |
| Cloud Datastore | lab | [datastore.md](datastore.md) | gRPC Datastore v1 (`DATASTORE_EMULATOR_HOST`) |
| Eventarc | lab | [eventarc.md](eventarc.md) | REST v1 triggers/channels; Pub/Sub and GCS delivery + retry |
| Artifact Registry | lab | [artifact-registry.md](artifact-registry.md) | REST v1 repos/packages/versions metadata (no blobs) |
| Cloud Build | lab | [cloud-build.md](cloud-build.md) | REST v1 createBuild theatre + triggers CRUD lite |
| Workflows | lab | [workflows.md](workflows.md) | REST v1 workflows CRUD + executions SUCCEEDED theatre |
| Cloud Spanner | lab | [spanner.md](spanner.md) | REST v1 instances/databases + session ExecuteSql theatre |
| App Engine | lab | [app-engine.md](app-engine.md) | REST Admin API v1 apps/services/versions (control-plane theatre) |

Default project id: `noctaxris-gcp-local` (`NOCTAXRIS_GCP_PROJECT`).
Seeded organization: `organizations/noctaxris-gcp-org`.

## Emulator limits (summary)

Per-service deferred depth lives on each page. Shared gaps:

- Seeded organization `organizations/noctaxris-gcp-org`; default project parent is
  that org; folders CRUD lite (no full hierarchy tooling)
- Cloud Build triggers use project-scoped paths (`projects/.../triggers`); Eventarc
  owns regional `.../locations/.../triggers` on the shared mux
- Bearer required on API paths (health/ready/version are public; Identity Toolkit
  `/identitytoolkit.googleapis.com/v1/accounts*` client methods are also public)
- Root principal bypasses IAM evaluation (lab operator)
- No host `docker.sock`; Compose publishes loopback only

## Client smoke

Soft-skip SDK and Terraform suites under `tests/` when
`NOCTAXRIS_GCP_ENDPOINT` is unset or `/_noctaxris-gcp/ready` fails.
Authenticated cases also soft-skip when `NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN` is unset.

HTTP live smokes under `tests/sdk/` (Go, Node.js, Python) cover:

| Area | Smoke |
|------|-------|
| CRM | get project; get org; list folders under `organizations/noctaxris-gcp-org` |
| GCS | list buckets |
| Pub/Sub | create topic (unique id) + list topics; delete on cleanup |
| Secret Manager | create secret, addVersion, access; delete on cleanup |
| Cloud Run | list services in `us-central1` |
| Artifact Registry | list repositories in `us-central1` |
| Workflows | list workflows in `us-central1` |
| App Engine | get app (soft-skip on 404 when app not created) |

Terraform apply/destroy soft-skips the same way; default stacks are `lab-storage`
and `lab-run` under `tests/terraform/stacks/` (`bash tests/terraform/run.sh`).

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
gcloud config set api_endpoint_overrides/artifactregistry http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/cloudbuild http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/workflows http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/workflowexecutions http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/spanner http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/appengine http://127.0.0.1:4588/
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
