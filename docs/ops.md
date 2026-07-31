# Operations

Durable single-host lab ops for Noctaxris-GCP. This is not a multi-tenant HA guide.

## Single API replica

Run **one** Noctaxris-GCP API process against a given data root (Compose named volume or host path). Do not scale replicas against the same `state.db`. Multi-instance access is unsupported and can corrupt SQLite state.

Compose mounts API sealed state (`noctaxris-gcp-data`) and the master key (`noctaxris-gcp-secrets` at `/var/lib/noctaxris-gcp-secrets`) as separate volumes. `noctaxris-gcp-init` chowns both to UID `65532` before the API starts. Default Compose sets `NOCTAXRIS_GCP_MASTER_KEY_FILE=/var/lib/noctaxris-gcp-secrets/master.key` so create works under `read_only: true`. Default Compose also starts restricted DinD (`noctaxris-gcp-engine`) and sets `NOCTAXRIS_GCP_DOCKER_HOST` plus fail-closed nested envs. The engine never mounts `noctaxris-gcp-data` or `noctaxris-gcp-secrets`. Nested SQL / Kafka / Redis containers run on the engine-internal `noctaxris-gcp-lab` bridge (not a Compose network; do not confuse with the `noctaxris-gcp-data` volume). Broker and DB ports stay unpublished on the operator host. If nested smoke fails on your host: `docker compose -f docker/compose.yaml -f docker/compose.engine-privileged.yaml --env-file docker/.env up --build` and keep host publish loopback. Nested proof script: `bash docker/smoke-nested.sh`.

## Listen and example roots

Process listen is loopback only for `localhost`, `127.0.0.0/8`, and `::1`. Port-only (`:4588`), `0.0.0.0`, and `::` are non-loopback and require TLS or `NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN=1` (Compose sets the opt-in for the in-container `0.0.0.0` bind; host publish stays `127.0.0.1:4588`).

The shipped `docker/.env.example` root pair (`root@example.iam.gserviceaccount.com` / `noctaxris-gcp-example-root-token`) is allowed on loopback listen only. Startup refuses that pair when listen is non-loopback, including default Compose. Copy `.env.example` to `.env` and replace both root values with unique lab credentials before `compose up`.

## Master key

Default `NOCTAXRIS_GCP_MASTER_KEY_FILE` is outside `NOCTAXRIS_GCP_DATA_ROOT` (sibling `…/noctaxris-gcp-secrets/master.key` when the data root is `…/noctaxris-gcp`). Compose pins that path on the `noctaxris-gcp-secrets` volume. Startup refuses a master key under the data root unless `NOCTAXRIS_GCP_ALLOW_MASTER_KEY_IN_DATA_ROOT=1`. Without `master.key`, sealed columns cannot be decrypted even if `state.db` is restored.

## Backup and restore

1. Stop Compose so writers are idle:

```bash
docker compose -f docker/compose.yaml --env-file docker/.env down
```

2. Archive data and secrets volumes (or the host data root plus master key path). Minimum set: `master.key` (Compose: `noctaxris-gcp-secrets`), `state.db`, object trees under the data root, and `audit.jsonl` when present.

```bash
docker run --rm -v noctaxris-gcp-data:/data -v "$PWD:/backup" busybox \
  tar czf /backup/noctaxris-gcp-data.tgz -C /data .
docker run --rm -v noctaxris-gcp-secrets:/data -v "$PWD:/backup" busybox \
  tar czf /backup/noctaxris-gcp-secrets.tgz -C /data .
```

Compose project prefixes may rename volumes (for example `docker_noctaxris-gcp-data`). Use `docker volume ls` and substitute the real names.

3. Restore into empty volumes or a fresh host path, confirm the files exist, then start Compose.

## Published images

Docker Hub image: **`kyaxris/noctaxris-gcp`** (canonical GitHub repo `Kyaxris-Labs/Noctaxris-GCP`).

| Tags | Source |
|------|--------|
| `1.x.y`, `1.x`, `1`, `latest`, `sha-<short>` | Tag push `v*` → [`.github/workflows/release.yml`](../.github/workflows/release.yml) (after `ci-required.yml` gates) |
| `nightly`, `nightly-YYYYMMDD`, `sha-<short>` | Nightly cron / dispatch → [`.github/workflows/docker-nightly.yml`](../.github/workflows/docker-nightly.yml) |
| Local / CI | `docker build -f docker/Dockerfile .` on every PR |

Repository secrets for Hub publish (never commit): `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`. Product version: file `VERSION`, OCI label when set at build, and open probe `GET /_noctaxris-gcp/version`. See [release.md](release.md).

## Image upgrades

1. Stop Compose.
2. Take a backup (above).
3. Pull a Hub tag (`docker pull kyaxris/noctaxris-gcp:1.0.1`) or rebuild (`docker compose ... up --build`).
4. Start Compose and confirm `/_noctaxris-gcp/ready` returns ready (optional: `/_noctaxris-gcp/version`).

