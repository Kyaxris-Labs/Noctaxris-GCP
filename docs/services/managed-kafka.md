# Managed Service for Apache Kafka

Lab Managed Kafka REST for clusters. Without nested DinD, `bootstrapServers` is a
theatre string and `state` is `ACTIVE`. With `NOCTAXRIS_GCP_DOCKER_HOST` set, create
attempts a nested Redpanda broker on the engine network (no host Kafka port publish);
nested start failures soft-fail back to theatre bootstrap.

## Status

**lab** — location-scoped cluster CRUD; optional nested Redpanda per cluster.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/clusters?clusterId={id}` (Cluster body) |
| `GET` | `/v1/projects/{p}/locations/{loc}/clusters` |
| `GET` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}` |
| `DELETE` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}` |

## Authz

Checked on `projects/{project}`:

- `managedkafka.clusters.create|get|list|delete`

## Emulator limits

- No host publish of Kafka ports; nested brokers listen on the DinD lab bridge only
- No topics, ACLs, Connect, or Schema Registry APIs
- Create is synchronous (`ACTIVE`; no long-running Operation)
- Nested Redpanda image: `docker.redpanda.com/redpandadata/redpanda:v24.2.4` (allowlisted)

## Deferred depth

- Multi-broker clusters, rebalancing, and full GCP capacity/network config fidelity
- Client TLS / SASL and Private Service Connect shapes
- Seed `managedkafka.googleapis.com` in default Service Usage (CRUD does not gate on it today)

## Verification / CLI smoke

```bash
go test ./internal/services/managedkafka/ ./internal/store/ -run Kafka -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters?clusterId=lab-kafka" \
  -d '{"displayName":"Lab Kafka"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters/lab-kafka"
```

Nested broker smoke (opt-in `compose.engine.yaml`):

```bash
# After cluster create, bootstrapServers may show noctaxris-gcp-kafka-<id>:9092 on the engine network.
```
