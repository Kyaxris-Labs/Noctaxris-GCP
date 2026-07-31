# Integration tests

Real SDK and Terraform suites against a running Noctaxris-GCP API. Soft-skip when the endpoint or root token is unset.

Endpoint default: `http://127.0.0.1:4588`. Use the same root Bearer as `docker/.env`.

## Prerequisites

1. Compose up and ready (Compose binds `0.0.0.0` in-container; the shipped `.env.example` root pair is refused):

```bash
cp docker/.env.example docker/.env
ROOT_SA="root@$(openssl rand -hex 4).iam.gserviceaccount.com"
ROOT_TOKEN="$(openssl rand -hex 32)"
awk -v sa="$ROOT_SA" -v token="$ROOT_TOKEN" '
  /^NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT=/ { print "NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT=" sa; next }
  /^NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN=/ { print "NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN=" token; next }
  { print }
' docker/.env > docker/.env.tmp && mv docker/.env.tmp docker/.env

docker compose -f docker/compose.yaml --env-file docker/.env up --build -d

curl -fsS http://127.0.0.1:4588/_noctaxris-gcp/health
curl -fsS http://127.0.0.1:4588/_noctaxris-gcp/ready
```

Or copy `.env.example` and replace both `NOCTAXRIS_GCP_ROOT_*` values yourself before `compose up`.

2. Export credentials (match `docker/.env`):

```bash
set -a && source docker/.env && set +a
export NOCTAXRIS_GCP_ENDPOINT=http://127.0.0.1:4588
export NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
export NOCTAXRIS_GCP_PROJECT="${NOCTAXRIS_GCP_PROJECT:-noctaxris-gcp-local}"
```

Optional overrides: `NOCTAXRIS_GCP_ENDPOINT`, `NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN`, `NOCTAXRIS_GCP_PROJECT`.

| Suite | Tools |
|-------|--------|
| SDK (Go) | Go 1.22+; feature `*_test.go` + `helpers_test.go` under `tests/sdk/go` |
| SDK (Node.js) | Node.js 24+; `*.test.mjs` + `helpers.mjs` under `tests/sdk/nodejs` (`npm test`) |
| SDK (Python) | Python 3.10+; `test_*.py` + `conftest.py` under `tests/sdk/python` |
| Terraform | Terraform CLI 1.5+, Google provider resolved on `init` |

## Soft-skip vs hard-fail

| Case | Behavior |
|------|----------|
| `NOCTAXRIS_GCP_ENDPOINT` unset inside an SDK/TF test | Soft-skip that test |
| API not ready when running `tests/run-all.sh` | Hard-fail (exit 1) |
| Root token unset when running `tests/run-all.sh` | Hard-fail (exit 1) |
| Nested Cloud Run / DinD rows without healthy engine | Soft-skip inside SDK tests |

Set `NOCTAXRIS_GCP_NESTED=1` to keep nested-oriented SDK rows enabled (still soft-skip without a healthy engine). Default Compose starts the nested engine; bare binary without `NOCTAXRIS_GCP_DOCKER_HOST` stays mock/theatre.

## Run all suites

From the repo root (bash / WSL / Git Bash):

```bash
bash tests/run-all.sh

# Nested / DinD-oriented SDK rows (soft-skip without engine)
NOCTAXRIS_GCP_NESTED=1 bash tests/run-all.sh
```

Or run each suite:

```bash
# SDK round-trips (multi-file feature suites; soft-skip rows when API down)
cd tests/sdk/go && go test ./... -count=1 -timeout 10m

cd tests/sdk/nodejs && npm install && npm test

cd tests/sdk/python && pip install -r requirements.txt && pytest

# Terraform apply + destroy (default set: storage, run, dns, compute, armor, kms, bigquery, iam, sql, redis)
bash tests/terraform/run.sh

# Compute VM stack (parity; not in default STACKS)
STACK=lab-compute-instance bash tests/terraform/run.sh

# Managed Kafka stack (parity / isolated; not in default STACKS until Compose nested fail-closed is proven)
STACK=lab-kafka bash tests/terraform/run.sh

# LB Armor attach stack (parity; not in default STACKS)
STACK=lab-lb-armor bash tests/terraform/run.sh

# Opt-in parity Terraform loop (lab-compute-instance, lab-lb-armor, lab-kafka)
TF_GCP_PARITY=1 bash tests/run-all.sh
```

When Compose publishes `127.0.0.1:4588` on a Windows host, run Terraform from that host (not WSL loopback).

Tag/release `ci-required` is unit + compose-static + smoke-core + govulncheck. Full SDK/TF proof needs green `integration-suites` or a manual `bash tests/run-all.sh` (optionally `NOCTAXRIS_GCP_NESTED=1`, `TF_GCP_PARITY=1`).

Maintainer gap list: [HANDOFF.md](HANDOFF.md). Stack notes: [terraform/README.md](terraform/README.md).
