# IAM

Lab-complete IAM Admin REST for service accounts and user-managed keys.
Project IAM policies live on Cloud Resource Manager, not this API.

## Lab actions

| Action | Method | Path |
|--------|--------|------|
| Create service account | `POST` | `/v1/projects/{project}/serviceAccounts` |
| List service accounts | `GET` | `/v1/projects/{project}/serviceAccounts` |
| Get service account | `GET` | `/v1/projects/{project}/serviceAccounts/{email}` |
| Delete service account | `DELETE` | `/v1/projects/{project}/serviceAccounts/{email}` |
| Create key | `POST` | `/v1/projects/{project}/serviceAccounts/{email}/keys` |
| List keys | `GET` | `/v1/projects/{project}/serviceAccounts/{email}/keys` |
| Get key | `GET` | `/v1/projects/{project}/serviceAccounts/{email}/keys/{key}` |
| Delete key | `DELETE` | `/v1/projects/{project}/serviceAccounts/{email}/keys/{key}` |

Permissions: `iam.serviceAccounts.*`, `iam.serviceAccountKeys.*` on
`projects/{project}`.

Creating a key seals the credentials JSON at rest, returns `privateKeyData`
(base64) once, and registers a hashed access token so
`Authorization: Bearer <token>` authenticates as that service account. The lab
token is stored in the credentials JSON `private_key` field.

## Emulator limits

- No workload identity federation, signJwt/signBlob, or SA IAM policies on the
  service account resource itself.
- No disable/enable/undelete service account methods.
- Key material is a lab credentials JSON (not a PKCS#8 RSA PEM).
- gRPC `IAM` admin service is not registered yet; use REST.

## gcloud smoke

```bash
gcloud config set api_endpoint_overrides/iam http://127.0.0.1:4588/
gcloud iam service-accounts create lab-runner \
  --display-name="Lab Runner" --project=noctaxris-gcp-local
gcloud iam service-accounts keys create /tmp/lab-runner.json \
  --iam-account=lab-runner@noctaxris-gcp-local.iam.gserviceaccount.com
```

## SDK note

Go client `cloud.google.com/go/iam/admin/apiv1` has no official emulator host
env var. Use `option.WithEndpoint("127.0.0.1:4588")` against the lab listener.
