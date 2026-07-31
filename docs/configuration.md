# Configuration

All settings use the `NOCTAXRIS_GCP_*` prefix.

| Variable | Default | Description |
|----------|---------|-------------|
| `NOCTAXRIS_GCP_LISTEN` | `127.0.0.1:4588` | Bind address |
| `NOCTAXRIS_GCP_DATA_ROOT` | `/var/lib/noctaxris-gcp` | SQLite + object blobs + audit |
| `NOCTAXRIS_GCP_MASTER_KEY_FILE` | sibling `…-secrets/master.key` | 32-byte ChaCha20-Poly1305 key path (outside data root) |
| `NOCTAXRIS_GCP_TLS_CERT` | empty | Optional TLS certificate PEM |
| `NOCTAXRIS_GCP_TLS_KEY` | empty | Optional TLS private key PEM |
| `NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT` | required at startup | Root principal email |
| `NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN` | required at startup | Root Bearer token (held in memory) |
| `NOCTAXRIS_GCP_PROJECT` | `noctaxris-gcp-local` | Default project seeded by EnsureRoot |
| `NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN` | unset / false | Permit non-loopback listen without TLS (Compose) |
| `NOCTAXRIS_GCP_ALLOW_MASTER_KEY_IN_DATA_ROOT` | unset / false | Permit master key under data root |
| `NOCTAXRIS_GCP_DOCKER_HOST` | empty | Nested DinD engine URL. Empty disables nested compute (default; unit tests need no Docker). Rejects `unix://`, `npipe://`, and `docker.sock`. Must be `tcp://noctaxris-gcp-engine:2376` or an entry in `NOCTAXRIS_GCP_DOCKER_HOST_ALLOWLIST`. |
| `NOCTAXRIS_GCP_DOCKER_CERT_PATH` | empty | Directory with `ca.pem`, `cert.pem`, and `key.pem` for engine TLS. Required whenever `NOCTAXRIS_GCP_DOCKER_HOST` is set. |
| `NOCTAXRIS_GCP_DOCKER_HOST_ALLOWLIST` | empty | Comma-separated exact `tcp://` URLs allowed in addition to `tcp://noctaxris-gcp-engine:2376`. |
| `NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED` | unset / false | Set to `1` or `true` so Cloud Run nested `:invoke` returns an error when the engine dial/run fails instead of soft-failing to mock with `engine.mode: mock` in the body. Default unset keeps soft-fail (unit tests without DinD stay green). |
| `NOCTAXRIS_GCP_NESTED_ENGINE_FAIL_CLOSED` | unset / false | Set to `1` or `true` so Cloud SQL, Managed Kafka, and Memorystore Redis create returns `FAILED_PRECONDITION` when the nested engine is enabled but dial/start fails (resource is rolled back). Default unset keeps soft-fail to theatre host/READY/ACTIVE (unit tests without DinD stay green). Distinct from `NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED`. |
| `NOCTAXRIS_GCP_IMAGE_PULL_ALLOWLIST` | empty | Comma-separated exact image refs, or registry prefixes ending in `/` (digest `@sha256:` required for registry hosts). Bare substring prefixes without a trailing `/` are rejected. |
| `NOCTAXRIS_GCP_HTTP_EGRESS` | empty (off) | Set to `1` to honor `NOCTAXRIS_GCP_HTTP_ALLOWLIST` for Pub/Sub push, Eventarc HTTP, Cloud Tasks, Scheduler, and STS OIDC JWKS/discovery fetches beyond lab-local URLs. Unset/off: only `http://127.0.0.1:4588/_noctaxris-gcp/http-catcher...`, loopback `:4588` `/_noctaxris-gcp/oidc-lab/.well-known/...`, and other loopback `:4588` lab-local URLs (allowlist ignored). |
| `NOCTAXRIS_GCP_HTTP_ALLOWLIST` | empty | Comma-separated exact HTTP(S) URLs allowed when egress is on. Listed URLs still reject private, loopback, link-local, and metadata hosts; delivery does not follow redirects. Ignored when egress is off. For STS verify, allowlist both the OIDC discovery URL and `jwks_uri` (exact match). |
| `NOCTAXRIS_GCP_STS_VERIFY` | empty (off) | Set to `1` or `true` to fail-closed verify WIF STS `subject_token` as RS256 JWT when the provider has `issuerUri` (JWKS/discovery via `httpegress`). Default off keeps any non-empty token theatre so unit tests and smoke stay green. Empty `issuerUri` stays theatre even when verify is on. Built-in issuer `http://127.0.0.1:4588/_noctaxris-gcp/oidc-lab` (discovery + JWKS on the lab listener) needs no egress allowlist on loopback. |
| `NOCTAXRIS_GCP_AUDIT_INJECT` | empty (off) | Set to `1` or `true` to enable lab `POST /_noctaxris-gcp/lab/auditLogs:inject` (still requires Bearer root). Default off returns `PERMISSION_DENIED`. See [services/cloud-audit-logs.md](services/cloud-audit-logs.md). |
| `NOCTAXRIS_GCP_SCC_INJECT` | empty (off) | Set to `1` or `true` to enable lab `POST /_noctaxris-gcp/lab/securitycenter:injectFindings`. Default off returns PermissionDenied. |
| `NOCTAXRIS_GCP_VPCSC_ENFORCE` | empty (off) | Set to `1` or `true` to deny cross-perimeter GCS upload/copy and Pub/Sub publish when a service perimeter restricts those APIs (dry-run `spec` included). Default off keeps Access Context Manager CRUD theatre only. See [services/access-context-manager.md](services/access-context-manager.md). |

