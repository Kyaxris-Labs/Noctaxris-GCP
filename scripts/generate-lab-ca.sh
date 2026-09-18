#!/usr/bin/env bash
set -euo pipefail
out="${1:-./lab-ca}"
exec go run ./scripts/generatelabca "$out"
