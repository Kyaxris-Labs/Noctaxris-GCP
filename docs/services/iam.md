# IAM

Lab-complete IAM Admin REST for service accounts and user-managed keys, plus
Workload Identity Federation pool/provider CRUD, STS token exchange (theatre by
default; optional OIDC JWKS verify), and service account `generateAccessToken`
impersonation theatre. Project IAM policies live on Cloud Resource Manager;
per-service-account IAM policies are stored on the SA resource name.

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
| Patch WIF provider | `PATCH` | `.../workloadIdentityPools/{pool}/providers/{provider}` |
| Delete WIF provider | `DELETE` | `.../workloadIdentityPools/{pool}/providers/{provider}` |
| Create custom role | `POST` | `/v1/projects/{project}/roles` |
| List custom roles | `GET` | `/v1/projects/{project}/roles` |
| Get custom role | `GET` | `/v1/projects/{project}/roles/{roleId}` |
| Patch custom role | `PATCH` | `/v1/projects/{project}/roles/{roleId}` |
| Delete custom role | `DELETE` | `/v1/projects/{project}/roles/{roleId}` |
| Undelete custom role | `POST` | `/v1/projects/{project}/roles/{roleId}:undelete` |
| STS token exchange (lab) | `POST` | `/v1/token` |

Permissions: `iam.serviceAccounts.*`, `iam.serviceAccountKeys.*`,
`iam.roles.*`, `iam.workloadIdentityPools.*`, `iam.workloadIdentityPoolProviders.*` on
`projects/{project}` (custom methods use the same project resource for authz).
SA IAM policy documents are keyed by
`projects/{project}/serviceAccounts/{email}`. Nested SA resources also inherit
project-level bindings in the lab evaluator. When CRM ancestry is wired
(`Parents` on the evaluator), project and folder Evaluate also unions folder
and organization allow policies (same inheritance model as GCP resource
hierarchy). Org/folder bindings use the existing CRM getIamPolicy/setIamPolicy
documents.

### Custom roles

Project custom roles use Google IAM Admin shapes under
`/v1/projects/{project}/roles`. Create body is
`{"roleId":"...","role":{"title","description","includedPermissions","stage"}}`.
Role resource names are `projects/{project}/roles/{roleId}` and may be bound in
CRM/IAM policies. Authz evaluates only the listed `includedPermissions` (no
`{svc}.*` catch-all for unknown predefined roles such as `roles/xyz.admin`).
Delete is soft-delete (`deleted: true`); list omits deleted rows unless
`showDeleted=true`. Get on a soft-deleted role returns HTTP 200 with
`deleted: true` (not 404). Soft-deleted roles stop granting immediately.
`:undelete` restores the role; creating the same `roleId` fails until undelete
(`iam.roles.undelete` on the project).

Creating a key seals the credentials JSON at rest, returns `privateKeyData`
(base64) once, and registers a hashed access token so
`Authorization: Bearer <token>` authenticates as that service account. The lab
token is stored in the credentials JSON `private_key` field.

When Organization Policy constraint `iam.disableServiceAccountKeyCreation` is
enforced on the project (or an ancestor), create key returns
`FAILED_PRECONDITION` (teach path; see [orgpolicy.md](orgpolicy.md)).

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
`issuerUri`, `allowedAudiences`, and `attributeMapping` JSON. Provider PATCH
supports `updateMask` for `displayName`, `description`, `disabled`,
`attributeMapping`, `oidc.issuerUri`, and `oidc.allowedAudiences` (mask without
a body field is `InvalidArgument`; empty `allowedAudiences` in the body clears
stored extras when the mask includes `oidc.allowedAudiences`). Soft-delete sets
`state=DELETED`.

`POST /v1/token` is a public STS endpoint (no Bearer required; the
`subject_token` authenticates the external identity). Required fields:

| Field | Notes |
|-------|--------|
| `grant_type` | Must be `urn:ietf:params:oauth:grant-type:token-exchange` |
| `audience` | WIF provider resource name; optional `//iam.googleapis.com/` prefix |
| `subject_token` | Default theatre: any non-empty string. With verify on: compact RS256 JWT |

