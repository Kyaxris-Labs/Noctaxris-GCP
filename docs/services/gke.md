# Google Kubernetes Engine (GKE)

Lab Container API v1 cluster CRUD. Without a nested engine, clusters return
`RUNNING` with a theatre `endpoint`. With `NOCTAXRIS_GCP_DOCKER_HOST` set, create
may run a pinned k3s one-shot (no host port publish; not a long-lived control plane).

## Status

**lab** — `projects.locations.clusters` create/get/list/delete; optional nested k3s proof.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

Routes stay under `/container/v1/...` (not bare `/v1/.../clusters`) so they do not
collide with Managed Kafka cluster paths on the shared ServeMux.

| Method | Path |
|--------|------|
| `POST` | `/container/v1/projects/{p}/locations/{loc}/clusters?clusterId={id}` |
| `GET` | `/container/v1/projects/{p}/locations/{loc}/clusters` |
| `GET` | `/container/v1/projects/{p}/locations/{loc}/clusters/{cluster}` |
| `DELETE` | `/container/v1/projects/{p}/locations/{loc}/clusters/{cluster}` |

## Authz

Checked on `projects/{project}`:

- `container.clusters.create|get|list|delete`

Seeded Service Usage: `container.googleapis.com`.

## Nested k3s limits

- Image: `rancher/k3s:v1.28.8-k3s1` (pinned in image allowlist)
- One-shot only (`NetworkMode: none`); no published apiserver port on the host
- Create budgets ~2s for the one-shot (large image pulls soft-fail); cluster row still `RUNNING`
- Failure soft-fails to theatre metadata (`noctaxrisNestedEngine` on the cluster JSON)

## Deferred depth

- Node pools, workloads, authenticating to a real apiserver
- Long-running k3s clusters and kubeconfig issuance

## Verification / CLI smoke

```bash
go test ./internal/services/gke/ ./internal/server/ -run 'GKE|GKEEdge' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/container/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters?clusterId=lab-gke" \
  -d '{"displayName":"Lab GKE"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/container/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters"
```