EnsureRoot also seeds lab organization `organizations/noctaxris-gcp-org` (folders
CRUD lite attaches under that parent). See [services/resourcemanager.md](services/resourcemanager.md).

## Compose

`docker/compose.yaml` sets:

- `NOCTAXRIS_GCP_LISTEN=0.0.0.0:4588`
- `NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN=1`
- `NOCTAXRIS_GCP_DATA_ROOT=/var/lib/noctaxris-gcp`
- `NOCTAXRIS_GCP_MASTER_KEY_FILE=/var/lib/noctaxris-gcp-secrets/master.key`
- Host publish `${NOCTAXRIS_GCP_PUBLISH_ADDR:-127.0.0.1}:4588:4588`
- Volumes: data + secrets + engine certs
- `read_only: true` and `tmpfs: /tmp`
- No `docker.sock`
- Nested engine on by default (`NOCTAXRIS_GCP_DOCKER_HOST=tcp://noctaxris-gcp-engine:2376`)
- Compose fail-closed: `NOCTAXRIS_GCP_NESTED_ENGINE_FAIL_CLOSED=1`, `NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED=1`

### Nested engine

From `docker/`:

```bash
docker compose -f compose.yaml --env-file .env up --build
```

Default Compose starts restricted DinD (`noctaxris-gcp-engine`, `privileged: false`)
and sets `NOCTAXRIS_GCP_DOCKER_HOST` / `NOCTAXRIS_GCP_DOCKER_CERT_PATH`. The engine
API stays on the Compose network (no host publish of 2375/2376). Bare binary /
unit tests leave `NOCTAXRIS_GCP_DOCKER_HOST` empty (mock/theatre). If nested
containers fail on your host:

```bash
docker compose -f compose.yaml -f compose.engine-privileged.yaml --env-file .env up --build
```

`compose.engine.yaml` is a thin compatibility overlay (API env + depends_on only;
engine already in base). Prefer plain `compose.yaml`. See [security-defaults.md](security-defaults.md).

Copy `docker/.env.example` to `docker/.env` and replace the example root pair
before starting. Startup refuses that pair on the non-loopback container bind.

## Client endpoints

