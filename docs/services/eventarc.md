# Eventarc

Lab Eventarc Triggers and Channels REST for Pub/Sub and GCS finalize events with
best-effort HTTP / Cloud Run / Cloud Functions delivery (one retry on failed
HTTP deliver).

## Status

**lab** — trigger CRUD; channel stub; attribute filters including `values` maps;
deliver on Pub/Sub publish and GCS object finalize with one retry;
`cloudFunction` destination (resource name or service+region object).

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
| `destination.cloudFunction` | Resource name string, or `{"service"|"function","region"|"location"}` / `{"name":...}` object. Resolves function `uri` when present; delivers in-process to the Functions invoke theatre when the function exists (no Bearer required on that path). Otherwise posts to lab `:invoke` URI |
| `serviceAccount` | Trigger-level SA email persisted and echoed; preferred identity for HTTP delivery Bearer mint |

Cloud Functions v2 create with `eventTrigger` / `eventarcTrigger` auto-inserts a
trigger whose destination is that function (see
[cloud-functions.md](cloud-functions.md)).

Delivery is fire-and-forget (3s timeout for HTTP). On transport error or HTTP
5xx, the lab retries once. In-process Cloud Functions delivery does not use HTTP.

For HTTP / Cloud Run `:invoke` delivery, the lab mints a registered Bearer
(`access_tokens`, same registration as IAM `generateAccessToken`) using, in
order: trigger `serviceAccount`, `destination.cloudRunService.serviceAccount`,
or `{project}-compute@developer.gserviceaccount.com` (auto-ensured). Fail closed
(skip deliver + log) when targeting lab `:invoke` with no resolvable SA.

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
  `NOCTAXRIS_GCP_HTTP_EGRESS=1` + exact allowlist (see security-defaults);
  non-allowlisted URIs are rejected at create (fail-closed)
- Lab catcher destinations are recorded in-process (no outbound HTTP); dump with
  `GET /_noctaxris-gcp/http-catcher` (`{"deliveries":[…]}`)
- Channel provider handshake is not implemented
- HTTP delivery is best-effort with one retry on transport error or HTTP 5xx;
  no dead-letter queue
- HTTP / Cloud Run `:invoke` delivery mints a lab Bearer (registered hash, not
  Google-signed OIDC); grant `roles/run.invoker` for the delivery SA. In-process
  `cloudFunction` delivery does not require Bearer

## Deferred depth

- Audit / Eventarc Advanced / Workflows destinations
- Dead-letter, ordering, full CloudEvents binary mode
- Real Google-signed OIDC JWTs

## Verification / CLI smoke

```bash
go test ./internal/services/eventarc/ ./internal/services/cloudfunctions/ ./internal/server/ -run 'Eventarc|HTTPCatcher|CloudFunctionsCreateWires' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/triggers?triggerId=lab" \
  -d '{"eventFilters":[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}],"destination":{"httpEndpoint":{"uri":"http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/eventarc-smoke"}}}'
curl -s http://127.0.0.1:4588/_noctaxris-gcp/http-catcher
# Or destination.cloudFunction after creating a function (see cloud-functions.md)
```
