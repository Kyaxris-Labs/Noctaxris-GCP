# Eventarc

Lab Eventarc Triggers REST for Pub/Sub and GCS finalize events with best-effort HTTP / Cloud Run delivery.

## Status

**lab** — trigger CRUD; deliver on Pub/Sub publish and GCS object finalize.

## Wire protocol

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{project}/locations/{location}/triggers?triggerId=` |
| `GET` | `/v1/projects/{project}/locations/{location}/triggers` |
| `GET` | `/v1/projects/{project}/locations/{location}/triggers/{trigger}` |
| `DELETE` | `/v1/projects/{project}/locations/{location}/triggers/{trigger}` |

### Supported event filters

| `type` value |
|--------------|
| `google.cloud.pubsub.topic.v1.messagePublished` |
| `google.cloud.storage.object.v1.finalized` |

Optional filters: `bucket` (GCS). Optional `transport.pubsub.topic` scopes Pub/Sub triggers to one topic.

### Destinations

| Field | Behavior |
|-------|----------|
| `destination.httpEndpoint.uri` | Best-effort HTTP POST (CloudEvents-ish JSON) |
| `destination.cloudRunService` | Resolves Cloud Run service `uri` when present; otherwise posts to lab `:invoke` theatre path |

Delivery is fire-and-forget (3s timeout, no retries).

## Authz

- `eventarc.triggers.create|get|list|delete`

## Client configuration

```bash
gcloud config set api_endpoint_overrides/eventarc http://127.0.0.1:4588/
```

## Deferred depth

- Audit / Eventarc Advanced / Workflows destinations
- Channel / provider APIs
- Retry policies, dead-letter, ordering
- Full CloudEvents binary content mode

## Verification / CLI smoke

```bash
go test ./internal/server/ -run Eventarc -count=1
```