Schema changes are additive (`CREATE TABLE IF NOT EXISTS`, `ALTER TABLE ... ADD COLUMN` with duplicate-column ignore). There is no down-migration. Prefer stop → backup → start over live multi-writer upgrades.

## Graceful shutdown

On `SIGTERM` or interrupt, the API drains HTTP with a short shutdown timeout. Prefer `docker compose ... stop` or `down` over `kill -9`. Backup still requires writers idle (Compose down) so SQLite and volume archives are consistent.

## Health vs ready

| Probe | Path | Meaning |
|-------|------|---------|
| Liveness | `GET /_noctaxris-gcp/health` | Process accepts HTTP (`ok`) |
| Readiness | `GET /_noctaxris-gcp/ready` | SQLite ping OK; if `NOCTAXRIS_GCP_DOCKER_HOST` is set, nested engine ping OK; body `ready` |

Compose `healthcheck` calls `/noctaxris-gcp healthcheck` (distroless, no curl) against readiness over the container's plain HTTP listener. Optional TLS (`NOCTAXRIS_GCP_TLS_CERT` / `NOCTAXRIS_GCP_TLS_KEY`) does not automatically switch Compose probes or client `http://` endpoint URLs; keep healthchecks and lab clients on HTTP unless you rewire both.

## CI matrix

GitHub Actions (`.github/workflows/ci.yml`):

| Job | When |
|-----|------|
| unit | Every push and PR (`go test ./...`; also `-race`) |
| image | `docker build -f docker/Dockerfile .` |
| sbom | After image: Syft SPDX SBOM artifact |
| govulncheck | `go run ./scripts/govulncheck-ci` (allowlist only for documented residuals with no module-path fix) |

Release / Hub publish (separate workflows):

| Workflow | When |
|----------|------|
| `ci-required.yml` | Called by `release.yml`: unit, compose-static, govulncheck, scoped race, image, smoke-core (Compose up + CRM/GCS/Secret Manager + audit hygiene). Failures block Hub push |
| `release.yml` | Tag push `v*` / dispatch: run `ci-required`, then push semver + `latest` (+ sha). Canonical repo + Hub secrets required |
| `docker-nightly.yml` | UTC cron + `workflow_dispatch`: push `nightly` (+ dated / sha). Does not move `latest` or semver |

A green PR proves unit tests, compose-static, scoped race, image build, SBOM, govulncheck, and smoke-core (API + nested engine ready). Weekly / dispatch nested smoke (`docker/smoke-nested.sh`) proves Memorystore nested create and Cloud Run nested `:invoke`. Nested compute is **on** in default Compose.

Operator integration suites (path-filtered on `tests/**` or dispatch): start Compose, then `bash tests/run-all.sh`. That script **hard-fails** if `/_noctaxris-gcp/ready` fails or the root token is missing. Individual SDK/TF tests still **soft-skip** when `NOCTAXRIS_GCP_ENDPOINT` is unset. Nested-oriented SDK rows: `NOCTAXRIS_GCP_NESTED=1 bash tests/run-all.sh`. Details: [tests/README.md](../tests/README.md).

Per-service CLI smoke remains documented on each `docs/services/` page for operator runs outside CI.

## Compose overlays (lab opt-in)

Default `docker/compose.yaml` publishes loopback and starts restricted DinD with fail-closed nested envs. Overlays are for privileged workaround and host-gateway labs only.

| Overlay | When |
|---------|------|
| `docker/compose.engine.yaml` | Compatibility shim (engine already in base). Older docs may still pass `-f compose.engine.yaml`. |
| `docker/compose.engine-privileged.yaml` | Nested `docker info` / create fails on restricted DinD (Desktop/WSL edge cases). Privileged DinD is a host workaround, not the secure default. Keep publish on `127.0.0.1:4588` |
| `docker/compose.lab-host-gateway.yaml` | Sets `NOCTAXRIS_GCP_INJECT_HOST_GATEWAY=1`. Cloud Run one-shot uses `NetworkMode: none` (limited benefit). Optional `NOCTAXRIS_GCP_PUBLISH_ADDR=0.0.0.0` for Desktop host-gateway reachability (lab-only). |
| `docker/smoke-nested.sh` | Operator/CI nested proof (engine healthy, Memorystore nested host, Cloud Run nested invoke) |

HTTP egress helpers (`NOCTAXRIS_GCP_HTTP_EGRESS`, `NOCTAXRIS_GCP_HTTP_ALLOWLIST`) stay off unless set on the API process. See [configuration.md](configuration.md).

## Related

- Security posture: [security-defaults.md](security-defaults.md)
- Release cut: [release.md](release.md)
- Architecture: [architecture.md](architecture.md)