| Client | How to point at the lab |
|--------|-------------------------|
| curl / raw HTTP | `http://127.0.0.1:4588` + `Authorization: Bearer <token>` |
| Cloud Storage SDK | `STORAGE_EMULATOR_HOST=127.0.0.1:4588` |
| Pub/Sub SDK | `PUBSUB_EMULATOR_HOST=127.0.0.1:4588` |
| Firestore SDK | `FIRESTORE_EMULATOR_HOST=127.0.0.1:4588` (when the client honors it) |
| Datastore SDK | `DATASTORE_EMULATOR_HOST=127.0.0.1:4588` |
| Firebase Auth | `FIREBASE_AUTH_EMULATOR_HOST=127.0.0.1:4588` |
| Secret Manager / KMS / Logging | `option.WithEndpoint("127.0.0.1:4588")` + Bearer |
| Cloud Run / Functions / Scheduler / Tasks | `option.WithEndpoint("127.0.0.1:4588")` + Bearer |
| BigQuery / Monitoring / Eventarc | `option.WithEndpoint("127.0.0.1:4588")` + Bearer |
| Artifact Registry / Cloud Build / Workflows / Spanner / App Engine | `option.WithEndpoint("127.0.0.1:4588")` + Bearer |
| Compute Engine | `option.WithEndpoint("127.0.0.1:4588")` + Bearer (REST `/compute/v1/...`; no nested VMs) |
| Cloud DNS | `option.WithEndpoint("127.0.0.1:4588")` + Bearer (REST `/dns/v1/...`) |
| Dataflow | `option.WithEndpoint("127.0.0.1:4588")` + Bearer (REST `/v1b3/...`; job theatre only) |
| Cloud Bigtable Admin | `option.WithEndpoint("127.0.0.1:4588")` + Bearer (REST Admin API v2; no data plane) |
| Memorystore Redis | `option.WithEndpoint("127.0.0.1:4588")` + Bearer (REST `/v1/.../locations/.../instances`; theatre host by default; optional nested Redis via DinD) |
| Cloud SQL | `option.WithEndpoint("127.0.0.1:4588")` + Bearer (REST `/sql/v1/...`; optional nested Postgres/MySQL via DinD) |
| Managed Kafka | `option.WithEndpoint("127.0.0.1:4588")` + Bearer (REST `/v1/.../locations/.../clusters`; optional nested Redpanda via DinD) |
| GKE | `option.WithEndpoint("127.0.0.1:4588")` + Bearer (REST `/container/v1/.../clusters`; optional k3s one-shot via DinD) |
| HTTP(S) LB / Cloud CDN | Bearer on control plane; public loopback dataplane `/lb/...` and `/cdn/...` on `:4588` |
| Filestore | Base URL must include `/file/v1/` (e.g. `http://127.0.0.1:4588/file/v1/`); bare host misses the lab path prefix (no NFS) |
| Vertex AI | `option.WithEndpoint("127.0.0.1:4588")` + Bearer (REST `/v1/projects/.../publishers/google/models/{id}:predict` / `:generateContent`; allowlisted model ids only) |
| Cloud Armor | Same Compute Engine endpoint (`/compute/v1/.../global/securityPolicies`); ByteMatchSet `:validate` is lab preview only (no edge enforce) |
| Certificate Manager | `option.WithEndpoint("127.0.0.1:4588")` + Bearer (REST `/v1/projects/.../locations/...`; create returns completed Operation) |
| Other Google clients | `option.WithEndpoint("127.0.0.1:4588")` (or language equivalent) + Bearer |
| Terraform Google provider | Custom endpoints with versioned path suffixes (see below) |

Cloud Build and Eventarc share regional
`.../locations/{loc}/triggers` on the mux: create selects by body shape
(Eventarc keys vs Cloud Build). Project-scoped `/v1/projects/{p}/triggers` remains
Cloud Build only. See [services/cloud-build.md](services/cloud-build.md) and
[services/eventarc.md](services/eventarc.md).

### Terraform custom endpoints

Point hashicorp/google at the lab (Bearer via `GOOGLE_OAUTH_ACCESS_TOKEN`).
Suffixes match provider product BaseUrl defaults:

```hcl
provider "google" {
  project = "noctaxris-gcp-local"
  region  = "us-central1"

  storage_custom_endpoint        = "http://127.0.0.1:4588/storage/v1/"
  pubsub_custom_endpoint         = "http://127.0.0.1:4588/v1/"
  secret_manager_custom_endpoint = "http://127.0.0.1:4588/v1/"
  cloud_run_v2_custom_endpoint   = "http://127.0.0.1:4588/v2/"
  dns_custom_endpoint            = "http://127.0.0.1:4588/dns/v1/"
  compute_custom_endpoint        = "http://127.0.0.1:4588/compute/v1/"
  # Filestore: provider field is filestore_custom_endpoint (BaseUrl …/v1/).
  # Lab mounts under /file/v1/ (Spanner owns bare /v1/.../instances; Memorystore
  # is location-scoped), so the override must be http://127.0.0.1:4588/file/v1/.
  # Create returns completed Operation theatre; remaining stack gap is the
  # /file/v1/ BaseUrl prefix. See tests/terraform/README.md honest skips.
}
```

