# Secret Manager

**Status:** lab

REST and gRPC `google.cloud.secretmanager.v1.SecretManagerService` for secrets
and versions. Payloads are sealed with the process master key (`store.Seal`)
before SQLite persistence.

There is no official Go `*_EMULATOR_HOST` for Secret Manager; point clients with
`option.WithEndpoint` / custom endpoints (see below).

## Implemented

| Area | Surface |
|------|---------|
| Secrets | Create / Get / List / Delete / Patch (Update) (REST + gRPC) |
| Replication | Stored on create (`replication` JSON theatre; default automatic) |
| CMEK | `customerManagedEncryption.kmsKeyName` stored; seal still uses process master key |
| Rotation | `rotation.rotationPeriod` + `rotation.nextRotationTime` stored; optional `topics` |
| rotateSecret (lab) | Custom REST `:rotateSecret` creates a new version (not an official RPC) |
| Versions | AddVersion; Access including `latest`; List / Get |
| List filter | `?filter=state:ENABLED` (or DISABLED / DESTROYED) |
| State | Enable / Disable / Destroy (destroyed Access refused) |
| Per-secret IAM | getIamPolicy / setIamPolicy / testIamPermissions (REST + gRPC) |

REST paths (project-scoped):

- `POST /v1/projects/{project}/secrets?secretId=`
- `GET|PATCH|DELETE /v1/projects/{project}/secrets/{secret}`
- `POST /v1/projects/{project}/secrets/{secret}:addVersion`
- `POST /v1/projects/{project}/secrets/{secret}:rotateSecret`
- `POST .../secrets/{secret}:getIamPolicy|:setIamPolicy|:testIamPermissions`
- `GET /v1/projects/{project}/secrets/{secret}/versions?filter=state:ENABLED`
- `GET /v1/projects/{project}/secrets/{secret}/versions/{version}:access`
- `POST .../versions/{version}:enable|disable|destroy`

Create/patch accept official-shaped `rotation` (`nextRotationTime`,
`rotationPeriod` Duration like `86400s`) and optional `topics[{name}]`. The lab
stores these fields; it does not schedule timers or publish Pub/Sub rotation
notifications.

`:rotateSecret` is lab-only theatre (Cloud Secret Manager has no rotate RPC;
production rotation notifies via Pub/Sub and clients call `addVersion`). Body may
include `payload.data` (base64); when omitted, the latest enabled payload is
copied (or `"rotated"` if none). When both rotation fields are set,
`nextRotationTime` advances by `rotationPeriod`. Permission:
`secretmanager.versions.add`.

### Authz

`secretmanager.*` is evaluated on the secret resource
`projects/{project}/secrets/{id}` **or** the project
`projects/{projectId}` (OR).

## Emulator limits

- CMEK name is stored only; encryption always uses the lab master key
- No automatic rotation timers or Pub/Sub notification delivery
- `:rotateSecret` is lab theatre, not an official API method
- Destroy clears ciphertext and refuses Access
- Regional secrets under `projects/*/locations/*` are not modeled

## Pointing clients

```bash
# Go example: option.WithEndpoint("127.0.0.1:4588") + insecure credentials
# as required by your client version for cleartext h2c.
```

gcloud:

```bash
gcloud config set api_endpoint_overrides/secretmanager http://127.0.0.1:4588/
```

Terraform:

```hcl
provider "google" {
  secret_manager_custom_endpoint = "http://127.0.0.1:4588/v1/"
}
```

## Verification / CLI smoke

```bash
export TOKEN="$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN"
export EP=http://127.0.0.1:4588
export PROJECT=noctaxris-gcp-local

curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"replication":{"automatic":{}},"rotation":{"rotationPeriod":"86400s","nextRotationTime":"2099-01-01T00:00:00Z"},"topics":[{"name":"projects/p/topics/rot"}]}' \
  "$EP/v1/projects/$PROJECT/secrets?secretId=lab-secret"

curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"payload":{"data":"aGVsbG8="}}' \
  "$EP/v1/projects/$PROJECT/secrets/lab-secret:addVersion"

curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{}' \
  "$EP/v1/projects/$PROJECT/secrets/lab-secret:rotateSecret"

curl -sS -H "Authorization: Bearer $TOKEN" \
  "$EP/v1/projects/$PROJECT/secrets/lab-secret/versions/latest:access"
```

```bash
go test ./internal/services/secretmanager/ ./internal/services/kms/ -count=1
go test ./internal/store/ ./internal/server/ -run 'Secret|Rotation|Rotate' -count=1
```

## Deferred depth

- CMEK-backed seal using Cloud KMS lab keys
- Regional secret resources under `projects/*/locations/*`
- Pub/Sub rotation notification delivery
