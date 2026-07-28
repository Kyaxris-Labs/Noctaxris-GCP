<p align="center">
  <b>Run GCP-shaped security labs on your laptop without a cloud bill or a host Docker socket.</b>
</p>

```bash
docker pull kyaxris/noctaxris-gcp:latest
# Container bind is 0.0.0.0; generate unique roots (shipped example pair is refused).
ROOT_SA="root@$(openssl rand -hex 4).iam.gserviceaccount.com"
ROOT_TOKEN="$(openssl rand -hex 32)"
docker run -d --name noctaxris-gcp -p 127.0.0.1:4588:4588 \
  -e NOCTAXRIS_GCP_LISTEN=0.0.0.0:4588 \
  -e NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN=1 \
  -e NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT="$ROOT_SA" \
  -e NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN="$ROOT_TOKEN" \
  kyaxris/noctaxris-gcp:latest
curl http://127.0.0.1:4588/_noctaxris-gcp/health
# ok
```

<p align="center">
  <a href="https://github.com/Kyaxris-Labs/Noctaxris-GCP/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/Kyaxris-Labs/Noctaxris-GCP/ci.yml?branch=main&label=CI" alt="CI"></a>
  <a href="https://hub.docker.com/r/kyaxris/noctaxris-gcp"><img src="https://img.shields.io/docker/pulls/kyaxris/noctaxris-gcp" alt="Docker pulls"></a>
  <a href="https://hub.docker.com/r/kyaxris/noctaxris-gcp/tags"><img src="https://img.shields.io/docker/v/kyaxris/noctaxris-gcp?sort=semver&label=image" alt="Docker image version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/Kyaxris-Labs/Noctaxris-GCP" alt="MIT License"></a>
</p>

Point GCP clients at `http://127.0.0.1:4588` with `Authorization: Bearer <token>`.

Go module: [`github.com/Kyaxris-Labs/Noctaxris-GCP`](https://github.com/Kyaxris-Labs/Noctaxris-GCP). Hub image: `kyaxris/noctaxris-gcp`.

## Why this exists

| | |
|---|---|
| Lab fidelity | Google IAM allow policies, service accounts, and Bearer auth on a single port |
| Secure defaults | Loopback publish only. No host `docker.sock`. Master key outside the data root |
| REST + gRPC | Cleartext HTTP/2 (h2c) multiplexes both on `:4588` |
| Compose simple | Single service image (no nested engine) |

## Quick start

```bash
docker pull kyaxris/noctaxris-gcp:latest

ROOT_SA="root@$(openssl rand -hex 4).iam.gserviceaccount.com"
ROOT_TOKEN="$(openssl rand -hex 32)"

docker run -d --name noctaxris-gcp -p 127.0.0.1:4588:4588 \
  -e NOCTAXRIS_GCP_LISTEN=0.0.0.0:4588 \
  -e NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN=1 \
  -e NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT="$ROOT_SA" \
  -e NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN="$ROOT_TOKEN" \
  kyaxris/noctaxris-gcp:latest

curl http://127.0.0.1:4588/_noctaxris-gcp/health
curl http://127.0.0.1:4588/_noctaxris-gcp/ready
curl -H "Authorization: Bearer $ROOT_TOKEN" \
  http://127.0.0.1:4588/v3/projects/noctaxris-gcp-local
```

Or Compose: copy `docker/.env.example` to `docker/.env`, replace both root values with unique credentials, then:

```bash
docker compose -f docker/compose.yaml --env-file docker/.env up --build
```

Default host publish is `127.0.0.1:4588` only. Default project id is `noctaxris-gcp-local`.

## Services

| Area | Services |
|------|----------|
| Identity | Cloud Resource Manager (orgs/folders), IAM, Service Usage |
| Crypto | Secret Manager, Cloud KMS |
| Data | Cloud Storage, Pub/Sub, Firestore, Datastore, Cloud Bigtable, Memorystore Redis |
| Audit/logs | Cloud Logging |
| Compute | Compute Engine (VPC/firewall), Cloud Run, Cloud Functions, Cloud Scheduler, Cloud Tasks, Cloud Build, App Engine |
| Registry | Artifact Registry |
| Networking | Cloud DNS |
| Analytics | BigQuery, Firebase Auth, Eventarc, Workflows, Cloud Spanner, Dataflow |
| Observability | Cloud Monitoring |

Per-service lab actions, emulator limits, and smoke notes: [docs/services/](docs/services/index.md).

## Security defaults

See [docs/security-defaults.md](docs/security-defaults.md). Highlights:

- Bearer required on API paths; health/version skip auth
- Example root pair from `.env.example` refused on non-loopback listen
- Root principal bypasses IAM evaluation (lab operator)
- All other principals deny by default

## Configuration

Environment prefix: `NOCTAXRIS_GCP_*`. Full list: [docs/configuration.md](docs/configuration.md).

## Architecture

[docs/architecture.md](docs/architecture.md)

## License

[MIT](LICENSE) (c) Kyaxris Labs
