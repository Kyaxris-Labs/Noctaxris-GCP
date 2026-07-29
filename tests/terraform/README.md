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
| `lab-armor` | `google_compute_security_policy` (Cloud Armor; SRC_IPS_V1 rules) | `compute_custom_endpoint` (`…/compute/v1/`); `add_terraform_attribution_label=false` (lab has no `setLabels`) |

Secrets stay in `lab-storage` (no separate `lab-secrets` stack). Auth uses
`GOOGLE_OAUTH_ACCESS_TOKEN` set from the root Bearer by `run.sh`.

```bash
# one stack
STACK=lab-armor bash tests/terraform/run.sh

# subset
STACKS="lab-storage lab-run lab-dns lab-compute lab-armor" bash tests/terraform/run.sh
```

## Honest skips

| Gap | Why not a stack |
|-----|-----------------|
| `google_dns_record_set` | Changes.create/get theatre exists; not yet wired into `lab-dns` (zone-only stack). No authoritative DNS / DNSSEC. |
| `google_compute_instance` | Images list/get/family theatre exists for ResolveImage; lab still has no disks/boot attach (metadata instances only). |
| `google_bigtable_*` | Instance Admin gRPC lite is present (Create/Get/List/Delete instance; Create returns a done Operation). Still no Table Admin gRPC, app profiles, cluster CRUD, or backups; provider table/app-profile resources will not apply end-to-end |
| `google_filestore_instance` | Provider BaseUrl is `https://file.googleapis.com/v1/`; lab mounts under `/file/v1/` (Spanner owns bare `/v1/.../instances`; Memorystore is location-scoped), so `filestore_custom_endpoint` must end in `/file/v1/`. Create returns completed Operation (`done: true` + `response`) theatre; Operations.get is immediate. |
| `google_sql_database_instance` | Provider targets `…/sql/v1beta4/` + Operations; lab mounts `/sql/v1` and `/sql/v1beta4` with DONE Operations theatre. |
| `google_redis_instance` | Create returns completed Operation (`done: true` + `response`); Operations.get is immediate done theatre. Fuller Memorystore settings (connect-mode / VPC / CMEK) still lite. |
| `google_managed_kafka_cluster` | Provider requires capacity + VPC subnet blocks; lab cluster CRUD is lite. |
| GKE / HTTP(S) LB / Cloud CDN | GKE lab path is `/container/v1/...` (Managed Kafka owns `/v1/.../clusters`); LB/CDN use lab GCS dataplane shapes, not standard NEG/backend-bucket stacks. |
| BigQuery / Spanner / Cloud Build / Workflows / Dataflow / Vertex / Firebase Auth / App Engine / KMS | Honest theatre or Identity Toolkit host; no dedicated stacks. Soft-skip Go SDK list/CLI coverage instead. |

When Compose publishes `127.0.0.1:4588` on a Windows host, run Terraform from
that host (not WSL loopback).