| Stack | Resources | Endpoints |
|-------|-----------|-----------|
| `tests/terraform/stacks/lab-storage` | GCS bucket, Secret Manager secret, Pub/Sub topic | `storage` / `pubsub` / `secret_manager` |
| `tests/terraform/stacks/lab-run` | `google_cloud_run_v2_service` (metadata theatre; no containers) | `cloud_run_v2` |
| `tests/terraform/stacks/lab-dns` | `google_dns_managed_zone`, `google_dns_record_set` | `dns` (`…/dns/v1/`) |
| `tests/terraform/stacks/lab-compute` | `google_compute_network` (no VMs) | `compute` (`…/compute/v1/`) |
| `tests/terraform/stacks/lab-armor` | `google_compute_security_policy` (Cloud Armor; `SRC_IPS_V1` rules) | `compute` (`…/compute/v1/`); attribution label off |
| `tests/terraform/stacks/lab-kms` | KMS key ring + crypto key | `kms` |
| `tests/terraform/stacks/lab-bigquery` | BigQuery dataset + table | `big_query` (`…/bigquery/v2/`) |
| `tests/terraform/stacks/lab-iam` | Service account | `iam` (listener root) |
| `tests/terraform/stacks/lab-sql` | Cloud SQL Postgres | `sql` (`…/sql/v1beta4/`) |
| `tests/terraform/stacks/lab-redis` | Memorystore Redis | `redis` |
| `tests/terraform/stacks/lab-kafka` | Managed Kafka cluster (parity; not default `STACKS`) | `managed_kafka` (`…/v1/`) |
| `tests/terraform/stacks/lab-compute-instance` | VPC + VM + boot disk (parity) | `compute` |
| `tests/terraform/stacks/lab-lb-armor` | Armor policy + backend `security_policy` (parity) | `compute` |

Cloud Armor is Compute-shaped (`securityPolicies` under `compute_custom_endpoint`).
Lab `byteMatchSet` + `:validate` preview allow/deny; backend attach is metadata via
`securityPolicy` / `setSecurityPolicy` (parity stack `lab-lb-armor`). Filestore Terraform must use
`filestore_custom_endpoint = "http://127.0.0.1:4588/file/v1/"` (not bare `:4588/`
and not Spanner `/v1/.../instances` or Memorystore location-scoped paths). Vertex AI
has no Terraform stack; call REST `:predict` / `:generateContent` with
`api_endpoint_overrides/aiplatform`.

Default `run.sh` stacks omit `lab-kafka`, `lab-compute-instance`, and `lab-lb-armor`
(use `STACK=` or `TF_GCP_PARITY=1`). Not stacked: Bigtable (provider
gRPC admin client vs lab REST `/v2/` + Instance Admin gRPC lite), Filestore (lab
`/file/v1/` path prefix vs provider BaseUrl `…/v1/`). Certificate Manager create
returns completed Operation theatre. See `tests/terraform/README.md`.

### gcloud `api_endpoint_overrides`

```bash
export CLOUDSDK_AUTH_ACCESS_TOKEN="$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN"
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
gcloud config set api_endpoint_overrides/compute http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/dns http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/dataflow http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/bigtableadmin http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/redis http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/sqladmin http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/container http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/certificatemanager http://127.0.0.1:4588/
gcloud config set api_endpoint_overrides/aiplatform http://127.0.0.1:4588/
# Filestore: filestore_custom_endpoint = "http://127.0.0.1:4588/file/v1/" (lab /file/v1/ path prefix)
# Managed Kafka / LB / CDN: REST on :4588 (see docs/services/)
gcloud projects describe noctaxris-gcp-local --format=json
```

Full service list (including Firebase Auth and Datastore emulator hosts):
[services/index.md](services/index.md).

### Soft-skip integration smoke

SDK and Terraform under `tests/` skip when `NOCTAXRIS_GCP_ENDPOINT` is unset or
`GET {endpoint}/_noctaxris-gcp/ready` is not HTTP 200. Authenticated cases also
soft-skip when `NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN` is unset. Unit tests
(`go test ./...`) stay green without a running container.

| Variable | Role |
|----------|------|
| `NOCTAXRIS_GCP_ENDPOINT` | Lab base URL (e.g. `http://127.0.0.1:4588`); required for live smoke |
| `NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN` | Bearer for authenticated SDK/Terraform cases |
| `NOCTAXRIS_GCP_PROJECT` | Optional; defaults to `noctaxris-gcp-local` |
| `NOCTAXRIS_GCP_NESTED` | Optional; set to `1` when running `tests/run-all.sh` so nested/DinD-oriented SDK rows stay enabled (still soft-skip without a healthy engine) |

```bash
export NOCTAXRIS_GCP_ENDPOINT=http://127.0.0.1:4588
export NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN="$ROOT_TOKEN"
# optional: export NOCTAXRIS_GCP_PROJECT=noctaxris-gcp-local
go test ./tests/sdk/go/ -count=1
# node --test tests/sdk/nodejs/*.test.mjs
# pytest tests/sdk/python/
# bash tests/terraform/run.sh
```
