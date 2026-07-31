# Services

Implemented lab surface on `127.0.0.1:4588`. Status **lab** means CLI/SDK-usable
with honest emulator limits on each page.

| Service | Status | Doc | Protocol |
|---------|--------|-----|----------|
| Cloud Resource Manager | lab | [resourcemanager.md](resourcemanager.md) | REST v3 projects, org seed, folders, tag keys/bindings lite |
| IAM | lab | [iam.md](iam.md) | REST v1 service accounts/keys, WIF pool/provider + STS `/v1/token`, TokenCreator `generateAccessToken` |
| Service Usage | lab | [serviceusage.md](serviceusage.md) | REST v1 enable / disable / list / batchEnable |
| Organization Policy | lab | [orgpolicy.md](orgpolicy.md) | REST v2 policies get/set/list; boolean constraints theatre (SA keys + GCS public IAM) |
| Cloud Storage | lab | [gcs.md](gcs.md) | JSON API v1 + V4 HMAC signed URL; bucket `retentionPolicy` fail-closed delete/overwrite (`STORAGE_EMULATOR_HOST`) |
| Pub/Sub | lab | [pubsub.md](pubsub.md) | gRPC + REST topics/subscriptions/snapshots; dead-letter + exactly-once; push `oidcToken` Bearer JWT (`PUBSUB_EMULATOR_HOST`) |
| Secret Manager | lab | [secret-manager.md](secret-manager.md) | REST + gRPC; rotation config + lab `:rotateSecret` |
| Firestore | lab | [firestore.md](firestore.md) | gRPC Firestore v1; atomic Commit + BatchWrite (`FIRESTORE_EMULATOR_HOST`) |
| Cloud KMS | lab | [kms.md](kms.md) | REST v1 symmetric + RSA_SIGN_PSS sign/verify |
| Cloud Logging | lab | [logging.md](logging.md) | REST v2 entries, sinks, one-shot tail, copy theatre |
| Cloud Audit Logs | lab (theatre) | [cloud-audit-logs.md](cloud-audit-logs.md) | Env-gated inject + listable `protoPayload` lite via Logging `entries:list` |
| Security Command Center | lab | [security-command-center.md](security-command-center.md) | Sources/findings CRUD lite; lab InjectFindings (`NOCTAXRIS_GCP_SCC_INJECT`) |
| Cloud Asset Inventory | lab (theatre) | [cloud-asset-inventory.md](cloud-asset-inventory.md) | searchAllResources / listAssets / exportAssets lite over store resources; feeds + history |
| Cloud Run | lab | [cloud-run.md](cloud-run.md) | REST Admin API v2 services/jobs, traffic, IAM, `:invoke` status/delay; opt-in nested fail-closed |
| Cloud Functions | lab | [cloud-functions.md](cloud-functions.md) | REST Functions v2, upload URL + source accept, IAM, `:invoke` stub |
| Cloud Scheduler | lab | [cloud-scheduler.md](cloud-scheduler.md) | REST v1 jobs, 5-field cron next-run, pause/resume, OIDC audience |
| Cloud Tasks | lab | [cloud-tasks.md](cloud-tasks.md) | REST v2 queues/tasks, rate limits, retry, App Engine fields, `:run` |
| BigQuery | lab | [bigquery.md](bigquery.md) | REST v2 datasets/tables, insertAll, tabledata.list, jobs.query (GROUP BY / UNION / INFORMATION_SCHEMA) |
| Firebase Auth | lab | [firebase-auth.md](firebase-auth.md) | Identity Toolkit REST, OOB reset, claims, verifyIdToken |
| Cloud Monitoring | lab | [monitoring.md](monitoring.md) | REST v3 descriptors, time series, alertPolicies theatre |
| Cloud Datastore | lab | [datastore.md](datastore.md) | gRPC Datastore v1 (`DATASTORE_EMULATOR_HOST`) |
| Eventarc | lab | [eventarc.md](eventarc.md) | REST v1 triggers/channels; Pub/Sub and GCS delivery + retry |
| Artifact Registry | lab | [artifact-registry.md](artifact-registry.md) | REST v1 repos/packages/versions metadata (no blobs) |
| Cloud Build | lab | [cloud-build.md](cloud-build.md) | REST v1 createBuild theatre with step statuses + triggers CRUD lite |
| Workflows | lab | [workflows.md](workflows.md) | REST v1 workflows CRUD + executions SUCCEEDED theatre |
| Cloud Spanner | lab | [spanner.md](spanner.md) | REST v1 instances/databases; session commit insert + ExecuteSql/Read rows |
| App Engine | lab | [app-engine.md](app-engine.md) | REST Admin API v1 apps/services/versions (control-plane theatre) |
| Compute Engine | lab | [compute-engine.md](compute-engine.md) | REST compute/v1 instances (metadata) + VPC/firewall CRUD; Images list/get/family stubs; firewall `:validate` |
| Cloud Bigtable | lab | [bigtable.md](bigtable.md) | REST Admin API v2 + Instance Admin gRPC lite (instances/tables control-plane) |
| Memorystore Redis | lab | [memorystore.md](memorystore.md) | REST v1 location-scoped instances; theatre host by default; optional nested `redis:7-alpine` via DinD |
| Cloud SQL | lab | [cloud-sql.md](cloud-sql.md) | REST `/sql/v1/` instances/users/databases CRUD; POSTGRES/MYSQL; optional nested DinD |
| Managed Service for Apache Kafka | lab | [managed-kafka.md](managed-kafka.md) | REST v1 clusters CRUD; optional nested Redpanda (no host Kafka ports) |
| Filestore | lab | [filestore.md](filestore.md) | REST `/file/v1/` instances CRUD; create returns completed Operation (`done:true`; no NFS; path prefix avoids Spanner/Memorystore clash) |
| Vertex AI | lab | [vertex-ai.md](vertex-ai.md) | Publisher `:predict` / `:generateContent` canned JSON; allowlisted model ids |
| Cloud DNS | lab | [cloud-dns.md](cloud-dns.md) | REST dns/v1 managedZones + rrsets CRUD + Changes create/get/list theatre |
| Dataflow | lab | [dataflow.md](dataflow.md) | REST v1b3 jobs create/get/list theatre (no workers) |
| Cloud Armor | lab | [cloud-armor.md](cloud-armor.md) | Compute securityPolicies CRUD + ByteMatchSet `:validate` |
| Certificate Manager | lab | [certificate-manager.md](certificate-manager.md) | certificates + certificateMaps CRUD; create returns completed Operation (`done:true`; `global` OK) |
| GKE | lab | [gke.md](gke.md) | Container API v1 clusters CRUD; optional k3s one-shot with nested engine |
| HTTP(S) load balancing | lab | [load-balancing.md](load-balancing.md) | Global LB metadata + public `/lb/{project}/{rule}/...` GCS dataplane |
| Cloud CDN | lab | [cloud-cdn.md](cloud-cdn.md) | Distributions CRUD + public `/cdn/{id}/...` edge |
| Access Context Manager | lab | [access-context-manager.md](access-context-manager.md) | accessPolicies + servicePerimeters CRUD; optional VPC-SC cross-perimeter deny on GCS/Pub/Sub |

