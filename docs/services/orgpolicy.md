# Organization Policy

Boolean Organization Policy constraints theatre for org, folder, and project
parents. Policies get/set/list over Org Policy API v2 REST; a small known
constraint catalog can optionally block IAM key create and public bucket IAM.

## Lab actions

| Action | Method | Path |
|--------|--------|------|
| List policies | `GET` | `/v2/{parent}/policies` |
| Get policy | `GET` | `/v2/{parent}/policies/{constraint}` |
| Get effective policy | `GET` | `/v2/{parent}/policies/{constraint}:getEffectivePolicy` |
| Create policy | `POST` | `/v2/{parent}/policies?constraint=` |
| Patch policy | `PATCH` | `/v2/{parent}/policies/{constraint}` |
| Delete policy | `DELETE` | `/v2/{parent}/policies/{constraint}` |
| List constraints | `GET` | `/v2/{parent}/constraints` |

`{parent}` is one of `projects/{project}`, `folders/{folder}`, or
`organizations/{org}` (seeded org id `noctaxris-gcp-org`).

Constraint ids may be bare (`iam.disableServiceAccountKeyCreation`) or prefixed
`constraints/...`.

### Known constraints

| Constraint | Enforce effect (lab) |
|------------|----------------------|
| `iam.disableServiceAccountKeyCreation` | `POST .../serviceAccounts/.../keys` returns `FAILED_PRECONDITION` |
| `storage.publicAccessPrevention` | Bucket `setIamPolicy` with `allUsers` / `allAuthenticatedUsers` returns `FAILED_PRECONDITION` |

Boolean policy body:

```json
{
  "name": "projects/noctaxris-gcp-local/policies/iam.disableServiceAccountKeyCreation",
  "spec": { "rules": [{ "enforce": true }] }
}
```

`IsOrgPolicyConstraintEnforced` walks CRM ancestry (project → folder/org via
`CRMParent`). The nearest explicit policy wins; unset means not enforced
(lab Google-managed default for these constraints).

Permissions: `orgpolicy.policies.list|get|create|update|delete`,
`orgpolicy.constraints.list` on the parent resource. `roles/owner` covers all.

## Emulator limits

- Only the two known boolean constraints above; no custom constraints, list
  constraints, or dry-run specs
- No conditionals / tag-based rules; `reset` clears enforcement at that parent
- No async propagation delay; enforce is immediate in-process
- gcloud `org-policies` / Terraform `google_org_policy_policy` not smoke-covered
  yet (soft-skip until wired)

## Verification

```bash
go test ./internal/store/ ./internal/services/orgpolicy/ ./internal/services/iam/ -count=1 -run 'OrgPolicy|CreateKeyDenied'
```

CLI smoke (after `registerOrgPolicy` is wired in `server.New`):

```bash
export CLOUDSDK_API_ENDPOINT_OVERRIDES_ORGPOLICY=http://127.0.0.1:4588/
# or curl:
curl -H "Authorization: Bearer $ROOT_TOKEN" \
  "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/constraints"
curl -X PATCH -H "Authorization: Bearer $ROOT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"spec":{"rules":[{"enforce":true}]}}' \
  "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/policies/iam.disableServiceAccountKeyCreation"
```

gcloud override:

```bash
gcloud config set api_endpoint_overrides/orgpolicy http://127.0.0.1:4588/
```
