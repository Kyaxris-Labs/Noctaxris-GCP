# Pub/Sub

**Status:** lab

gRPC `google.pubsub.v1.Publisher` and `Subscriber` on the shared h2c port.
Official Go clients that honor `PUBSUB_EMULATOR_HOST` can target the same
address as Noctaxris-GCP.

## Implemented

| Area | RPCs |
|------|------|
| Topics | CreateTopic, GetTopic, ListTopics, DeleteTopic, Publish |
| Subscriptions | CreateSubscription, GetSubscription, ListSubscriptions, DeleteSubscription |
| Consume | Pull, Acknowledge |
| StreamingPull | Returns `UNIMPLEMENTED` |

Publish fans out one stored copy per subscription of the topic. Pull leases
messages for the subscription ack deadline; Acknowledge deletes them.

### Authz

Permissions such as `pubsub.topics.*` and `pubsub.subscriptions.*` (including
`pubsub.subscriptions.consume` for Pull/Acknowledge) are evaluated on
`projects/{projectId}`.

gRPC Bearer auth is applied by the shared server interceptor. Handlers also
re-check IAM when a principal is present. If you register Pub/Sub outside the
default server wiring, supply a principal resolver (metadata `authorization`
Bearer) or attach an equivalent interceptor.

## Emulator limits

- No push subscriptions, ordering keys, exactly-once delivery, or schemas
- No snapshots, seek, or dead-letter policies
- StreamingPull is not implemented
- Message retention and backlog quotas are not enforced

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

With the emulator host set, use the Google Cloud client libraries or:

```bash
# After creating topic/subscription via SDK against PUBSUB_EMULATOR_HOST:
# publish a message, pull, then acknowledge.
go test ./internal/server/ -run TestPubSubPublishPull
```

## Deferred depth

- Push endpoints and message filters
- Ordering keys / exactly-once
- StreamingPull and snapshots
- REST mirror of `/v1/projects/*/topics` (optional; gRPC is the primary path)
