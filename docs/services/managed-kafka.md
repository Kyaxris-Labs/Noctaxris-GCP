# Managed Service for Apache Kafka

Lab Managed Kafka REST for clusters, topics, and ACL metadata. Without nested DinD,
`bootstrapServers` is a theatre string and `state` is `ACTIVE`. With
`NOCTAXRIS_GCP_DOCKER_HOST` set, create attempts a nested Redpanda broker on the
shared `noctaxris-gcp-lab` bridge (no host Kafka port publish); nested start
failures soft-fail back to theatre bootstrap unless
`NOCTAXRIS_GCP_NESTED_ENGINE_FAIL_CLOSED` is `1`/`true` (create returns
`FAILED_PRECONDITION` and the cluster row is rolled back).

Topic create persists in SQLite. When the cluster has a nested `container_id`,
create best-effort runs `rpk topic create` inside the Redpanda container and
soft-fails if the engine is off or exec fails. ACLs are metadata-only theatre
(not applied to the broker).

## Status

**lab** — location-scoped cluster/topic CRUD; ACL metadata theatre; optional nested Redpanda per cluster.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/clusters?clusterId={id}` (Cluster body) |
| `GET` | `/v1/projects/{p}/locations/{loc}/clusters` |
| `GET` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}` |
| `DELETE` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}` |
| `POST` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}/topics?topicId={id}` (Topic body) |
| `GET` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}/topics` |
| `GET` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}/topics/{topic}` |
| `DELETE` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}/topics/{topic}` |
| `POST` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}/acls?aclId={id}` (Acl body) |
| `GET` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}/acls` |
| `GET` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}/acls/{aclId}` |
| `DELETE` | `/v1/projects/{p}/locations/{loc}/clusters/{cluster}/acls/{aclId}` |

Topic JSON fields: `name`, `partitionCount`, `replicationFactor`, `configs`.
ACL `aclId` follows the Managed Kafka Resource Pattern shapes (`cluster`,
`topic/{name}`, `allTopics`, prefixed forms, and so on). Response includes
derived `resourceType` / `resourceName` / `patternType` plus `aclEntries` and
`etag`.

## Authz

Checked on `projects/{project}`:

- `managedkafka.clusters.create|get|list|delete`
- `managedkafka.topics.create|get|list|delete`
- `managedkafka.acls.create|get|list|delete`

Seeded Service Usage: `managedkafka.googleapis.com`. Create cluster/topic/ACL
refuses with `FAILED_PRECONDITION` when that API is DISABLED.

## Emulator limits

- No host publish of Kafka ports; nested brokers listen on `noctaxris-gcp-lab` (shared with SQL/Redis)
- ACLs are SQLite metadata only (not pushed to Redpanda/Kafka authorizer)
- No Connect or Schema Registry APIs
- Create is synchronous (`ACTIVE`; no long-running Operation)
- Nested Redpanda image: `docker.redpanda.com/redpandadata/redpanda:v24.2.4` (allowlisted)
- Nested cluster start soft-fails to theatre by default; opt-in `NOCTAXRIS_GCP_NESTED_ENGINE_FAIL_CLOSED` hard-errors create
- Nested topic create via `rpk` soft-fails without rolling back the SQLite topic row

## Deferred depth

- Multi-broker clusters, rebalancing, and full GCP capacity/network config fidelity
- Client TLS / SASL and Private Service Connect shapes
- Broker-applied ACLs, Connect, and Schema Registry

## Verification / CLI smoke

```bash
go test ./internal/services/managedkafka/ ./internal/store/ -count=1 -run 'Kafka|ManagedKafka|Topic'
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters?clusterId=lab-kafka" \
  -d '{"displayName":"Lab Kafka"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters/lab-kafka/topics?topicId=orders" \
  -d '{"partitionCount":3,"replicationFactor":1}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters/lab-kafka/topics/orders"
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters/lab-kafka/acls?aclId=topic/orders" \
  -d '{"aclEntries":[{"principal":"User:*","permissionType":"ALLOW","operation":"READ","host":"*"}]}'
```

Nested broker smoke (opt-in `compose.engine.yaml`):

```bash
# After cluster create, bootstrapServers may show noctaxris-gcp-kafka-<id>:9092 on the engine network.
# Topic create with container_id set best-effort runs: rpk topic create <id> -p N -r M
```
