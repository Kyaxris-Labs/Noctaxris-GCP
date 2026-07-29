# Changelog

## [Unreleased]

### Added

| Area | Change |
|------|--------|
| Docs | README + public layout aligned with Noctaxris (logo asset, Services matrix, Defaults, Architecture, Docs table, Contributors; `docs/index.md` / `ops.md` / `release.md`; `tests/README.md` / `run-all.sh` / `HANDOFF.md`) |
| Bigtable | Instance Admin gRPC lite (`CreateInstance`/`GetInstance`/`ListInstances`/`DeleteInstance`); Create returns a done Operation with Instance response; REST `/v2/` unchanged |
| Certificate Manager | Create certificate / certificateMap returns completed Operation (`done: true` + `response`); `GET .../operations/{operation}` immediate done theatre |
| Filestore | Create instance returns completed Operation (`done: true` + `response`); `GET /file/v1/.../operations/{operation}` immediate done theatre |
| HTTP egress | Shared `internal/kernel/httpegress` gate for Pub/Sub push, Eventarc HTTP, Cloud Tasks, Scheduler; lab catcher + loopback `:4588`; opt-in `NOCTAXRIS_GCP_HTTP_EGRESS` + exact allowlist; no redirects |
| Cloud Build | Regional `.../locations/.../triggers` via shared mux with Eventarc (body-shape dispatch) |
| Pub/Sub | Push `oidcToken` (serviceAccountEmail + audience) persisted and returned; push sets `Authorization: Bearer` with unsigned lab JWT (`alg=none`); catcher records authorization |
| CI | Docker image build + SPDX SBOM artifact jobs |
| Docs | `COMPARISON.md` Noctaxris vs Noctaxris-GCP sibling section |
| Cloud Storage | Bucket `retentionPolicy` persist + JSON API patch/get; delete/overwrite fail closed while object age < `retentionPeriod`; locked policy rejects shortening |
| Cloud SQL | REST `/sql/v1/` instances CRUD (POSTGRES/MYSQL); theatre RUNNABLE; optional nested `postgres:16-alpine` / `mysql:8.0` via DinD |
| IAM | STS `POST /v1/token` WIF token-exchange theatre (`wif:{provider}:{subject}` Bearer); `roles/iam.serviceAccountTokenCreator` evaluated on SA for `generateAccessToken` |
| IAM | Package unit tests for STS exchange fail-closed paths + TokenCreator grant/deny + viewer deny on `generateAccessToken`; docs deferred depth for WIF OIDC theatre |
| Cloud DNS | `changes.create` / `changes.get` / `changes.list` theatre applies rrset additions/deletions (`status: done`); in-process change history |
| Compute Engine | Global Images list/get/family stubs (`debian-12`, `ubuntu-2204-lts`, `cos-stable`) for Terraform ResolveImage |
| BigQuery | Unit tests for dataset/table CRUD, insertAll, and jobs.query |
| Firebase Auth | Unit tests for signUp/signIn, admin CRUD, verifyIdToken, and admin authz fail-closed |
| Vertex AI | Unit tests for non-google publisher and unknown model fail-closed on generateContent |
| Artifact Registry | Repository/package/version CRUD unit test |
| Cloud Build | Global triggers list/get/delete CRUD unit test |
| Docs | Deferred depth sections for Artifact Registry, Cloud Build, and Workflows (honest theatre limits) |
| Pub/Sub tests | Unit coverage for push OIDC lab catcher delivery, push endpoint SSRF fail-closed, and stored DLQ / exactly-once flags |
| Tests | Package tests for KMS encrypt/decrypt, Secret Manager rotateSecret/access, CRM folders/tags, Service Usage enable/disable |
| GKE | Container API v1 clusters CRUD (`/container/v1/...`); optional k3s one-shot with nested engine |
| HTTP(S) LB | Global `backendServices` / `urlMaps` / `forwardingRules` metadata; public lab invoke `GET /lb/{project}/{name}/...` to GCS backends |
| Cloud CDN | Distributions CRUD; public edge `GET /cdn/{id}/...` from GCS or LB origin |
| Memorystore | Hybrid nested `redis:7-alpine` on internal `noctaxris-gcp-data` when `NOCTAXRIS_GCP_DOCKER_HOST` set; theatre `host` when engine unset; `container_id` persisted for delete cleanup |
| Managed Kafka | REST v1 `/locations/{loc}/clusters` CRUD; theatre bootstrap; opt-in nested Redpanda (`docker.redpanda.com/redpandadata/redpanda:v24.2.4`) soft-fail without engine |
| Serverless + observe | Package unit tests (happy path + authz deny + Scheduler/Tasks/Eventarc HTTP egress fail-closed) for Cloud Run, Cloud Functions, Scheduler, Cloud Tasks, Logging, Monitoring, Eventarc, App Engine; service docs Emulator limits / Verification aligned |
| Clients / docs | HANDOFF + terraform README honest skips for SQL/Kafka/Redis/GKE/LB/CDN; Go SDK soft-skip list rows for KMS, Service Usage, BQ, Spanner, Build, Logging, Monitoring, Functions, Scheduler, Tasks, Eventarc; architecture nested engines; `NOCTAXRIS_GCP_NESTED` in configuration |
| Security | `IsPublicPath` path.Clean before `/cdn/` and `/lb/` public prefixes; security-defaults documents Identity Toolkit + LB/CDN public edge risk |
| Nested/edge services | get-after-create store miss returns REST 500 (cloudsql, managedkafka, gke, cdn, memorystore, loadbalancing); nil-Authz fail-closed unit coverage |

### Changed

