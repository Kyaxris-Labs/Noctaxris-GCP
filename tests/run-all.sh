#!/usr/bin/env bash
# Run SDK and Terraform integration suites against Noctaxris-GCP.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export NOCTAXRIS_GCP_ENDPOINT="${NOCTAXRIS_GCP_ENDPOINT:-http://127.0.0.1:4588}"
export NOCTAXRIS_GCP_PROJECT="${NOCTAXRIS_GCP_PROJECT:-noctaxris-gcp-local}"

EP="$NOCTAXRIS_GCP_ENDPOINT"
if ! curl -fsS "$EP/_noctaxris-gcp/ready" >/dev/null; then
  echo "Noctaxris-GCP not ready at $EP — start Compose first (see tests/README.md)" >&2
  exit 1
fi

if [[ -z "${NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN:-}" ]]; then
  if [[ -f docker/.env ]]; then
    # shellcheck disable=SC1091
    set -a && source docker/.env && set +a
  fi
fi
if [[ -z "${NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN:-}" ]]; then
  echo "NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN unset — export it or source docker/.env" >&2
  exit 1
fi

echo "==> SDK (Go)"
(cd tests/sdk/go && go test ./... -count=1 -timeout 10m)

echo "==> SDK (Node.js)"
(cd tests/sdk/nodejs && npm install --no-fund --no-audit && npm test)

echo "==> SDK (Python)"
(cd tests/sdk/python && python -m pip install -q -r requirements.txt && python -m pytest)

echo "==> Terraform (default stacks)"
bash tests/terraform/run.sh

echo "All suites finished."
