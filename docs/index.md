# Noctaxris-GCP docs

Public reference for **Noctaxris-GCP** (module `github.com/Kyaxris-Labs/Noctaxris-GCP`). Product name is PascalCase `Noctaxris-GCP`.

Noctaxris-GCP is a Docker-first GCP-shaped emulator for cloud security labs. Lab cores ship loopback by default, no host `docker.sock`, master key outside the data root, Bearer auth with IAM allow-policy evaluation, and REST plus gRPC on one h2c port (`:4588`). Nested DinD for Cloud Run is opt-in via Compose. Remaining deferred depth is tracked on the per-service pages.

## Reference

| Doc | Topic |
|-----|--------|
| [services/index.md](services/index.md) | Per-service APIs, authz, CLI smoke, deferred depth |
| [architecture.md](architecture.md) | Deploy graph, packages, and request path |
| [configuration.md](configuration.md) | Env vars, data layout, Compose, client endpoints |
| [ops.md](ops.md) | Single-replica rule, backup/restore, upgrades, CI matrix, Hub images |
| [release.md](release.md) | Cut a semver release and Docker Hub tags |
| [security-defaults.md](security-defaults.md) | Host, crypto, and auth posture |
| [../tests/README.md](../tests/README.md) | SDK (Go/Node/Python) / Terraform integration suites |
| [../CHANGELOG.md](../CHANGELOG.md) | Lab-core release notes |

Quick start stays in the root [README](../README.md).
