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
| Versions | AddVersion; Access including `latest`; List / Get |
| List filter | `?filter=state:ENABLED` (or DISABLED / DESTROYED) |
| State | Enable / Disable / Destroy (destroyed Access refused) |
| Per-secret IAM | getIamPolicy / setIamPolicy / testIamPermissions (REST + gRPC) |

REST paths (project-scoped):

- `POST /v1/projects/{project}/secrets?secretId=`
- `GET|PATCH|DELETE /v1/projects/{project}/secrets/{secret}`
- `POST /v1/projects/{project}/secrets/{secret}:addVersion`
- `POST .../secrets/{secret}:getIamPolicy|:setIamPolicy|:testIamPermissions`
- `GET /v1/projects/{project}/secrets/{secret}/versions?filter=state:ENABLED`
- `GET /v1/projects/{project}/secrets/{secret}/versions/{version}:access`
- `POST .../versions/{version}:enable|disable|destroy`

### Authz

`secretmanager.*` is evaluated on the secret resource
`projects/{project}/secrets/{id}` **or** the project
`projects/{projectId}` (OR).

## Emulator limits

- CMEK name is stored only; encryption always uses the lab master key
- No rotation schedules or Pub/Sub notifications
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
  -d '{"replication":{"automatic":{}},"customerManagedEncryption":{"kmsKeyName":"projects/p/locations/global/keyRings/r/cryptoKeys/k"}}' \
  "$EP/v1/projects/$PROJECT/secrets?secretId=lab-secret"

curl -sS -X PATCH -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"labels":{"env":"lab"}}' \
  "$EP/v1/projects/$PROJECT/secrets/lab-secret"

# payload.data is base64("hello")
curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"payload":{"data":"aGVsbG8="}}' \
  "$EP/v1/projects/$PROJECT/secrets/lab-secret:addVersion"

curl -sS -H "Authorization: Bearer $TOKEN" \
  "$EP/v1/projects/$PROJECT/secrets/lab-secret/versions/latest:access"

curl -sS -H "Authorization: Bearer $TOKEN" \
  "$EP/v1/projects/$PROJECT/secrets/lab-secret/versions?filter=state:ENABLED"
```

## Deferred depth

- CMEK-backed seal using Cloud KMS lab keys
- Regional secret resources under `projects/*/locations/*`
- Automatic rotation and Pub/Sub rotation notifications
