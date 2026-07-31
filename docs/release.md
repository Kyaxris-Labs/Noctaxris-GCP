# Release checklist

How to cut a public Noctaxris-GCP release (example: **1.0.1**). Docker Hub image: **`kyaxris/noctaxris-gcp`**.

## Secrets (GitHub Actions)

Set on the canonical repo [`Kyaxris-Labs/Noctaxris-GCP`](https://github.com/Kyaxris-Labs/Noctaxris-GCP) (Settings → Secrets and variables → Actions). Never commit credentials.

| Secret | Purpose |
|--------|---------|
| `DOCKERHUB_USERNAME` | Docker Hub account or org username that owns `kyaxris/noctaxris-gcp` |
| `DOCKERHUB_TOKEN` | Docker Hub [access token](https://docs.docker.com/docker-hub/access-tokens/) with push rights (not the account password) |

Forks skip publish with a log line. Missing secrets fail closed on the canonical repo for release and nightly publish.

## Before the tag

1. Bump `VERSION` (plain text, e.g. `1.0.1`) and keep `internal/version/version.go` (and Dockerfile `ARG VERSION`) in sync.
2. Move CHANGELOG notes under `## [1.0.1]` (feature-oriented sections; no internal delivery labels).
3. Confirm PR CI is green (`unit`, `compose-static`, `race`, `image`, `sbom`, `govulncheck`, `smoke-core`). Release also runs required gates (`ci-required.yml`). Run `bash docker/smoke-nested.sh` when the release touches DinD / nested paths.
4. Confirm docs still describe loopback defaults and default-on nested DinD (privileged and host-gateway overlays stay opt-in).

## Cut the release

```bash
# On the commit you intend to ship (main tip after merge):
git tag -a v1.0.1 -m "Release Noctaxris-GCP 1.0.1"
git push origin v1.0.1
```

Pushing tag `v*` runs [`.github/workflows/release.yml`](../.github/workflows/release.yml). That workflow first runs the required CI gates ([`.github/workflows/ci-required.yml`](../.github/workflows/ci-required.yml)) against the tagged commit. Docker Hub push runs only when those gates succeed. Then it builds `docker/Dockerfile` and pushes:

| Tag | Notes |
|-----|--------|
| `kyaxris/noctaxris-gcp:1.0.1` | Exact semver |
| `kyaxris/noctaxris-gcp:1.0` | Minor track |
| `kyaxris/noctaxris-gcp:1` | Major track |
| `kyaxris/noctaxris-gcp:latest` | Latest tagged release |
| `kyaxris/noctaxris-gcp:sha-<short>` | Immutable build id |

Then create the GitHub Release for `v1.0.1` (UI or `gh release create v1.0.1 --notes-file ...`) using the CHANGELOG `1.0.1` section.

Optional: Actions → **release** → Run workflow with an existing tag if you need to re-push Hub tags after fixing secrets.

## Nightly

[`.github/workflows/docker-nightly.yml`](../.github/workflows/docker-nightly.yml) runs on a UTC cron and `workflow_dispatch`. It pushes `nightly`, `nightly-YYYYMMDD`, and `sha-<short>` only. It does **not** move `latest` or semver tags.

## Local image check

```bash
docker build -f docker/Dockerfile --build-arg VERSION=1.0.1 -t kyaxris/noctaxris-gcp:local .
# Run with unique roots (see README); then:
curl -sS http://127.0.0.1:4588/_noctaxris-gcp/version
# 1.0.1
```

## Related

- Ops / CI matrix: [ops.md](ops.md)
- Security posture: [security-defaults.md](security-defaults.md)
