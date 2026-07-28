# Changelog

## [Unreleased]

## [0.3.0] - 2026-07-28

### Added

| Area | Change |
|------|--------|
| Artifact Registry | Repositories CRUD; packages/versions list/get/delete plus lab metadata create (no blobs) |
| Cloud Build | createBuild Operation theatre (`WORKING`→`SUCCESS` on get); project-scoped triggers create/get/list/delete |
| App Engine | Admin API v1 control-plane theatre: apps create/get, services list/get, versions CRUD (runtime + env metadata; no serving) |
| Cloud Resource Manager | Seeded lab org `organizations/noctaxris-gcp-org`, folders CRUD lite, project `parent` + ancestry theatre |
| Cloud Workflows | Workflows CRUD (`sourceContents`); executions create/get/list with immediate SUCCEEDED theatre result |
| Cloud Spanner | Instances/databases CRUD; session create; `:executeSql` empty ResultSet theatre (no Spanner binary) |
| Terraform | Deepen `lab-storage` (GCS + Secret Manager + Pub/Sub topic); add `lab-run` (`google_cloud_run_v2_service` via `cloud_run_v2_custom_endpoint`); `tests/terraform/run.sh` runs both with soft-skip |
| Cloud Run | Traffic metadata, jobs CRUD theatre, service IAM get/set, richer revisions, invoke header records |
| Cloud Functions | generateUploadUrl theatre, patch merge, function IAM get/set |
| Cloud Scheduler | 5-field cron next-run; OIDC audience stored (token stripped) |
| Cloud Tasks | Rate limits / retryConfig / App Engine fields stored; `:run` forces dispatch |
| Eventarc | Channel stub, attribute `values` maps, one retry on failed deliver |
| Firebase Auth | Password-reset OOB lab, listUsers pagination, setCustomUserClaims, verifyIdToken for unsigned lab JWT |
| Cloud KMS | `ASYMMETRIC_SIGN` / `RSA_SIGN_PSS_2048_SHA256` (SOFTWARE) with asymmetricSign + GetPublicKey; UpdateCryptoKey labels; cryptoKey getIamPolicy/setIamPolicy |
| Cloud Logging | Sinks CRUD (metadata only); one-shot `entries:tail`; `entries:copy` completed-LRO theatre |
| Cloud Monitoring | DeleteTimeSeries; alertPolicies CRUD metadata theatre; ALIGN_SUM alongside ALIGN_MEAN |
| Firestore | Collection-group queries, ORDER BY + LIMIT, single-field inequality filters, FieldTransform `serverTimestamp` / `increment` on Commit, PartitionQuery single-partition stub |
| Datastore | GQL subset and structured AND filters, AllocateIds, BeginTransaction / Rollback lab tokens for transactional Commit |
| BigQuery | CREATE TABLE via `jobs.query`, `jobs.get`, `tabledata.list`, JOIN lite, dryRun queries, `insertAll` `skipInvalidRows` |

### Changed

- Cloud Run service create/update returns a completed Operation LRO (Terraform `google_cloud_run_v2_service` OpAsync); GET still returns the service resource
- Terraform provider endpoint examples use versioned suffixes (`…/v1/`, `…/v2/`) matching hashicorp/google defaults
- Expanded Go, Node.js, and Python SDK HTTP soft-skip smokes beyond project+GCS to CRM org/folders, Pub/Sub, Secret Manager, Cloud Run, Artifact Registry, Workflows, and App Engine get (404 soft-skip)

### Fixed

- Server httptest coverage for Artifact Registry repo/package/version roundtrip, Cloud Build WORKING→SUCCESS plus project-scoped triggers, and Eventarc regional trigger coexistence after the Cloud Build path split

## [0.2.0] - 2026-07-28

### Added

- Cloud Run Admin API v2 lab: services CRUD, revision metadata, in-process `:invoke` mock (no container / no host `docker.sock`)
- Cloud Functions v2 lab: control-plane CRUD, ACTIVE theatre, optional HTTP `:invoke` stub
- Cloud Scheduler v1 lab: jobs CRUD, cron storage, best-effort HTTP and Pub/Sub fire
- Cloud Tasks v2 lab: queues/tasks CRUD, scheduleTime storage, best-effort HTTP dispatch / `:run`
- BigQuery lab: datasets/tables CRUD, `insertAll`, limited `jobs.query` (SELECT/WHERE/LIMIT on inserted rows)
- Firebase Auth lab: Identity Toolkit email/password and admin user CRUD; unsigned lab custom tokens
- Cloud Monitoring lab: metric descriptors and time series write/list with basic aligners
- Cloud Datastore lab: gRPC Lookup / Commit / equality RunQuery (`DATASTORE_EMULATOR_HOST`)
- Eventarc lab: triggers for Pub/Sub publish and GCS finalize with best-effort HTTP / Cloud Run delivery
- `registerExpandCompute` and `registerExpandAnalytics` wiring from `server.New`

### Changed

- Deepened existing identity, data, secrets, Firestore, KMS, and Logging lab surfaces (IAM enable/disable and SA IAM; CRM project patch; Service Usage batchEnable; GCS bucket IAM / compose / copy / metadata; Pub/Sub REST mirror and push theatre; Secret Manager patch and per-secret IAM; Firestore field masks and richer filters; KMS version list/get/restore; Logging list/delete logs)
- Docs, README Services table, architecture registration table, and client endpoint override lists cover the full lab matrix
