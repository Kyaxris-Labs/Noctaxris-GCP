# Security defaults

Noctaxris-GCP fails closed. Defaults favor a loopback lab on a single laptop.

## Network

| Setting | Default | Notes |
|---------|---------|-------|
| Listen | `127.0.0.1:4588` | Non-loopback without TLS requires `NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN=1` |
| Compose publish | `127.0.0.1:4588` | Container bind is `0.0.0.0:4588` with the opt-in above |
| Host Docker socket | never mounted | Nested DinD is opt-in only via `docker/compose.engine.yaml` |

## Nested engine (opt-in)

- Empty `NOCTAXRIS_GCP_DOCKER_HOST` disables nested compute. Unit tests and default
  Compose stay green without Docker / DinD.
- Never mount host `/var/run/docker.sock` on the API service. Runtime rejects
  `unix://`, `npipe://`, and any host string containing `docker.sock`.
- Opt-in overlay `docker/compose.engine.yaml` starts `noctaxris-gcp-engine`
  (digest-pinned `docker:27-dind`) as restricted DinD (`privileged: false` +
  caps / devices / `cgroup: host` / writable `/sys/fs/cgroup`) and sets
  `NOCTAXRIS_GCP_DOCKER_HOST=tcp://noctaxris-gcp-engine:2376` plus
  `NOCTAXRIS_GCP_DOCKER_CERT_PATH=/certs/client`. The engine API is not published
  to the host.
- Non-default engine URLs require `NOCTAXRIS_GCP_DOCKER_HOST_ALLOWLIST`. TLS
  client PEMs are required whenever Docker host is set.
- Image pulls fail closed: pinned lab bases (`alpine:3.20`, …) only, unless
  extended with `NOCTAXRIS_GCP_IMAGE_PULL_ALLOWLIST` (digest required for registry
  hosts).
- If nested containers fail on Desktop/WSL2, add `compose.engine-privileged.yaml`
  (`privileged: true`). Keep host publish on `127.0.0.1:4588`.

## Authentication

- API requests require `Authorization: Bearer <token>`.
- Root token comes from `NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN` and maps to `NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT`.
- Other tokens are SHA-256 hashed and looked up in `access_tokens` (minted when IAM creates a service account key).
- Missing or invalid credentials return Google JSON `UNAUTHENTICATED` (HTTP 401).
- Public paths: `/_noctaxris-gcp/health`, `/_noctaxris-gcp/ready`, `/_noctaxris-gcp/version`.

## Example root refusal

The pair shipped in `docker/.env.example` is refused when listen is non-loopback
(Compose container bind). Generate unique roots before `compose up`.

## Authorization

- IAM allow policies are stored per resource in SQLite.
- Deny by default.
- The authenticated root principal bypasses IAM evaluation. This matches lab
  operator convenience in the AWS-shaped sibling product and is intentional.
  Documented here so CTF authors do not treat root as a normal service account.
- Non-root evaluation uses role bindings (`roles/owner` grants all permissions
  for lab depth). `testIamPermissions` returns only granted permissions.

## Secrets at rest

- Master key file defaults to a sibling path outside the data root
  (`…/noctaxris-gcp-secrets/master.key`).
- ChaCha20-Poly1305 seals SA private keys, secret payloads, and KMS key material.
- Compose mounts a dedicated secrets volume and runs the API with `read_only: true`.

## Container

- Distroless `nonroot` (UID 65532).
- No `EXPOSE` of anything except the documented API port in the image metadata.
- Healthcheck uses the binary (`noctaxris-gcp healthcheck`), not curl.

## Vulnerability scan (govulncheck)

- CI installs `govulncheck` at a pinned module version (not `@latest`) and runs
  `go run ./scripts/govulncheck-ci`, which fails on any symbol-reachable finding
  whose OSV ID is not listed in `scripts/govulncheck-allowlist.txt`.
- Prefer toolchain and dependency upgrades over allowlisting. The allowlist is
  only for residuals with no module-path fix (documented when an ID is added).
- Adding `github.com/docker/docker` (Engine client SDK) surfaces Docker Engine
  CVEs with Fixed in: N/A on that module path (`GO-2026-4883`, `GO-2026-4887`,
  `GO-2026-5617`, `GO-2026-5668`). CI allowlists those IDs in
  `scripts/govulncheck-allowlist.txt` so new app vulns still fail the job.
  Noctaxris-GCP does not call `CopyToContainer` / `CopyFromContainer`; nested
  one-shots use create/start/wait/logs only. AuthZ-plugin bypass findings do not
  apply to the packaged engine path (no AuthZ plugins). Residual is still the
  nested engine binary version and who can talk to it over TLS on the Compose
  network. Re-run `go run ./scripts/govulncheck-ci` after bumps; add only new
  Fixed N/A IDs that appear locally.
- Go toolchain tracks a current patch (see `go.mod`); API image build stage uses
  a digest-pinned `golang` bookworm base matching that version.
