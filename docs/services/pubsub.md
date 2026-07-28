# Pub/Sub

**Status:** lab

gRPC `google.pubsub.v1.Publisher` / `Subscriber` and a REST mirror under
`/v1/projects/{project}/topics|subscriptions` on the shared port. Official Go
clients that honor `PUBSUB_EMULATOR_HOST` can target the same address;
Terraform typically uses the REST surface.

## Implemented

| Area | Surface |
|------|---------|
| Topics | Create / Get / List / Delete / Update (gRPC + REST); Publish |
| Subscriptions | Create / Get / List / Delete / Update (gRPC + REST); Pull; Acknowledge; ModifyAckDeadline |
| StreamingPull | Lab-minimal: delivers current backlog once, then ends the stream |
| Push | If `pushConfig.pushEndpoint` is set, best-effort HTTP POST on publish (2xx acks that copy) |

REST paths (colon actions live inside path wildcards):

- `PUT|GET|PATCH|DELETE /v1/projects/{p}/topics/{topic}`
- `POST /v1/projects/{p}/topics/{topic}:publish`
- `PUT|GET|PATCH|DELETE /v1/projects/{p}/subscriptions/{sub}`
- `POST .../subscriptions/{sub}:pull|:acknowledge|:modifyAckDeadline`

Publish fans out one stored copy per subscription. Pull leases messages for the
subscription ack deadline; Acknowledge deletes them. Push delivery is best-effort
and does not block publish success when the endpoint is unreachable.

### Authz

Permissions such as `pubsub.topics.*` and `pubsub.subscriptions.*` (including
`pubsub.subscriptions.consume` for Pull / Acknowledge / ModifyAckDeadline /
StreamingPull) are evaluated on `projects/{projectId}`.

gRPC Bearer auth is applied by the shared server interceptor. Handlers also
re-check IAM when a principal is present.

## Emulator limits

- No ordering keys, exactly-once delivery, or schemas
- No snapshots, seek, or dead-letter policies
- StreamingPull does not stay long-lived beyond one backlog flush
- Message retention and backlog quotas are not enforced
- Push has no OIDC / auth header injection

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
  pubsub_custom_endpoint = "http://127.0.0.1:4588/"
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
  -d "{\"topic\":\"projects/$PROJECT/topics/lab-topic\"}" \
  "$EP/v1/projects/$PROJECT/subscriptions/lab-sub"

curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"messages":[{"data":"aGVsbG8="}]}' \
  "$EP/v1/projects/$PROJECT/topics/lab-topic:publish"

curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"maxMessages":10}' \
  "$EP/v1/projects/$PROJECT/subscriptions/lab-sub:pull"
```

Also: `go test ./internal/server/ -run 'TestPubSub'`

## Deferred depth

- Ordering keys / exactly-once / schemas
- Snapshots, seek, and dead-letter policies
- Long-lived StreamingPull with continuous ack/modify loops
- Authenticated push (OIDC) and message filters
