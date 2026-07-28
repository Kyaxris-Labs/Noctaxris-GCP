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
| Other Google clients | `option.WithEndpoint("127.0.0.1:4588")` (or language equivalent) + Bearer |
| Terraform Google provider | `storage_custom_endpoint`, `pubsub_custom_endpoint`, `secret_manager_custom_endpoint` (see `tests/terraform/`) |

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
gcloud projects describe noctaxris-gcp-local --format=json
```

Full service list (including Firebase Auth and Datastore emulator hosts):
[services/index.md](services/index.md).

### Soft-skip integration smoke

SDK and Terraform under `tests/` skip when `NOCTAXRIS_GCP_ENDPOINT` is unset or
`GET {endpoint}/_noctaxris-gcp/ready` is not HTTP 200. Unit tests
(`go test ./...`) stay green without a running container.