Default project id: `noctaxris-gcp-local` (`NOCTAXRIS_GCP_PROJECT`).
Seeded organization: `organizations/noctaxris-gcp-org`.

## Emulator limits (summary)

Per-service deferred depth lives on each page. Shared gaps:

- Seeded organization `organizations/noctaxris-gcp-org`; default project parent is
  that org; folders CRUD lite (no full hierarchy tooling)
- Cloud Build and Eventarc share regional `.../locations/.../triggers` (body-shape
  dispatch on create; list may merge); project-scoped triggers stay Cloud Build
- Bearer required on API paths (health/ready/version are public; Identity Toolkit
  `/identitytoolkit.googleapis.com/v1/accounts*` client methods are also public)
- Root principal bypasses IAM evaluation (lab operator)
- No host `docker.sock`; nested DinD on by default in Compose (see Nested DinD below)
- Compute Engine stores instance/VPC/firewall metadata only (no VMs or NICs); Images are a fixed canned set; firewall `:validate` is single-rule lite
- Bigtable Admin is control-plane theatre (no row mutate/read)
- Memorystore Redis uses nested Redis when Compose engine is healthy (fail-closed under Compose); bare binary without Docker host stays theatre
- Filestore is control-plane theatre under `/file/v1/` (no NFS; path prefix avoids Spanner/Memorystore clash)
- Vertex AI returns canned predict/generateContent for allowlisted model ids only
- Dataflow jobs advance state theatre only (no workers or pipeline execution)
- Cloud DNS stores zones/rrsets + in-process Changes history (no authoritative query plane)
- Cloud Armor stores securityPolicies + rules only (ByteMatchSet `:validate` theatre; no edge enforce)
- Certificate Manager stores certificates/maps metadata only (no CA issuance)
- GKE stores cluster metadata; optional k3s one-shot only (no apiserver host publish)
- HTTP(S) LB dataplane is loopback-only GCS fetch (no Internet origins)
- Cloud CDN edge is loopback-only with theatre cache headers
- Access Context Manager is perimeter CRUD theatre; enforce is opt-in env only (no network context)

