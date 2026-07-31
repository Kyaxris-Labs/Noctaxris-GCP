# Changelog

## [Unreleased]

## [1.0.1] - 2026-07-31

Default Compose turns nested DinD on; Terraform and SDK suites widen accordingly.

### Added

| Area | Change |
|------|--------|
| Compose | Nested DinD (`noctaxris-gcp-engine`) in default `compose.yaml` with fail-closed nested create/invoke envs; `compose.lab-host-gateway.yaml`; `docker/smoke-nested.sh` |
| CI | PR `ci.yml` gains compose-static, scoped race, smoke-core (engine on), path-filtered SDK/TF integration, weekly/dispatch nested smoke |
| Terraform | Default stacks `lab-kms`, `lab-bigquery`, `lab-iam`, `lab-sql`, `lab-redis` (Managed Kafka remains honest skip) |
| SDK tests | Multi-file Go/Node/Python smokes with Node/Python gap fill vs Go |
| Memorystore | Delete returns done Operation (Terraform destroy waiters) |
| GKE | Nested k3s one-shot on create budgets ~2s (soft-fail theatre on pull timeout) |
| Cloud Armor | `setLabels` DONE Operation so Terraform provider apply completes |
| Terraform | `big_query_custom_endpoint` name; `lab-iam` pins google `~> 5.45` + listener-root IAM endpoint |

### Changed

| Area | Change |
|------|--------|
| Nested defaults | Compose operators get fail-closed nested SQL/Kafka/Redis/Run; bare binary / unit tests still soft-fail when `DOCKER_HOST` empty |
| Docs | ops/configuration/security-defaults/services index + README Defaults describe default-on DinD |
| Node SDK | `engines.node` minimum `>=24` |

## [1.0.0] - 2026-07-30

First major release after the hybrid lab-complete surface (identity, storage, serverless, nested SQL/Kafka/Redis/GKE, forensics/policy products, Hub release CI).

### Added

| Area | Change |
|------|--------|
| Cloud Asset Inventory | `searchAllResources` / `listAssets` over projects, buckets, topics, SAs; `exportAssets` done-LRO theatre; feeds CRUD + history for `batchGetAssetsHistory` |
| Cloud Audit Logs | Lab inject `POST /_noctaxris-gcp/lab/auditLogs:inject` (`NOCTAXRIS_GCP_AUDIT_INJECT=1` + Bearer root); SQLite CAL rows with `protoPayload` lite; list via Logging `entries:list` on `cloudaudit.googleapis.com` logNames; optional live `audit.Writer` sink mirror |
| Access Context Manager | VPC Service Controls perimeter lite: accessPolicies + servicePerimeters CRUD theatre; optional `NOCTAXRIS_GCP_VPCSC_ENFORCE` denies cross-perimeter GCS upload/copy and Pub/Sub publish (incl. notification fanout) |
| Organization Policy | REST v2 policies get/set/list on org/folder/project; constraints `iam.disableServiceAccountKeyCreation` + `storage.publicAccessPrevention`; IAM createKey and GCS public setIamPolicy enforce hooks |
| Security Command Center | Sources/findings CRUD lite (org + project); lab `InjectFindings` (`NOCTAXRIS_GCP_SCC_INJECT=1`, default off) |
| IAM | Opt-in STS OIDC verify (`NOCTAXRIS_GCP_STS_VERIFY=1`): RS256 JWT + iss/aud/exp via JWKS/discovery fetched only through `httpegress`; default theatre keeps any non-empty `subject_token` |
| IAM | STS `POST /v1/token` WIF token-exchange theatre; `roles/iam.serviceAccountTokenCreator` on `generateAccessToken`; project custom roles CRUD with `includedPermissions` |
| Authz | Org/folder IAM inheritance in Evaluate (CRM ancestry walk) |
| HTTP catcher | Public `POST`/`GET /_noctaxris-gcp/http-catcher` accept + dump; Scheduler and Cloud Tasks short-circuit `IsLabCatcher` |
| Scheduler / Tasks / Eventarc | Authenticated interservice dispatch: persist SA fields; mint registered lab Bearer on fire/dispatch/deliver to loopback Run/Functions `:invoke` |
| Cloud Run / Functions | `:invoke` Invoker on service/function resource (`EvaluateAny`); seed `roles/run.invoker` / `roles/cloudfunctions.invoker` |
| Cloud Functions + Eventarc | Functions v2 create with event trigger inserts Eventarc trigger (`destination.cloudFunction`); in-process deliver on Pub/Sub/GCS match |
| HTTP egress | Shared `internal/kernel/httpegress` gate for Pub/Sub push, Eventarc HTTP, Cloud Tasks, Scheduler; lab catcher + loopback `:4588`; opt-in egress + exact allowlist; no redirects |
| Cloud Storage | Bucket `retentionPolicy` fail-closed delete/overwrite; `notificationConfigs` CRUD with `OBJECT_FINALIZE` / `OBJECT_DELETE` Pub/Sub publish |
| Pub/Sub | Push `oidcToken` theatre JWT; push DLQ counters aligned with pull |
| Cloud SQL | REST `/sql/v1/` (+ `/sql/v1beta4/`) instances/users/databases; `sql#operation` DONE + Operations.get; optional nested Postgres/MySQL |
| Managed Kafka | Clusters + topics CRUD; ACL metadata theatre; optional nested Redpanda; best-effort `rpk topic create` |
| Memorystore | Nested Redis on shared `noctaxris-gcp-lab`; AUTH (`authEnabled`/`authString`); create returns completed Operation + Operations.get |
| GKE | Container API v1 clusters CRUD; optional k3s one-shot with nested engine |
| HTTP(S) LB / Cloud CDN | Backend/urlMap/forwardingRule metadata; public `/lb/...` and `/cdn/...` edges |
| Bigtable | Instance Admin gRPC lite; Create returns done Operation |
| Certificate Manager / Filestore | Create returns completed Operation; Operations.get immediate done theatre |
| Cloud Build | Regional triggers via shared mux with Eventarc |
| Cloud DNS | `changes.create` / `get` / `list` theatre applies rrset edits |
| Compute Engine | Global Images list/get/family stubs for Terraform ResolveImage |
| CI | Release gates (`ci-required.yml`) + Hub publish (`release.yml` on `v*`, `docker-nightly.yml`) for `kyaxris/noctaxris-gcp` |

