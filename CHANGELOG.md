# Changelog

## [Unreleased]

### Added

### Changed

### Fixed

- CI govulncheck: bump Go to 1.26.5 (clears `GO-2026-5856`); adopt Noctaxris-style `scripts/govulncheck-ci` + empty allowlist

## [0.4.0] - 2026-07-28

### Added

| Area | Change |
|------|--------|
| Compute Engine | Instances CRUD metadata theatre (status stop/start/reset; no nested VMs); VPC networks, regional subnetworks, firewalls CRUD lite under `/compute/v1/...` |
| Cloud DNS | managedZones + resourceRecordSets CRUD (`dns/v1`); zone create seeds NS/SOA metadata |
| Dataflow | regional jobs create/get/list theatre (`v1b3`); create=`RUNNING`, get advances to `DONE`; project-level jobs list |
| Cloud Bigtable | Admin API v2 instances/tables CRUD theatre (cluster metadata lite; no data plane / no Bigtable server) |
| Memorystore Redis | Location-scoped instances CRUD theatre (`tier`, `memorySizeGb`, host theatre; no Redis process) |
| SDK smokes | Soft-skip HTTP coverage for Compute Engine, Cloud DNS, Bigtable, Memorystore, Dataflow (Go/Node/Python) |
| Terraform | `lab-dns` (`google_dns_managed_zone` via `dns_custom_endpoint`); `lab-compute` (`google_compute_network` via `compute_custom_endpoint`); `run.sh` default set includes both; README documents skips for DNS Changes, Compute images/instances, and Bigtable gRPC |

### Changed

| Area | Change |
|------|--------|
| Artifact Registry | Repository IAM get/set; `files.list` and package `tags.list` metadata theatre; PATCH labels via `updateMask` |
| Cloud Build | `builds:cancel` / `builds:retry`; project-scoped `triggers:run` theatre (no webhook) |
| Cloud Workflows | PATCH workflow (revision bump on source change); execution `:cancel`; JSON `argument` validation; list `pageSize` |
| Cloud Spanner | `PATCH .../ddl` stores statements (completed Operation); `sessions:batchCreate`; `:read` empty ResultSet; `:partitionQuery` stub; list instance configs stub |
| App Engine | Service patch traffic split + `migrateTraffic` theatre; list instances empty stub |
| Cloud Resource Manager | MoveFolder; SearchFolders; undelete folder; org getIamPolicy/setIamPolicy lite; project labels patch |
| IAM | `signJwt` theatre (`alg=none` unsigned lab JWT; Credentials-shaped payload / `keyId` / `signedJwt`) |
| Service Usage | `services:batchDisable`; richer get/list config (`apis`, `documentation.summary` for seeded titles) |
| Pub/Sub | Snapshots create/get/list/delete lite (REST + gRPC metadata; seek-to-snapshot still rejected) |

### Fixed

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
- `registerServerless` and `registerAnalytics` wiring from `server.New`

### Changed

- Deepened existing identity, data, secrets, Firestore, KMS, and Logging lab surfaces (IAM enable/disable and SA IAM; CRM project patch; Service Usage batchEnable; GCS bucket IAM / compose / copy / metadata; Pub/Sub REST mirror and push theatre; Secret Manager patch and per-secret IAM; Firestore field masks and richer filters; KMS version list/get/restore; Logging list/delete logs)
- Docs, README Services table, architecture registration table, and client endpoint override lists cover the full lab matrix
