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
| Secrets | Create / Get / List / Delete (REST + gRPC) |
| Versions | AddVersion; Access including `latest`; List / Get |
| State | Enable / Disable / Destroy (destroyed Access refused) |

REST paths (project-scoped):

- `POST /v1/projects/{project}/secrets?secretId=`
- `GET|DELETE /v1/projects/{project}/secrets/{secret}`
- `POST /v1/projects/{project}/secrets/{secret}:addVersion`
- `GET /v1/projects/{project}/secrets/{secret}/versions/{version}:access`
- `POST .../versions/{version}:enable|disable|destroy`

### Authz

`secretmanager.secrets.*` and `secretmanager.versions.*` are evaluated on
`projects/{projectId}`.

## Emulator limits

- No replication / regional secrets / CMEK customer keys (lab seal only)
- No rotation schedules or Pub/Sub notifications
- No IAM policies on individual secrets
- Destroy clears ciphertext and refuses Access

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
  secret_manager_custom_endpoint = "http://127.0.0.1:4588/"
}
```

## Verification / CLI smoke

```bash
export TOKEN="$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN"
export EP=http://127.0.0.1:4588
export PROJECT=noctaxris-gcp-local

curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{}' \
  "$EP/v1/projects/$PROJECT/secrets?secretId=lab-secret"

# payload.data is base64("hello")
curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"payload":{"data":"aGVsbG8="}}' \
  "$EP/v1/projects/$PROJECT/secrets/lab-secret:addVersion"

curl -sS -H "Authorization: Bearer $TOKEN" \
  "$EP/v1/projects/$PROJECT/secrets/lab-secret/versions/latest:access"
```

## Deferred depth

- Per-secret IAM and CMEK
- Automatic rotation
- Regional secret resources under `projects/*/locations/*`