## Nested DinD

Default Compose starts `noctaxris-gcp-engine` and sets `NOCTAXRIS_GCP_DOCKER_HOST` with
fail-closed nested create/invoke. Bare binary / unit tests leave the host empty
(theatre/mock; no Docker required):

```bash
cd docker
docker compose -f compose.yaml --env-file .env up --build
```

Default Compose starts restricted DinD (`noctaxris-gcp-engine`, `privileged: false`)
on the Compose network only (no host publish of 2375/2376) and sets
`NOCTAXRIS_GCP_DOCKER_HOST` / `NOCTAXRIS_GCP_DOCKER_CERT_PATH` plus fail-closed
nested envs. Nested SQL, Managed Kafka, and Memorystore Redis share the
engine-internal `noctaxris-gcp-lab` bridge (API-created; no host publish of
broker/DB ports). Cloud Run one-shot invoke stays off that bridge
(`NetworkMode: none`). Host `docker.sock`, `unix://`, and `npipe://` are rejected.
If nested containers fail on Desktop/WSL2, add `-f compose.engine-privileged.yaml`.
Nested proof: `bash docker/smoke-nested.sh`. Details:
[configuration.md](../configuration.md), [security-defaults.md](../security-defaults.md),
[architecture.md](../architecture.md), [cloud-run.md](cloud-run.md).

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
| Compute Engine | list instances in `us-central1-a` |
| Cloud DNS | list managed zones |
| Cloud Bigtable | list instances |
| Memorystore Redis | list instances in `us-central1` |
| Cloud SQL | list instances (`/sql/v1/`) |
| Managed Kafka | list clusters in `us-central1` |
| GKE | list clusters (`/container/v1/...`) |
| HTTP(S) LB | global backendServices, targetHttpsProxies; backend `securityPolicy` attach |
| Cloud CDN | list distributions |
| Dataflow | list jobs in `us-central1` |
| Cloud Armor | list securityPolicies (global) |
| Certificate Manager | list certificates in `global` |
| Filestore | list instances in `us-central1` (`/file/v1/`) |
| Vertex AI | `:generateContent` for allowlisted `gemini-1.5-flash` |
| IAM | create SA + `:generateAccessToken`; STS `/v1/token` WIF exchange; delete on cleanup |
| GCS | create bucket + `:generateSignedUrl`; retentionPolicy delete deny |
| Pub/Sub | OIDC push create/round-trip + publish (Authorization assert soft-skips without catcher dump) |
| Cloud Run | nested fail-closed `:invoke` only when `NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED` set |

Terraform apply/destroy soft-skips the same way. Stacks under
`tests/terraform/stacks/`:

