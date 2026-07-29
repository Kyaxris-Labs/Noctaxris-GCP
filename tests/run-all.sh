#!/usr/bin/env bash
# Run SDK and Terraform integration suites against Noctaxris-GCP.
#
# Soft-skip: individual SDK/TF tests skip when endpoint/token/ready fail.
# Hard-fail: this script exits non-zero if the API is not ready or root token
# is missing (suites cannot run at all).
#
# Optional:
#   NOCTAXRIS_GCP_NESTED=1  — keep nested/DinD SDK rows enabled (tests soft-skip
#                             without compose.engine.yaml; default suites stay
#                             mock-invoke friendly when unset)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export NOCTAXRIS_GCP_ENDPOINT="${NOCTAXRIS_GCP_ENDPOINT:-http://127.0.0.1:4588}"
export NOCTAXRIS_GCP_PROJECT="${NOCTAXRIS_GCP_PROJECT:-noctaxris-gcp-local}"

EP="$NOCTAXRIS_GCP_ENDPOINT"
if ! curl -fsS "$EP/_noctaxris-gcp/ready" >/dev/null; then
  echo "Noctaxris-GCP not ready at $EP — start Compose first (see tests/README.md)" >&2
  echo "Hard-fail: API readiness required for run-all (not a soft-skip)." >&2
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
  echo "Hard-fail: root token required for run-all (not a soft-skip)." >&2
  exit 1
fi

SDK_TIMEOUT="10m"
if [[ "${NOCTAXRIS_GCP_NESTED:-}" == "1" ]]; then
  SDK_TIMEOUT="20m"
  echo "==> Nested opt-in (NOCTAXRIS_GCP_NESTED=1); SDK timeout ${SDK_TIMEOUT}"
fi

echo "==> SDK (Go) [NESTED=${NOCTAXRIS_GCP_NESTED:-0}]"
(cd tests/sdk/go && go test ./... -count=1 -timeout "$SDK_TIMEOUT")

echo "==> SDK (Node.js)"
(cd tests/sdk/nodejs && npm install --no-fund --no-audit && npm test)

echo "==> SDK (Python)"
(cd tests/sdk/python && python -m pip install -q -r requirements.txt && python -m pytest)

echo "==> Terraform (default stacks)"
bash tests/terraform/run.sh

echo "All suites finished."
