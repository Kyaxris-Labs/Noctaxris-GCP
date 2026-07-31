# Terraform against Noctaxris-GCP

Soft-skips when `NOCTAXRIS_GCP_ENDPOINT` is unset, terraform is missing,
`NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN` is unset, or `/_noctaxris-gcp/ready` fails.

```bash
export NOCTAXRIS_GCP_ENDPOINT=http://127.0.0.1:4588
export NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN="$ROOT_TOKEN"
bash tests/terraform/run.sh
```

On Windows without bash, soft-skip is the default when the endpoint is unset.
With Git Bash or WSL, use the same `bash tests/terraform/run.sh` path. PowerShell
can only soft-skip today (no native runner):

```powershell
if (-not $env:NOCTAXRIS_GCP_ENDPOINT) { Write-Host "NOCTAXRIS_GCP_ENDPOINT unset — skip Terraform suite" }
```

## Stacks

| Stack | Resources | Provider custom endpoints |
|-------|-----------|---------------------------|
| `lab-storage` (default set) | GCS bucket, Secret Manager secret, Pub/Sub topic | `storage_custom_endpoint` (`…/storage/v1/`), `secret_manager_custom_endpoint` (`…/v1/`), `pubsub_custom_endpoint` (`…/v1/`) |
| `lab-run` | Cloud Run v2 service (metadata theatre; no containers) | `cloud_run_v2_custom_endpoint` (`…/v2/`) |
| `lab-dns` | Cloud DNS managed zone + `google_dns_record_set` (Changes) | `dns_custom_endpoint` (`…/dns/v1/`) |
| `lab-compute` | Compute VPC network (metadata theatre; no VMs) | `compute_custom_endpoint` (`…/compute/v1/`) |
| `lab-armor` | `google_compute_security_policy` (Cloud Armor; SRC_IPS_V1 rules) | `compute_custom_endpoint` (`…/compute/v1/`); lab `setLabels` DONE Operation |
| `lab-kms` | KMS key ring + crypto key | `kms_custom_endpoint` (`…/v1/`) |
| `lab-bigquery` | BigQuery dataset + table | `big_query_custom_endpoint` (`…/bigquery/v2/`) |
| `lab-iam` | Service account | `iam_custom_endpoint` listener root (`…/`); provider `~> 5.45` (REST; google >=6 uses IAM gRPC) |
| `lab-sql` | Cloud SQL Postgres instance (nested when Compose engine healthy) | `sql_custom_endpoint` (`…/sql/v1beta4/`) |
| `lab-redis` | Memorystore Redis instance (nested when Compose engine healthy; delete returns done Operation) | `redis_custom_endpoint` (`…/v1/`) |
| `lab-kafka` | Managed Kafka cluster (`capacity_config` + `gcp_config.access_config.network_configs`; nested Redpanda when Compose engine healthy). **Parity-only** until default Compose + fail-closed nested apply+destroy earns default `STACKS` (`STACK=lab-kafka` or `TF_GCP_PARITY=1`). | `managed_kafka_custom_endpoint` (`…/v1/`) |
| `lab-compute-instance` | VPC + subnetwork + `google_compute_instance` with boot disk (metadata theatre; zone/region/global Operations.get for provider wait). **Parity-only** (`STACK=lab-compute-instance` or `TF_GCP_PARITY=1`). | `compute_custom_endpoint` (`…/compute/v1/`) |
| `lab-lb-armor` | Minimal Armor attach: `google_compute_security_policy` + `google_compute_backend_service` (`security_policy`; lab `setSecurityPolicy` + DONE operations). **Parity-only** (`STACK=lab-lb-armor` or `TF_GCP_PARITY=1`). | `compute_custom_endpoint` (`…/compute/v1/`); attribution label off |

Secrets stay in `lab-storage` (no separate `lab-secrets` stack). Auth uses
`GOOGLE_OAUTH_ACCESS_TOKEN` set from the root Bearer by `run.sh`.

Default Compose starts nested DinD; `lab-sql` / `lab-redis` expect
fail-closed nested create when the engine is up (`NOCTAXRIS_GCP_NESTED_ENGINE_FAIL_CLOSED`).
`lab-kafka`, `lab-compute-instance`, and `lab-lb-armor` stay out of the default
`STACKS` list (`lab-kafka` until nested fail-closed earn; the other two stay opt-in parity).

```bash
# one stack
STACK=lab-armor bash tests/terraform/run.sh

# subset
STACKS="lab-storage lab-kms lab-sql" bash tests/terraform/run.sh

# parity stacks (lab-compute-instance, lab-lb-armor, lab-kafka)
STACK=lab-compute-instance bash tests/terraform/run.sh
STACK=lab-lb-armor bash tests/terraform/run.sh
STACK=lab-kafka bash tests/terraform/run.sh
TF_GCP_PARITY=1 bash tests/run-all.sh
```

## Honest skips

| Gap | Why not a stack |
|-----|-----------------|
| `google_compute_instance` (default set) | Default `lab-compute` is VPC-only; full VM + boot disk is parity stack `lab-compute-instance` (`STACK=` / `TF_GCP_PARITY=1`). |
| `google_bigtable_*` | Instance Admin gRPC lite is present; still no Table Admin gRPC / row mutate. |
| `google_filestore_instance` | Provider BaseUrl is `https://file.googleapis.com/v1/`; lab mounts under `/file/v1/`. |
| GKE / Cloud CDN TF stacks | GKE lab path is `/container/v1/...`; CDN uses lab GCS dataplane shapes, not standard NEG/backend-bucket stacks. |
| Full HTTP(S) LB TF (default set) | Parity stack `lab-lb-armor` covers backend `security_policy` attach only; not in default `STACKS`. |
| Spanner / Cloud Build / Workflows / Dataflow / Vertex / Firebase Auth / App Engine | Honest theatre or Identity Toolkit host; soft-skip Go SDK list/CLI coverage instead. |

When Compose publishes `127.0.0.1:4588` on a Windows host, run Terraform from
that host (not WSL loopback).
