# Release checklist

How to cut a public Noctaxris-GCP release (example: **0.5.0**). Docker Hub image: **`kyaxris/noctaxris-gcp`**.

## Secrets (GitHub Actions)

Set on the canonical repo [`Kyaxris-Labs/Noctaxris-GCP`](https://github.com/Kyaxris-Labs/Noctaxris-GCP) (Settings → Secrets and variables → Actions). Never commit credentials.

| Secret | Purpose |
|--------|---------|
| `DOCKERHUB_USERNAME` | Docker Hub account or org username that owns `kyaxris/noctaxris-gcp` |
| `DOCKERHUB_TOKEN` | Docker Hub [access token](https://docs.docker.com/docker-hub/access-tokens/) with push rights (not the account password) |

Forks should skip publish. Missing secrets must fail closed on the canonical repo when a publish workflow is enabled.

## Before the tag

1. Bump `VERSION` (plain text, e.g. `0.5.0`) and keep any embedded version default in sync.
2. Move CHANGELOG notes under `## [0.5.0]` (feature-oriented sections; no internal delivery labels).
3. Confirm PR CI is green (`unit`, `image`, `sbom`, `govulncheck`). Run nested Compose when the release touches DinD / engine overlays.
4. Confirm docs still describe loopback defaults and opt-in nested compute only.

## Cut the release

```bash
# On the commit you intend to ship (main tip after merge):
git tag -a v0.5.0 -m "Release Noctaxris-GCP 0.5.0"
git push origin v0.5.0
```

When a `v*` release workflow is configured on the canonical repo, it should build `docker/Dockerfile` and push:

| Tag | Meaning |
|-----|---------|
| `kyaxris/noctaxris-gcp:0.5.0` | Exact semver |
| `kyaxris/noctaxris-gcp:0.5` | Major.minor |
| `kyaxris/noctaxris-gcp:0` | Major |
| `kyaxris/noctaxris-gcp:latest` | Latest tagged release |
| `kyaxris/noctaxris-gcp:sha-<short>` | Git short SHA |

Then create the GitHub Release for `v0.5.0` (UI or `gh release create v0.5.0 --notes-file ...`) using the CHANGELOG `0.5.0` section.

Until Hub publish automation lands, operators can still build and push locally after CI is green:

```bash
docker build -f docker/Dockerfile --build-arg VERSION=0.5.0 -t kyaxris/noctaxris-gcp:0.5.0 .
docker push kyaxris/noctaxris-gcp:0.5.0
```

## Local image check

```bash
docker build -f docker/Dockerfile --build-arg VERSION=0.5.0 -t kyaxris/noctaxris-gcp:local .
# Run with unique roots (see README); then:
curl -sS http://127.0.0.1:4588/_noctaxris-gcp/version
# 0.5.0
```

## Related

- Ops / CI matrix: [ops.md](ops.md)
- Security posture: [security-defaults.md](security-defaults.md)