Optional: `subject_token_type` (accepted, not validated). JSON camelCase and
form `snake_case` field names are both accepted.

**Theatre (default):** When `NOCTAXRIS_GCP_STS_VERIFY` is unset/off, or the
provider `issuerUri` is empty, the lab accepts any non-empty `subject_token`
(unit tests and smoke stay on this path).

**OIDC verify (opt-in):** Set `NOCTAXRIS_GCP_STS_VERIFY=1` (or `true`). When the
provider has a non-empty `issuerUri`, STS fail-closes unless the subject JWT
verifies:

1. Fetch OIDC discovery (`{issuer}/.well-known/openid-configuration`) and/or
   `{issuer}/.well-known/jwks.json` **only** through shared `httpegress`
   (lab-local loopback `:4588`, or `NOCTAXRIS_GCP_HTTP_EGRESS=1` plus exact
   allowlist entries for discovery and `jwks_uri`). No open SSRF to arbitrary
   issuer hosts.
2. Verify RS256 signature against JWKS; check `iss` (matches `issuerUri`),
   `aud` (provider resource name, `//iam.googleapis.com/{provider}`, and any
   stored `oidc.allowedAudiences`; JWT `aud` may be a string or array), `exp`
   (required; must be future), optional `nbf`, and non-empty `sub`.
   Discovery `jwks_uri` is followed only when it equals
   `{issuerUri}/.well-known/jwks.json` (scheme, host, and path). Other
   same-origin paths are ignored; the lab then fetches that canonical JWKS URL.
   Failed verify returns generic `invalid subject_token` (not raw verifier errors).

On success the lab returns `access_token`, `token_type=Bearer`, `expires_in=3600`,
and registers the token as principal `wif:{providerId}:{subject}` where
`subject` is a sanitized form of the subject (theatre: raw `subject_token`;
verify: JWT `sub`; alnum/`-`/`_`/`.`; others become `-`; max 64 chars).
Unknown, deleted, or disabled pools/providers fail closed (`UNAUTHENTICATED`).
Bind that principal on CRM/IAM policies using the literal member string
`wif:{providerId}:{subject}` (the evaluator does not rewrite it to
`serviceAccount:`).

**Lab OIDC issuer (`oidc-lab`):** The API listener exposes discovery and JWKS at
`/_noctaxris-gcp/oidc-lab/.well-known/openid-configuration` and
`/_noctaxris-gcp/oidc-lab/.well-known/jwks.json` (public; no Bearer). Issuer
is `http://{host}/_noctaxris-gcp/oidc-lab` from the request host (Compose uses
the container bind). There is no token-mint route; sign RS256 JWTs with the stable lab JWKS key
(see `go test ./internal/server/ -run OIDC` for a working pattern). Point a WIF
provider `issuerUri` at that issuer (loopback `:4588` or your publish address)
and set `NOCTAXRIS_GCP_STS_VERIFY=1` for end-to-end exchange without an
external IdP.

List keys accepts `pageSize` (default 100, max 100) and `pageToken` (integer
offset); responses may include `nextPageToken`.

Create service account fails with `FAILED_PRECONDITION` when
`iam.googleapis.com` is DISABLED for the project (Service Usage gate).

## Emulator limits