| Stack | Focus |
|-------|-------|
| `lab-storage` | Cloud Storage bucket, Secret Manager secret, Pub/Sub topic |
| `lab-run` | Cloud Run v2 service |
| `lab-dns` | Cloud DNS managed zone + `google_dns_record_set` |
| `lab-compute` | Compute Engine VPC network |
| `lab-armor` | Cloud Armor `google_compute_security_policy` (SRC_IPS_V1 rules) |
| `lab-kms` | KMS key ring + crypto key |
| `lab-bigquery` | BigQuery dataset + table |
| `lab-iam` | Service account |
| `lab-sql` | Cloud SQL Postgres (nested when Compose engine healthy) |
| `lab-redis` | Memorystore Redis (nested when Compose engine healthy) |
| `lab-kafka` | Managed Kafka cluster (parity; not default `STACKS`) |
| `lab-compute-instance` | VPC + VM + boot disk (parity; not default `STACKS`) |
| `lab-lb-armor` | Armor policy + backend `security_policy` (parity; not default `STACKS`) |

Default run (`bash tests/terraform/run.sh`) applies the default `STACKS` list
(`lab-storage` … `lab-redis`). Override with `STACK=lab-armor` or
`STACKS="lab-storage lab-run"`. Parity stacks:
`STACK=lab-compute-instance` / `STACK=lab-lb-armor` / `STACK=lab-kafka`, or
`TF_GCP_PARITY=1 bash tests/run-all.sh`. Honest skips (Filestore `/file/v1/`
BaseUrl prefix, and others): [tests/terraform/README.md](../../tests/terraform/README.md).

```bash
export NOCTAXRIS_GCP_ENDPOINT=http://127.0.0.1:4588
export NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN="$ROOT_TOKEN"
go test ./tests/sdk/go/ -count=1
# node --test tests/sdk/nodejs/*.test.mjs
# pytest tests/sdk/python/
# bash tests/terraform/run.sh
# STACK=lab-armor bash tests/terraform/run.sh
# STACK=lab-dns bash tests/terraform/run.sh
# STACK=lab-compute bash tests/terraform/run.sh
# STACK=lab-compute-instance bash tests/terraform/run.sh
# STACK=lab-lb-armor bash tests/terraform/run.sh
# STACK=lab-kafka bash tests/terraform/run.sh
# TF_GCP_PARITY=1 bash tests/run-all.sh
```

## gcloud `api_endpoint_overrides`

Point selected command groups at the lab (then use
`CLOUDSDK_AUTH_ACCESS_TOKEN` with the root Bearer):

```bash
gcloud config set api_endpoint_overrides/cloudresourcemanager http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/iam http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/serviceusage http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/orgpolicy http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/storage http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/pubsub http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/secretmanager http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/firestore http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/cloudkms http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/logging http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/cloudasset http://127.0.0.1:4588/
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
gcloud config set api_endpoint_overrides/compute http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/dns http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/dataflow http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/bigtableadmin http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/redis http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/sqladmin http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/container http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/certificatemanager http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/aiplatform http://127.0.0.1:4588/
# Filestore lab paths are under /file/v1/ — use filestore_custom_endpoint = "http://127.0.0.1:4588/file/v1/"
# (see tests/terraform/README.md for BaseUrl prefix skip; create returns completed Operation)
# (bare api_endpoint_overrides/file to :4588/ alone misses the /file prefix)
# Managed Kafka / LB / CDN: REST on :4588 (see managed-kafka.md, load-balancing.md, cloud-cdn.md)
```

Firebase Auth and Datastore prefer emulator host env vars
(`FIREBASE_AUTH_EMULATOR_HOST`, `DATASTORE_EMULATOR_HOST`) rather than gcloud
endpoint overrides. See [configuration.md](../configuration.md).

## Verification

Unit tests and live SDK / Terraform suites:

```bash
go test ./...
bash tests/run-all.sh   # needs Compose up; see tests/README.md
```

Quick probes:

```bash
curl http://127.0.0.1:4588/_noctaxris-gcp/health
curl http://127.0.0.1:4588/_noctaxris-gcp/ready
curl -H "Authorization: Bearer $ROOT_TOKEN" \
  http://127.0.0.1:4588/v3/projects/noctaxris-gcp-local
```

Docs index: [../index.md](../index.md).
