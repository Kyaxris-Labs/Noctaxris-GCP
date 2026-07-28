#!/usr/bin/env bash
# Terraform apply + destroy against a running Noctaxris-GCP API.
# Soft-skips when terraform is missing, NOCTAXRIS_GCP_ENDPOINT is unset,
# or /_noctaxris-gcp/ready fails.
#
# Default: all stacks under stacks/. Override with STACK=lab-storage or
# STACKS="lab-storage lab-run lab-dns lab-compute".
set -euo pipefail

TF_DIR="$(cd "$(dirname "$0")" && pwd)"

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

if [[ -n "${STACK:-}" ]]; then
  STACKS=("$STACK")
elif [[ -n "${STACKS:-}" ]]; then
  # shellcheck disable=SC2206
  STACKS=($STACKS)
else
  STACKS=(lab-storage lab-run lab-dns lab-compute)
fi

PROJECT="${NOCTAXRIS_GCP_PROJECT:-noctaxris-gcp-local}"
PREFIX="tf$(date +%s)$(printf '%04d' $RANDOM)"
PREFIX="$(echo "$PREFIX" | tr '[:upper:]' '[:lower:]')"

export GOOGLE_OAUTH_ACCESS_TOKEN="$TOKEN"
export CLOUDSDK_AUTH_ACCESS_TOKEN="$TOKEN"
export TF_PLUGIN_CACHE_DIR="${TF_PLUGIN_CACHE_DIR:-${HOME}/.terraform.d/plugin-cache}"
mkdir -p "$TF_PLUGIN_CACHE_DIR"

run_stack() {
  local STACK_NAME="$1"
  local STACK="$TF_DIR/stacks/${STACK_NAME}"
  if [[ ! -d "$STACK" ]]; then
    echo "unknown Terraform stack: $STACK_NAME" >&2
    exit 1
  fi

  (
    set -euo pipefail
    WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/noctaxris-gcp-tf.XXXXXX")"
    trap 'rm -rf "$WORKDIR"' EXIT
    cp -a "$STACK/." "$WORKDIR/"
    cd "$WORKDIR"

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
  )
}

for STACK_NAME in "${STACKS[@]}"; do
  run_stack "$STACK_NAME"
done