### Changed

| Area | Change |
|------|--------|
| Nested engine | Opt-in `NOCTAXRIS_GCP_NESTED_ENGINE_FAIL_CLOSED` for SQL/Kafka/Redis create (FAILED_PRECONDITION + rollback); default soft-fail theatre |
| Nested lab network | Memorystore Redis shares DinD bridge `noctaxris-gcp-lab` with SQL/Kafka (no host broker/DB publish) |
| Service Usage | Seed `managedkafka.googleapis.com`; gate SQL/Kafka/GKE/Pub/Sub topic/GCS bucket creates when DISABLED |
| Firebase Auth | Client `accounts:delete` / `accounts:update` require matching lab `idToken` |
| IAM | Narrowed `{svc}.*` predefined grants; `roles/viewer` / `roles/editor` least-privilege tighten |
| Image allowlist | Exact refs or trailing-`/` prefixes with digest; pinned nested SQL/Redis images |
| Signed URL middleware | Bearer skip limited to `/storage/` and `/upload/storage/` |
| SQLite | `SetMaxOpenConns(1)` + WAL for Eventarc delivery concurrency |
| Dependencies | firestore, iam, secretmanager, genproto, otelhttp, modernc.org/libc bumps (go 1.26.5) |

### Fixed

| Area | Change |
|------|--------|
| Ready probe | Align with Noctaxris: SQLite `Ping`, optional nested-engine ping when `NOCTAXRIS_GCP_DOCKER_HOST` set, body `ready`; `ci-required` wait uses the same `curl \| grep -q ready` loop |
| Authz | Nil `Evaluator` fail-closed for non-root |
| Create handlers | Get-after-create miss returns 500 (nested/edge services + Filestore/Spanner/Cert Manager/Build/Tasks) |
| CRM folders | Folder IDs use UUID hex instead of `UnixNano` (Windows clock collide-on-create) |
| Security | `IsPublicPath` path.Clean before `/cdn/` and `/lb/` public prefixes |

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
