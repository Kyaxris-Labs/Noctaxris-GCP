# Pub/Sub

**Status:** lab

gRPC `google.pubsub.v1.Publisher` / `Subscriber` and a REST mirror under
`/v1/projects/{project}/topics|subscriptions|snapshots` on the shared port.
Official Go clients that honor `PUBSUB_EMULATOR_HOST` can target the same
address; Terraform typically uses the REST surface.

## Implemented

| Area | Surface |
|------|---------|
| Topics | Create / Get / List / Delete / Update (gRPC + REST); Publish |
| Subscriptions | Create / Get / List / Delete / Update (gRPC + REST); Pull; Acknowledge; ModifyAckDeadline |
| Snapshots | Create / Get / List / Delete (gRPC + REST); metadata only |
| Dead letter | `deadLetterPolicy.deadLetterTopic` + `maxDeliveryAttempts` (5–100); after max pulls without ack, message is published to the DL topic and removed |
| Exactly-once | `enableExactlyOnceDelivery` stored and returned (theatre flag; no ordering/EOS lease semantics beyond storage) |
| Filters | Attribute equality: `attributes.key = "value"` (AND-combined terms); non-matching messages are not delivered |
| Seek | Seek to time (gRPC + REST `:seek`); clears ack state for later messages, deletes earlier backlog |
| StreamingPull | Long-lived loop: recv acks/modacks, send messages until client cancels |
| Push | If `pushConfig.pushEndpoint` is set, best-effort HTTP POST on publish (2xx acks that copy) |
| Push OIDC | `pushConfig.oidcToken` (`serviceAccountEmail`, `audience`) stored and returned; push sets `Authorization: Bearer` with unsigned lab JWT (`alg=none`) |
| Push update | REST `PATCH` and `:modifyPushConfig` (including OIDC fields) |

REST paths (colon actions live inside path wildcards):

- `PUT|GET|PATCH|DELETE /v1/projects/{p}/topics/{topic}`
- `POST /v1/projects/{p}/topics/{topic}:publish`
- `PUT|GET|PATCH|DELETE /v1/projects/{p}/subscriptions/{sub}`
- `POST .../subscriptions/{sub}:pull|:acknowledge|:modifyAckDeadline|:modifyPushConfig|:seek`
- `PUT|GET|DELETE /v1/projects/{p}/snapshots/{snap}`
- `GET /v1/projects/{p}/snapshots`

Create snapshot body: `{"subscription":"projects/.../subscriptions/...","labels":{...}}`.
Response includes `name`, `topic` (from the subscription), `expireTime` (lab: create+7d), and `labels`.

Publish fans out one stored copy per matching subscription. Pull leases messages for the
subscription ack deadline; Acknowledge deletes them. Push delivery is best-effort
and does not block publish success when the endpoint is unreachable.

When `pushConfig.oidcToken.serviceAccountEmail` is set, push requests include
`Authorization: Bearer <lab JWT>`. The lab JWT is unsigned theatre (`alg=none`,
empty signature segment) with `aud` = audience (or the push endpoint when audience
is empty), and `email` / `sub` = the service account email. This is not Google-signed
OIDC. Unlike Cloud Scheduler, Pub/Sub returns `oidcToken` on get (API-shaped config).
Lab catcher deliveries also record the `authorization` header value on the catcher
JSON for tests.

### Authz

Permissions such as `pubsub.topics.*`, `pubsub.subscriptions.*`, and
`pubsub.snapshots.*` (including `pubsub.subscriptions.consume` for Pull /
Acknowledge / ModifyAckDeadline / StreamingPull / Seek) are evaluated on
`projects/{projectId}`.

gRPC Bearer auth is applied by the shared server interceptor. Handlers also
re-check IAM when a principal is present.

## Emulator limits

- No ordering keys or schemas; exactly-once is a stored flag only (no EOS ack semantics)
- Snapshots are metadata-only (no backlog retention); seek-to-snapshot returns invalid argument
- Filter language is attribute equality only (no HAS, OR, NOT)
- Message retention and backlog quotas are not enforced
- Push OIDC uses unsigned lab JWT theatre (`alg=none`), not real Google-signed tokens
- Dead-letter publishes on pull attempt count only (no separate deliveryAttempt metric API)

## Pointing clients

```bash
export PUBSUB_EMULATOR_HOST=127.0.0.1:4588
export GOOGLE_CLOUD_PROJECT=noctaxris-gcp-local
```

gcloud:

```bash
gcloud config set api_endpoint_overrides/pubsub http://127.0.0.1:4588/
```

Terraform:

```hcl
provider "google" {
  pubsub_custom_endpoint = "http://127.0.0.1:4588/v1/"
}
```

## Verification / CLI smoke

```bash
export TOKEN="$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN"
export EP=http://127.0.0.1:4588
export PROJECT=noctaxris-gcp-local

curl -sS -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{}' "$EP/v1/projects/$PROJECT/topics/lab-topic"

curl -sS -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{}' "$EP/v1/projects/$PROJECT/topics/lab-dlq"

curl -sS -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"topic\":\"projects/$PROJECT/topics/lab-topic\",\"filter\":\"attributes.region = \\\"us\\\"\",\"deadLetterPolicy\":{\"deadLetterTopic\":\"projects/$PROJECT/topics/lab-dlq\",\"maxDeliveryAttempts\":5},\"enableExactlyOnceDelivery\":true}" \
  "$EP/v1/projects/$PROJECT/subscriptions/lab-sub"

curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"messages":[{"data":"aGVsbG8=","attributes":{"region":"us"}}]}' \
  "$EP/v1/projects/$PROJECT/topics/lab-topic:publish"

curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"maxMessages":10}' \
  "$EP/v1/projects/$PROJECT/subscriptions/lab-sub:pull"

curl -sS -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"subscription\":\"projects/$PROJECT/subscriptions/lab-sub\"}" \
  "$EP/v1/projects/$PROJECT/snapshots/lab-snap"

curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"pushConfig":{"pushEndpoint":"http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/push","oidcToken":{"serviceAccountEmail":"push-sa@noctaxris-gcp-local.iam.gserviceaccount.com","audience":"https://example.com"}}}' \
  "$EP/v1/projects/$PROJECT/subscriptions/lab-sub:modifyPushConfig"
```

Also: `go test ./internal/store/ ./internal/server/ -run 'PubSub|DeadLetter|OIDC' -count=1`
(DLQ redelivery needs repeated pull + `modifyAckDeadline` 0 or expired lease; see store test.)

## Deferred depth

- Ordering keys / full exactly-once ack semantics / schemas
- Snapshot backlog retention and seek-to-snapshot
- Full filter language (OR / NOT / HAS)
- Real Google-signed push OIDC (lab uses `alg=none` theatre)
