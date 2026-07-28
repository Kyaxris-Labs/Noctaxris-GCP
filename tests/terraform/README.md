# Terraform against Noctaxris-GCP

Soft-skips when `NOCTAXRIS_GCP_ENDPOINT` is unset, terraform is missing, or
`/_noctaxris-gcp/ready` fails.

```bash
export NOCTAXRIS_GCP_ENDPOINT=http://127.0.0.1:4588
export NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN="$ROOT_TOKEN"
bash tests/terraform/run.sh
```

Stack `lab-storage` creates a Storage bucket and Secret Manager secret via
Google provider custom endpoints. `pubsub_custom_endpoint` is set on the
provider for later use; Pub/Sub resources are omitted until a REST topic API
exists (lab Pub/Sub is gRPC-only today).
