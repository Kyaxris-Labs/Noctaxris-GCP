# Security defaults

Noctaxris-GCP fails closed. Defaults favor a loopback lab on a single laptop.

## Network

| Setting | Default | Notes |
|---------|---------|-------|
| Listen | `127.0.0.1:4588` | Non-loopback without TLS requires `NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN=1` |
| Compose publish | `127.0.0.1:4588` | Container bind is `0.0.0.0:4588` with the opt-in above |
| Host Docker socket | never mounted | No DinD engine in this product |

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