| Area | Change |
|------|--------|
| Maintainability | Shared `internal/kernel/restlab` REST authn/authz helpers; Filestore + Eventarc adopt them |
| Store | Split analytics store into domain files (`analytics_migrate`, `bq_*`, `firebase_*`, `monitoring_*`, `datastore_*`, `eventarc_*`) |
| IAM | `roles/viewer` suffix-only reads (no `Contains(".get")`); no `secretmanager.versions.access`; `roles/editor` denies `setIamPolicy` + SA token/signing |
| IAM docs | Document TokenCreator + STS; remove outdated "metadata only / no STS" and "does not evaluate TokenCreator" limits |
| Image allowlist | Exact refs or trailing-`/` prefixes with digest; bare ambiguous prefixes rejected; pinned `postgres:16-alpine`, `mysql:8.0` for nested SQL; pin `redis:7-alpine` for Memorystore |
| Signed URL middleware | Bearer skip limited to `/storage/` and `/upload/storage/` |
| SQLite | `SetMaxOpenConns(1)` + WAL for Eventarc delivery concurrency |
| Nested invoke | Soft-fail responses omit raw engine error strings; opt-in `NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED` hard-errors on dial/run/disabled |
| Dependencies | Bump firestore `v1.24.0`, iam `v1.12.0`, secretmanager `v1.21.0`, genproto (2026-07-27), otelhttp `v0.69.0`, modernc.org/libc `v1.74.4` (go 1.26.5 unchanged; docker/grpc/sqlite already current) |

### Fixed

| Area | Change |
|------|--------|
| Authz | Nil `Evaluator` fail-closed for non-root |
| Create handlers | Get-after-create errors return 500 (Filestore, Spanner, Certificate Manager, Cloud Build triggers, Cloud Tasks) |
| CRM folders | Folder IDs use UUID hex instead of `UnixNano` (avoids collide-on-create on coarse Windows clocks) |

## [0.5.0] - 2026-07-28

### Added

| Area | Change |
|------|--------|
| Terraform | `lab-armor` (`google_compute_security_policy` via `compute_custom_endpoint`; attribution label off for missing `setLabels`); `run.sh` default set includes it; README documents Certificate Manager / Filestore LRO skips (`certificate_manager_custom_endpoint` / `filestore_custom_endpoint`) |
| Filestore | Instances CRUD theatre under `/file/v1/projects/.../locations/.../instances` (`tier`, `fileShares`, `networks`; no NFS; path prefix avoids Memorystore `/v1/.../instances` ServeMux clash) |
| Vertex AI | Allowlisted publisher model `:predict` / `:generateContent` canned JSON; unknown modelId fail-closed |
| IAM | WIF pool/provider CRUD theatre (metadata only; not real federation); `generateAccessToken` impersonation theatre (`scope`/`lifetime`, registers lab Bearer) |
| Cloud Resource Manager | TagKeys + TagBindings lite under `/v3/tagKeys` and `/v3/tagBindings` (org `organizations/noctaxris-gcp-org`) |
| Secret Manager | Rotation config store (`rotationPeriod`/`nextRotationTime`/`topics`); lab `:rotateSecret` creates a new version |
| Cloud Storage | V4 HMAC signed URL generate (`:generateSignedUrl`) + query-signature verify on GET/PUT media |
| Nested compute | Opt-in DinD scaffolding: `internal/compute` allowlist + TLS dial (`NOCTAXRIS_GCP_DOCKER_HOST` / `NOCTAXRIS_GCP_DOCKER_CERT_PATH`); Compose `compose.engine.yaml` (restricted) + `compose.engine-privileged.yaml`; Cloud Run soft-fail nested one-shot |
| Cloud Run | Richer `:invoke` theatre (`labStatusCode` / `labDelayMs` + env aliases); `Invoker` interface with mock default and Docker-host hooks (no host `docker.sock`; tests need no DinD) |
| Cloud Functions | Source upload accept theatre (`PUT`/`POST` generateUploadUrl path); `storageSource` create starts `DEPLOYING` then flips `ACTIVE` on upload |
| Cloud Build | Persist per-step status in build JSON (`WORKING` on create, `SUCCESS` on getBuild, `CANCELLED` on cancel) |
| Compute Engine | Instance `metadata` map normalize/return on get; firewall `:validate` allow/deny eval lite + `:testIamPermissions` |
| Pub/Sub | Subscription `deadLetterPolicy` (topic + maxDeliveryAttempts) with pull-side dead-letter fanout; `enableExactlyOnceDelivery` theatre flag stored (REST + gRPC) |
| BigQuery | `jobs.query` GROUP BY with COUNT/SUM, UNION ALL of two SELECTs, and `dataset.INFORMATION_SCHEMA.TABLES` stub |
| Firestore | Atomic Commit (SQLite all-or-nothing) with `current_document` exists/not-exists preconditions; BatchWrite coverage |
| Cloud Spanner | `:commit` mutation insert theatre (SQLite-backed rows); `:executeSql` / `:read` return inserted rows |
| Cloud Armor | Compute `securityPolicies` CRUD + `addRule`/`removeRule`; lab `byteMatchSet` + `:validate` allow/deny preview |
| Certificate Manager | certificates + certificateMaps CRUD theatre (`/v1/projects/.../locations/...`; `global` OK) |
| SDK smokes | Soft-skip HTTP coverage for Cloud Armor, Certificate Manager, Filestore, Vertex AI generateContent, IAM generateAccessToken, GCS generateSignedUrl (Go/Node/Python) |

### Changed

| Area | Change |
|------|--------|
| govulncheck | Allowlist Docker Engine Fixed-in-N/A IDs that appear with `github.com/docker/docker` client (`GO-2026-4883/4887/5617/5668`) |

### Fixed

- CI govulncheck: bump Go to 1.26.5 (clears `GO-2026-5856`); adopt Noctaxris-style `scripts/govulncheck-ci` + allowlist

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
