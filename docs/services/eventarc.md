# Eventarc

Lab Eventarc Triggers and Channels REST for Pub/Sub and GCS finalize events with best-effort HTTP / Cloud Run delivery (one retry on failed deliver).

## Status

**lab** — trigger CRUD; channel stub; attribute filters including `values` maps; deliver on Pub/Sub publish and GCS object finalize with one retry.

## Wire protocol

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{project}/locations/{location}/triggers?triggerId=` |
| `GET` | `/v1/projects/{project}/locations/{location}/triggers` |
| `GET` | `/v1/projects/{project}/locations/{location}/triggers/{trigger}` |
| `DELETE` | `/v1/projects/{project}/locations/{location}/triggers/{trigger}` |
| `POST` | `/v1/projects/{project}/locations/{location}/channels?channelId=` |
| `GET` | `/v1/projects/{project}/locations/{location}/channels` |
| `GET`/`DELETE` | `/v1/projects/{project}/locations/{location}/channels/{channel}` |

### Supported event filters

| `type` value |
|--------------|
| `google.cloud.pubsub.topic.v1.messagePublished` |
| `google.cloud.storage.object.v1.finalized` |

Additional filters: `bucket` (GCS), any other attribute equality, and `values` maps (`{"attribute":"x","values":{"k":"v"}}`). Optional `channel` on the trigger references a channel resource. Optional `transport.pubsub.topic` scopes Pub/Sub triggers to one topic.

### Destinations

| Field | Behavior |
|-------|----------|
| `destination.httpEndpoint.uri` | Best-effort HTTP POST (CloudEvents-ish JSON) |
| `destination.cloudRunService` | Resolves Cloud Run service `uri` when present; otherwise posts to lab `:invoke` theatre path |

Delivery is fire-and-forget (3s timeout). On transport error or HTTP 5xx, the lab retries once.

Channels store `provider`, `pubsubTopic`, and `state` metadata only (no provider handshake).

## Authz

- `eventarc.triggers.create|get|list|delete`
- `eventarc.channels.create|get|list|delete`

## Client configuration

```bash
gcloud config set api_endpoint_overrides/eventarc http://127.0.0.1:4588/
```

## Emulator limits

- Triggers stay regional (`.../locations/{loc}/triggers`); project-scoped
  `/v1/projects/{p}/triggers` is Cloud Build on this shared mux. Regional create
  with Eventarc-shaped bodies (`eventFilters` / `destination` / …) stays Eventarc;
  Cloud Build-shaped bodies on the same path go to Cloud Build.
- HTTP destinations require the lab catcher / loopback `:4588` or
  `NOCTAXRIS_GCP_HTTP_EGRESS=1` + exact allowlist (see security-defaults)
- Channel provider handshake is not implemented
- Delivery is best-effort HTTP with one retry (no dead-letter)

## Deferred depth

- Audit / Eventarc Advanced / Workflows destinations
- Dead-letter, ordering, full CloudEvents binary mode

## Verification / CLI smoke

```bash
go test ./internal/server/ -run Eventarc -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/triggers?triggerId=lab" \
  -d '{"eventFilters":[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}],"destination":{"httpEndpoint":{"uri":"http://127.0.0.1:9/hook"}}}'
```
