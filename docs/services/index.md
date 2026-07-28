# Services

Implemented Wave 1 surface on `127.0.0.1:4588`. Status **lab** means CLI/SDK-usable
with honest emulator limits on each page.

| Service | Status | Doc |
|---------|--------|-----|
| Cloud Resource Manager | lab | [resourcemanager.md](resourcemanager.md) |
| IAM | lab | [iam.md](iam.md) |
| Service Usage | lab | [serviceusage.md](serviceusage.md) |
| Cloud Storage | lab | [gcs.md](gcs.md) |
| Pub/Sub | lab | [pubsub.md](pubsub.md) |
| Secret Manager | lab | [secret-manager.md](secret-manager.md) |
| Firestore | lab | [firestore.md](firestore.md) |
| Cloud KMS | lab | [kms.md](kms.md) |
| Cloud Logging | lab | [logging.md](logging.md) |

Default project id: `noctaxris-gcp-local` (`NOCTAXRIS_GCP_PROJECT`).

## Emulator limits (summary)

Per-service deferred depth lives on each page. Shared gaps:

- Single seeded project; no orgs/folders
- Bearer required on API paths (health/ready/version are public)
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
```

See also [configuration.md](../configuration.md).

## Verification

```bash
go test ./...
curl http://127.0.0.1:4588/_noctaxris-gcp/health
curl http://127.0.0.1:4588/_noctaxris-gcp/ready
curl -H "Authorization: Bearer $ROOT_TOKEN" \
  http://127.0.0.1:4588/v3/projects/noctaxris-gcp-local
```
