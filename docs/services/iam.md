# IAM

Lab-complete IAM Admin REST for service accounts and user-managed keys.
Project IAM policies live on Cloud Resource Manager; per-service-account IAM
policies are stored on the SA resource name.

## Lab actions

| Action | Method | Path |
|--------|--------|------|
| Create service account | `POST` | `/v1/projects/{project}/serviceAccounts` |
| List service accounts | `GET` | `/v1/projects/{project}/serviceAccounts` |
| Get service account | `GET` | `/v1/projects/{project}/serviceAccounts/{email}` |
| Patch service account | `PATCH` | `/v1/projects/{project}/serviceAccounts/{email}` |
| Delete service account | `DELETE` | `/v1/projects/{project}/serviceAccounts/{email}` |
| Undelete service account | `POST` | `/v1/projects/{project}/serviceAccounts/{email}:undelete` |
| Enable service account | `POST` | `/v1/projects/{project}/serviceAccounts/{email}:enable` |
| Disable service account | `POST` | `/v1/projects/{project}/serviceAccounts/{email}:disable` |
| Sign blob (lab) | `POST` | `/v1/projects/{project}/serviceAccounts/{email}:signBlob` |
| Sign JWT (lab) | `POST` | `/v1/projects/{project}/serviceAccounts/{email}:signJwt` |
| Get SA IAM policy | `POST` | `/v1/projects/{project}/serviceAccounts/{email}:getIamPolicy` |
| Set SA IAM policy | `POST` | `/v1/projects/{project}/serviceAccounts/{email}:setIamPolicy` |
| Test SA IAM permissions | `POST` | `/v1/projects/{project}/serviceAccounts/{email}:testIamPermissions` |
| Create key | `POST` | `/v1/projects/{project}/serviceAccounts/{email}/keys` |
| List keys | `GET` | `/v1/projects/{project}/serviceAccounts/{email}/keys` |
| Get key | `GET` | `/v1/projects/{project}/serviceAccounts/{email}/keys/{key}` |
| Delete key | `DELETE` | `/v1/projects/{project}/serviceAccounts/{email}/keys/{key}` |

Permissions: `iam.serviceAccounts.*`, `iam.serviceAccountKeys.*` on
`projects/{project}` (custom methods use the same project resource for authz).
SA IAM policy documents are keyed by
`projects/{project}/serviceAccounts/{email}`. Nested SA resources also inherit
project-level bindings in the lab evaluator.

Creating a key seals the credentials JSON at rest, returns `privateKeyData`
(base64) once, and registers a hashed access token so
`Authorization: Bearer <token>` authenticates as that service account. The lab
token is stored in the credentials JSON `private_key` field.

Patch accepts `displayName` via body `serviceAccount.displayName` (or top-level
`displayName`) with optional `updateMask=displayName`.

Delete is soft-delete (`deleted_at`); `:undelete` restores the account and its
keys. List/get skip soft-deleted rows.

`signBlob` accepts `bytesToSign` or `payload` (base64). The lab returns
`keyId=lab-sha256` and `signature` / `signedBlob` as the base64 SHA-256 digest
of the decoded bytes (not an RSA signature).

`signJwt` accepts IAM Credentials-shaped body `{"payload":"<json claims>"}`.
The lab returns `keyId=lab-none` and `signedJwt` as an unsigned JWT
(`alg=none`, empty signature segment). This is not real asymmetric signing.
If `exp` is present it must be an integer unix timestamp not in the past and
within 12 hours (same bound as the official Credentials API).

List keys accepts `pageSize` (default 100, max 100) and `pageToken` (integer
offset); responses may include `nextPageToken`.

Create service account fails with `FAILED_PRECONDITION` when
`iam.googleapis.com` is DISABLED for the project (Service Usage gate).

## Emulator limits

- No workload identity federation or service account impersonation.
- `signBlob` is SHA-256 theatre, not PKCS#1 / RSA signing.
- `signJwt` is unsigned lab JWT theatre (`alg=none`), not RSA/ES256.
- Soft-delete has no 30-day purge timer; rows remain until process data is wiped.
- Key material is a lab credentials JSON (not a PKCS#8 RSA PEM).
- gRPC `IAM` admin service is not registered yet; use REST.

## Verification / CLI smoke

```bash
go test ./internal/server/ -run IAM -count=1
gcloud config set api_endpoint_overrides/iam http://127.0.0.1:4588/
gcloud iam service-accounts create lab-runner \
  --display-name="Lab Runner" --project=noctaxris-gcp-local
gcloud iam service-accounts update lab-runner@noctaxris-gcp-local.iam.gserviceaccount.com \
  --display-name="Lab Runner Renamed"
gcloud iam service-accounts disable lab-runner@noctaxris-gcp-local.iam.gserviceaccount.com
gcloud iam service-accounts enable lab-runner@noctaxris-gcp-local.iam.gserviceaccount.com
gcloud iam service-accounts keys create /tmp/lab-runner.json \
  --iam-account=lab-runner@noctaxris-gcp-local.iam.gserviceaccount.com
```

## SDK note

Go client `cloud.google.com/go/iam/admin/apiv1` has no official emulator host
env var. Use `option.WithEndpoint("127.0.0.1:4588")` against the lab listener.
