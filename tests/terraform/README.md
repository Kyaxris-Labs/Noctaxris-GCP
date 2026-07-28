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
| `google_dns_record_set` | Provider uses `Changes.create`; lab has rrsets CRUD only (no Changes API) |
| `google_compute_instance` | Provider `ResolveImage` needs Images API; lab has no disks/images |
| `google_bigtable_*` | Provider uses gRPC `InstanceAdminClient`; lab Bigtable Admin is REST `/v2/` only (`bigtable_custom_endpoint` alone is not enough) |
| `google_certificate_manager_certificate` / `certificate_map` | Provider `certificate_manager_custom_endpoint` (BaseUrl `…/v1/`) can hit lab REST, but create returns the resource (with `name`); `CertificateManagerOperationWaitTime` treats `name` as an LRO and polls until timeout. Lab create is synchronous (no Operation). |
| `google_filestore_instance` | Provider knob is `filestore_custom_endpoint` (BaseUrl `https://file.googleapis.com/v1/`); lab paths need `…/file/v1/` after the host. Same LRO wait: create returns instance resource with `name`, provider polls as Operation. Lab create is synchronous under `/file/v1/`. |

When Compose publishes `127.0.0.1:4588` on a Windows host, run Terraform from
that host (not WSL loopback).
