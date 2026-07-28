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

EnsureRoot also seeds lab organization `organizations/noctaxris-gcp-org` (folders
CRUD lite attaches under that parent). See [services/resourcemanager.md](services/resourcemanager.md).

## Compose

`docker/compose.yaml` sets:

- `NOCTAXRIS_GCP_LISTEN=0.0.0.0:4588`
- `NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN=1`
- `NOCTAXRIS_GCP_DATA_ROOT=/var/lib/noctaxris-gcp`
- `NOCTAXRIS_GCP_MASTER_KEY_FILE=/var/lib/noctaxris-gcp-secrets/master.key`
- Host publish `127.0.0.1:4588:4588`
- Volumes: data + secrets
- `read_only: true` and `tmpfs: /tmp`
- No `docker.sock`

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
| Memorystore Redis | `option.WithEndpoint("127.0.0.1:4588")` + Bearer (REST `/v1/...`; no Redis process) |
| Other Google clients | `option.WithEndpoint("127.0.0.1:4588")` (or language equivalent) + Bearer |
| Terraform Google provider | Custom endpoints with versioned path suffixes (see below) |

Cloud Build triggers use classic project-scoped paths
(`POST/GET/DELETE /v1/projects/{p}/triggers[/{id}]`). Regional
`.../locations/{loc}/triggers` is owned by Eventarc on the shared mux and is not
mounted for Cloud Build. See [services/cloud-build.md](services/cloud-build.md).

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
}
```

| Stack | Resources | Endpoints |
|-------|-----------|-----------|
| `tests/terraform/stacks/lab-storage` | GCS bucket, Secret Manager secret, Pub/Sub topic | `storage` / `pubsub` / `secret_manager` |
| `tests/terraform/stacks/lab-run` | `google_cloud_run_v2_service` (metadata theatre; no containers) | `cloud_run_v2` |
| `tests/terraform/stacks/lab-dns` | `google_dns_managed_zone` | `dns` (`…/dns/v1/`) |
| `tests/terraform/stacks/lab-compute` | `google_compute_network` (no VMs) | `compute` (`…/compute/v1/`) |

Not stacked: DNS record sets (provider `Changes.create`; lab has no Changes API),
Compute instances (Images API / `ResolveImage`), Bigtable (provider gRPC admin
client vs lab REST `/v2/`). See `tests/terraform/README.md`.

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

```bash
export NOCTAXRIS_GCP_ENDPOINT=http://127.0.0.1:4588
export NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN="$ROOT_TOKEN"
# optional: export NOCTAXRIS_GCP_PROJECT=noctaxris-gcp-local
go test ./tests/sdk/go/ -count=1
# node --test tests/sdk/nodejs/*.test.mjs
# pytest tests/sdk/python/
# bash tests/terraform/run.sh
```
