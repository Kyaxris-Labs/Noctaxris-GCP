# IAM

Lab-complete IAM Admin REST for service accounts and user-managed keys, plus
Workload Identity Federation pool/provider CRUD, STS token-exchange theatre, and
service account `generateAccessToken` impersonation theatre. Project IAM
policies live on Cloud Resource Manager; per-service-account IAM policies are
stored on the SA resource name.

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
| Generate access token (lab) | `POST` | `/v1/projects/{project\|-}/serviceAccounts/{email}:generateAccessToken` |
| Get SA IAM policy | `POST` | `/v1/projects/{project}/serviceAccounts/{email}:getIamPolicy` |
| Set SA IAM policy | `POST` | `/v1/projects/{project}/serviceAccounts/{email}:setIamPolicy` |
| Test SA IAM permissions | `POST` | `/v1/projects/{project}/serviceAccounts/{email}:testIamPermissions` |
| Create key | `POST` | `/v1/projects/{project}/serviceAccounts/{email}/keys` |
| List keys | `GET` | `/v1/projects/{project}/serviceAccounts/{email}/keys` |
| Get key | `GET` | `/v1/projects/{project}/serviceAccounts/{email}/keys/{key}` |
| Delete key | `DELETE` | `/v1/projects/{project}/serviceAccounts/{email}/keys/{key}` |
| Create WIF pool | `POST` | `/v1/projects/{project}/locations/{location}/workloadIdentityPools?workloadIdentityPoolId=` |
| List WIF pools | `GET` | `/v1/projects/{project}/locations/{location}/workloadIdentityPools` |
| Get WIF pool | `GET` | `/v1/projects/{project}/locations/{location}/workloadIdentityPools/{pool}` |
| Delete WIF pool | `DELETE` | `/v1/projects/{project}/locations/{location}/workloadIdentityPools/{pool}` |
| Create WIF provider | `POST` | `.../workloadIdentityPools/{pool}/providers?workloadIdentityPoolProviderId=` |
| List WIF providers | `GET` | `.../workloadIdentityPools/{pool}/providers` |
| Get WIF provider | `GET` | `.../workloadIdentityPools/{pool}/providers/{provider}` |
| Delete WIF provider | `DELETE` | `.../workloadIdentityPools/{pool}/providers/{provider}` |
| STS token exchange (lab) | `POST` | `/v1/token` |

Permissions: `iam.serviceAccounts.*`, `iam.serviceAccountKeys.*`,
`iam.workloadIdentityPools.*`, `iam.workloadIdentityPoolProviders.*` on
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

`generateAccessToken` matches IAM Credentials shape: required `scope[]`,
optional `lifetime` (Duration ending in `s`, max 12h, default 1h), optional
`delegates` (accepted, not enforced). Returns `accessToken` + `expireTime` and
registers the token so Bearer auth becomes the target SA. Project may be `-`
(Credentials-style) or a concrete project id.

Authz uses `EvaluateAny` for `iam.serviceAccounts.getAccessToken` on the SA
resource **or** the parent project. Bind
`roles/iam.serviceAccountTokenCreator` on the target SA (or grant
`getAccessToken` via `roles/owner` / an explicit project binding). Basic
`roles/viewer` and `roles/editor` do **not** grant token impersonation. Root
still bypasses.

### Workload Identity Federation + STS

Pool/provider CRUD stores display name, description, disabled flag, OIDC
`issuerUri`, and `attributeMapping` JSON. Soft-delete sets `state=DELETED`.

`POST /v1/token` is a public STS theatre endpoint (no Bearer required; the
`subject_token` authenticates the external identity). Required fields:

| Field | Notes |
|-------|--------|
| `grant_type` | Must be `urn:ietf:params:oauth:grant-type:token-exchange` |
| `audience` | WIF provider resource name; optional `//iam.googleapis.com/` prefix |
| `subject_token` | Lab accepts any non-empty string (no OIDC signature verify) |

Optional: `subject_token_type` (accepted, not validated). JSON camelCase and
form `snake_case` field names are both accepted.

On success the lab returns `access_token`, `token_type=Bearer`, `expires_in=3600`,
and registers the token as principal `wif:{providerId}:{subject}` where
`subject` is a sanitized form of `subject_token` (alnum/`-`/`_`/`.`; others
become `-`; max 64 chars). Unknown, deleted, or disabled pools/providers fail
closed (`UNAUTHENTICATED`). Bind that principal on CRM/IAM policies using the
literal member string `wif:{providerId}:{subject}` (the evaluator does not
rewrite it to `serviceAccount:`).

There is no OIDC discovery document and no real IdP signature verification.

List keys accepts `pageSize` (default 100, max 100) and `pageToken` (integer
offset); responses may include `nextPageToken`.

Create service account fails with `FAILED_PRECONDITION` when
`iam.googleapis.com` is DISABLED for the project (Service Usage gate).

## Emulator limits

- STS does not verify OIDC JWTs or call external IdPs; `subject_token` is lab theatre (any non-empty string).
- `generateAccessToken` mints a lab Bearer token for the target SA; scopes are recorded but not enforced against Google APIs.
- `signBlob` is SHA-256 theatre, not PKCS#1 / RSA signing.
- `signJwt` is unsigned lab JWT theatre (`alg=none`), not RSA/ES256.
- Soft-delete has no 30-day purge timer; rows remain until process data is wiped.
- Key material is a lab credentials JSON (not a PKCS#8 RSA PEM).
- gRPC `IAM` admin service is not registered yet; use REST.

## Deferred depth

- OIDC discovery + JWT signature verification for real external IdPs (STS `POST /v1/token` is exchange theatre only)
- Custom roles CRUD beyond seeded built-in roles (`roles/owner`, `roles/editor`, `roles/viewer`, `roles/iam.serviceAccountTokenCreator`, and other seeded lab roles)
- gRPC IAM Admin service registration

## Verification / CLI smoke

```bash
go test ./internal/kernel/authz/ ./internal/services/iam/ ./internal/kernel/authn/ ./internal/server/ -count=1 -run 'TokenCreator|STS|WIF|GenerateAccess|IAM|Token'
gcloud config set api_endpoint_overrides/iam http://127.0.0.1:4588/
gcloud iam service-accounts create lab-runner \
  --display-name="Lab Runner" --project=noctaxris-gcp-local
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"displayName":"Lab Pool"}' \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/workloadIdentityPools?workloadIdentityPoolId=lab-pool"
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"displayName":"OIDC","oidc":{"issuerUri":"https://example.com"}}' \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/workloadIdentityPools/lab-pool/providers?workloadIdentityPoolProviderId=oidc"
curl -s -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "grant_type=urn:ietf:params:oauth:grant-type:token-exchange" \
  --data-urlencode "audience=//iam.googleapis.com/projects/noctaxris-gcp-local/locations/global/workloadIdentityPools/lab-pool/providers/oidc" \
  --data-urlencode "subject_token=lab-sub" \
  "http://127.0.0.1:4588/v1/token"
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"scope":["https://www.googleapis.com/auth/cloud-platform"],"lifetime":"600s"}' \
  "http://127.0.0.1:4588/v1/projects/-/serviceAccounts/lab-runner@noctaxris-gcp-local.iam.gserviceaccount.com:generateAccessToken"
```

## SDK note

Go client `cloud.google.com/go/iam/admin/apiv1` has no official emulator host
env var. Use `option.WithEndpoint("127.0.0.1:4588")` against the lab listener.
