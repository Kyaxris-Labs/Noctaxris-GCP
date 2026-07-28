#!/usr/bin/env bash
# Thin wrapper for CI / Unix operators. Prefer: go run ./scripts/govulncheck-ci
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
exec go run ./scripts/govulncheck-ci
