#!/usr/bin/env bash
# Terraform apply + destroy against a running Noctaxris-GCP API.
# Soft-skips when terraform is missing, NOCTAXRIS_GCP_ENDPOINT is unset,
# or /_noctaxris-gcp/ready fails.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
STACK_NAME="${STACK:-lab-storage}"
STACK="$(cd "$(dirname "$0")/stacks/${STACK_NAME}" && pwd)"

EP="${NOCTAXRIS_GCP_ENDPOINT:-}"
if [[ -z "${EP}" ]]; then
  echo "NOCTAXRIS_GCP_ENDPOINT unset — skip Terraform suite" >&2
  exit 0
fi
EP="${EP%/}"

if ! command -v terraform >/dev/null 2>&1; then
  echo "terraform not on PATH — skip Terraform suite" >&2
  exit 0
fi

if ! curl -fsS "$EP/_noctaxris-gcp/ready" >/dev/null; then
  echo "Noctaxris-GCP not ready at $EP — skip Terraform suite" >&2
  exit 0
fi

TOKEN="${NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN:-}"
if [[ -z "${TOKEN}" ]]; then
  echo "NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN unset — skip Terraform suite" >&2
  exit 0
fi

PROJECT="${NOCTAXRIS_GCP_PROJECT:-noctaxris-gcp-local}"
PREFIX="tf$(date +%s)$(printf '%04d' $RANDOM)"
PREFIX="$(echo "$PREFIX" | tr '[:upper:]' '[:lower:]')"

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/noctaxris-gcp-tf.XXXXXX")"
cleanup() {
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

cp -a "$STACK/." "$WORKDIR/"
cd "$WORKDIR"

export GOOGLE_OAUTH_ACCESS_TOKEN="$TOKEN"
export CLOUDSDK_AUTH_ACCESS_TOKEN="$TOKEN"

export TF_PLUGIN_CACHE_DIR="${TF_PLUGIN_CACHE_DIR:-${HOME}/.terraform.d/plugin-cache}"
mkdir -p "$TF_PLUGIN_CACHE_DIR"

terraform init -input=false -no-color
terraform apply -input=false -auto-approve -no-color \
  -var="endpoint=$EP" \
  -var="project=$PROJECT" \
  -var="name_prefix=$PREFIX"
terraform destroy -input=false -auto-approve -no-color \
  -var="endpoint=$EP" \
  -var="project=$PROJECT" \
  -var="name_prefix=$PREFIX"

echo "Terraform STACK=$STACK_NAME ok against $EP"
