#!/usr/bin/env bash
# Nested DinD operator/CI smoke for Noctaxris-GCP.
# Requires Docker Compose and curl.
#
# Default compose.yaml starts the API + restricted noctaxris-gcp-engine.
# Optional privileged engine overlay (hosts where restricted DinD fails):
#   COMPOSE_EXTRA_FILES="-f docker/compose.engine-privileged.yaml" bash docker/smoke-nested.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE=(docker compose -f "$ROOT/docker/compose.yaml")
# shellcheck disable=SC2206
if [[ -n "${COMPOSE_EXTRA_FILES:-}" ]]; then
  # shellcheck disable=SC2206
  EXTRA=( ${COMPOSE_EXTRA_FILES} )
  COMPOSE+=("${EXTRA[@]}")
fi
COMPOSE+=(--env-file "$ROOT/docker/.env")
EP="${EP:-http://127.0.0.1:4588}"
READY_TIMEOUT_SEC="${READY_TIMEOUT_SEC:-240}"
KEEP_UP="${KEEP_UP:-0}"
PROJECT="${NOCTAXRIS_GCP_PROJECT:-noctaxris-gcp-local}"

cleanup() {
  if [[ "$KEEP_UP" == "1" ]]; then
    return 0
  fi
  "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

EXAMPLE_ROOT_SA="root@example.iam.gserviceaccount.com"
EXAMPLE_ROOT_TOKEN="noctaxris-gcp-example-root-token"

if [[ ! -f "$ROOT/docker/.env" ]]; then
  cp "$ROOT/docker/.env.example" "$ROOT/docker/.env"
fi

# shellcheck disable=SC1091
set -a
source <(tr -d '\r' < "$ROOT/docker/.env")
set +a

if [[ "${NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT:-}" == "$EXAMPLE_ROOT_SA" && \
      "${NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN:-}" == "$EXAMPLE_ROOT_TOKEN" ]]; then
  ROOT_SA="root-smoke-$(openssl rand -hex 4)@noctaxris-gcp-local.iam.gserviceaccount.com"
  ROOT_TOKEN="$(openssl rand -hex 32)"
  awk -v sa="$ROOT_SA" -v token="$ROOT_TOKEN" '
    /^NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT=/ { print "NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT=" sa; next }
    /^NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN=/ { print "NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN=" token; next }
    { print }
  ' "$ROOT/docker/.env" > "$ROOT/docker/.env.tmp"
  mv "$ROOT/docker/.env.tmp" "$ROOT/docker/.env"
  # shellcheck disable=SC1091
  set -a
  source <(tr -d '\r' < "$ROOT/docker/.env")
  set +a
fi

TOKEN="${NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN:?missing NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN}"
AUTH="Authorization: Bearer ${TOKEN}"
LOC="us-central1"
RID="smoke$(date +%s)"

echo "==> compose up (API + nested engine)"
"${COMPOSE[@]}" up --build -d

echo "==> wait for GET $EP/_noctaxris-gcp/ready (engine TLS dial included)"
deadline=$((SECONDS + READY_TIMEOUT_SEC))
until curl -fsS "$EP/_noctaxris-gcp/ready" 2>/dev/null | grep -q ready; do
  if (( SECONDS >= deadline )); then
    echo "timeout waiting for ready" >&2
    "${COMPOSE[@]}" ps >&2 || true
    "${COMPOSE[@]}" logs --no-color --tail=120 >&2 || true
    exit 1
  fi
  sleep 3
done
echo "ready"

echo "==> engine service healthy"
ENGINE_ID="$("${COMPOSE[@]}" ps -q noctaxris-gcp-engine)"
if [[ -z "$ENGINE_ID" ]]; then
  echo "noctaxris-gcp-engine not running" >&2
  exit 1
fi
docker inspect -f '{{.State.Health.Status}}' "$ENGINE_ID" | grep -qx healthy

echo "==> Memorystore Redis create (nested; Compose fail-closed)"
REDIS_BODY=$(curl -fsS -H "$AUTH" -H "Content-Type: application/json" \
  -X POST "$EP/v1/projects/${PROJECT}/locations/${LOC}/instances?instanceId=${RID}-redis" \
  -d '{"tier":"BASIC","memorySizeGb":1,"displayName":"smoke-nested"}')
echo "$REDIS_BODY" | grep -q '"done":true\|"name":'
REDIS_GET=$(curl -fsS -H "$AUTH" \
  "$EP/v1/projects/${PROJECT}/locations/${LOC}/instances/${RID}-redis")
echo "$REDIS_GET" | grep -q 'noctaxris-gcp-redis\|"state":"READY"'
# Nested host should be engine DNS name, not pure theatre suffix.
echo "$REDIS_GET" | grep -q "noctaxris-gcp-redis-${RID}-redis"

echo "==> Cloud Run service + nested :invoke (fail-closed Compose; no RESPONSE_BODY mock force)"
curl -fsS -H "$AUTH" -H "Content-Type: application/json" \
  -X POST "$EP/v2/projects/${PROJECT}/locations/${LOC}/services?serviceId=${RID}-run" \
  -d '{"template":{"containers":[{"image":"alpine:3.20"}]}}' \
  >/dev/null
INVOKE=$(curl -fsS -H "$AUTH" -H "Content-Type: application/json" \
  -X POST "$EP/v2/projects/${PROJECT}/locations/${LOC}/services/${RID}-run:invoke" \
  -d '{}')
echo "$INVOKE" | grep -q '"mode":"nested"'

echo "==> smoke-nested ok"