- STS OIDC verify is opt-in (`NOCTAXRIS_GCP_STS_VERIFY=1`); default theatre accepts any non-empty `subject_token`.
- Verify fetches JWKS/discovery only via `httpegress` (lab-local `:4588` or egress + exact allowlist); no arbitrary issuer SSRF. Discovery `jwks_uri` is honored only at `{issuerUri}/.well-known/jwks.json`.
- Verify accepts RS256 only; `aud` must match the provider resource name (with or without `//iam.googleapis.com/` prefix) plus stored `oidc.allowedAudiences`.
- `oidc-lab` is discovery/JWKS only (no mint); anyone who can reach the JWKS can sign lab JWTs when verify uses that issuer; external issuers still need egress + exact allowlist when not on loopback oidc-lab.
- `generateAccessToken` mints a lab Bearer token for the target SA; scopes are recorded but not enforced against Google APIs.
- `signBlob` is SHA-256 theatre, not PKCS#1 / RSA signing.
- `signJwt` is unsigned lab JWT theatre (`alg=none`), not RSA/ES256.
- Soft-delete has no 30-day purge timer; rows remain until process data is wiped.
- Key material is a lab credentials JSON (not a PKCS#8 RSA PEM).
- Custom roles are project-scoped only (no organization custom roles CRUD).
- Predefined `roles/{svc}.*` grants `{svc}.*` only for an allowlisted set of lab services; unknown services fail closed.
- gRPC `IAM` admin service is not registered yet; use REST.

## Deferred depth

- Organization-level custom roles CRUD
- gRPC IAM Admin service registration

## Verification / CLI smoke

```bash
go test ./internal/kernel/authz/ ./internal/services/iam/ ./internal/store/ ./internal/kernel/authn/ ./internal/server/ -count=1 -run 'CustomRole|UnknownRole|TokenCreator|STS|WIF|GenerateAccess|IAM|Token|OrgIAM|FolderIAM'
gcloud config set api_endpoint_overrides/iam http://127.0.0.1:4588/
gcloud iam service-accounts create lab-runner \
  --display-name="Lab Runner" --project=noctaxris-gcp-local
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"roleId":"bucketLister","role":{"title":"Bucket Lister","includedPermissions":["storage.buckets.list"]}}' \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/roles"
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"displayName":"Lab Pool"}' \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/workloadIdentityPools?workloadIdentityPoolId=lab-pool"
curl -s "http://127.0.0.1:4588/_noctaxris-gcp/oidc-lab/.well-known/openid-configuration"
curl -s "http://127.0.0.1:4588/_noctaxris-gcp/oidc-lab/.well-known/jwks.json"
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"displayName":"OIDC","oidc":{"issuerUri":"http://127.0.0.1:4588/_noctaxris-gcp/oidc-lab","allowedAudiences":["https://custom-aud"]}}' \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/workloadIdentityPools/lab-pool/providers?workloadIdentityPoolProviderId=oidc"
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"oidc":{"allowedAudiences":["https://new-aud"]},"updateMask":"oidc.allowedAudiences"}' \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/workloadIdentityPools/lab-pool/providers/oidc"
curl -s -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "grant_type=urn:ietf:params:oauth:grant-type:token-exchange" \
  --data-urlencode "audience=//iam.googleapis.com/projects/noctaxris-gcp-local/locations/global/workloadIdentityPools/lab-pool/providers/oidc" \
  --data-urlencode "subject_token=lab-sub" \
  "http://127.0.0.1:4588/v1/token"
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"scope":["https://www.googleapis.com/auth/cloud-platform"],"lifetime":"600s"}' \
  "http://127.0.0.1:4588/v1/projects/-/serviceAccounts/lab-runner@noctaxris-gcp-local.iam.gserviceaccount.com:generateAccessToken"
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{}' \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/roles/bucketLister:undelete"
# STS verify (NOCTAXRIS_GCP_STS_VERIFY=1): sign a JWT with oidc-lab JWKS (see internal/server tests) and exchange:
# curl -s -H "Content-Type: application/x-www-form-urlencoded" \
#   --data-urlencode "grant_type=urn:ietf:params:oauth:grant-type:token-exchange" \
#   --data-urlencode "audience=//iam.googleapis.com/projects/noctaxris-gcp-local/locations/global/workloadIdentityPools/lab-pool/providers/oidc" \
#   --data-urlencode "subject_token=<RS256 JWT>" \
#   "http://127.0.0.1:4588/v1/token"
```

## SDK note

Go client `cloud.google.com/go/iam/admin/apiv1` has no official emulator host
env var. Use `option.WithEndpoint("127.0.0.1:4588")` against the lab listener.
