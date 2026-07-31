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
| `lab-dns` | Cloud DNS managed zone | `dns_custom_endpoint` (`…/dns/v1/`) |
| `lab-compute` | Compute VPC network (metadata theatre; no VMs) | `compute_custom_endpoint` (`…/compute/v1/`) |
| `lab-armor` | `google_compute_security_policy` (Cloud Armor; SRC_IPS_V1 rules) | `compute_custom_endpoint` (`…/compute/v1/`); lab `setLabels` DONE Operation |
| `lab-kms` | KMS key ring + crypto key | `kms_custom_endpoint` (`…/v1/`) |
| `lab-bigquery` | BigQuery dataset + table | `big_query_custom_endpoint` (`…/bigquery/v2/`) |
| `lab-iam` | Service account | `iam_custom_endpoint` listener root (`…/`); provider `~> 5.45` (REST; google >=6 uses IAM gRPC) |
| `lab-sql` | Cloud SQL Postgres instance (nested when Compose engine healthy) | `sql_custom_endpoint` (`…/sql/v1beta4/`) |
| `lab-redis` | Memorystore Redis instance (nested when Compose engine healthy; delete returns done Operation) | `redis_custom_endpoint` (`…/v1/`) |

Secrets stay in `lab-storage` (no separate `lab-secrets` stack). Auth uses
`GOOGLE_OAUTH_ACCESS_TOKEN` set from the root Bearer by `run.sh`.

Default Compose starts nested DinD; `lab-sql` / `lab-redis` expect fail-closed
nested create when the engine is up.

```bash
# one stack
STACK=lab-armor bash tests/terraform/run.sh

# subset
STACKS="lab-storage lab-kms lab-sql" bash tests/terraform/run.sh
```

## Honest skips

| Gap | Why not a stack |
|-----|-----------------|
| `google_dns_record_set` | Changes.create/get theatre exists; not yet wired into `lab-dns` (zone-only stack). No authoritative DNS / DNSSEC. |
| `google_compute_instance` | Images list/get/family theatre exists for ResolveImage; lab still has no disks/boot attach (metadata instances only). |
| `google_bigtable_*` | Instance Admin gRPC lite is present; still no Table Admin gRPC / row mutate. |
| `google_filestore_instance` | Provider BaseUrl is `https://file.googleapis.com/v1/`; lab mounts under `/file/v1/`. |
| `google_managed_kafka_cluster` | Provider requires capacity + VPC subnet blocks; lab cluster CRUD is lite. Keep as opt-in experiment, not default. |
| GKE / HTTP(S) LB / Cloud CDN | Lab path shapes differ from standard NEG/backend-bucket stacks. |
| Spanner / Cloud Build / Workflows / Dataflow / Vertex / Firebase Auth / App Engine | Honest theatre or Identity Toolkit host; soft-skip Go SDK list/CLI coverage instead. |

When Compose publishes `127.0.0.1:4588` on a Windows host, run Terraform from
that host (not WSL loopback).
