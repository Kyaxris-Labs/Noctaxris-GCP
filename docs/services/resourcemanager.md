# Cloud Resource Manager

Lab-complete project get/list/search/patch and project IAM policy methods on the
Resource Manager REST surface, plus v1 getAncestry theatre, a seeded organization,
and folders CRUD lite.

## Lab actions

| Action | Method | Path |
|--------|--------|------|
| List projects | `GET` | `/v3/projects` |
| Search projects | `POST` | `/v3/projects:search` |
| Get project | `GET` | `/v3/projects/{project}` |
| Patch project | `PATCH` | `/v3/projects/{project}` |
| Get IAM policy | `POST` | `/v3/projects/{project}:getIamPolicy` |
| Set IAM policy | `POST` | `/v3/projects/{project}:setIamPolicy` |
| Test IAM permissions | `POST` | `/v3/projects/{project}:testIamPermissions` |
| Get ancestry (theatre) | `POST` | `/v1/projects/{project}:getAncestry` |
| Get organization | `GET` | `/v3/organizations/{organization}` |
| List folders | `GET` | `/v3/folders?parent=` |
| Create folder | `POST` | `/v3/folders` |
| Get folder | `GET` | `/v3/folders/{folder}` |
| Patch folder | `PATCH` | `/v3/folders/{folder}` |
| Delete folder | `DELETE` | `/v3/folders/{folder}` |

Permissions checked (except `testIamPermissions`): `resourcemanager.projects.get`,
`resourcemanager.projects.list`, `resourcemanager.projects.search`,
`resourcemanager.projects.update`, `resourcemanager.projects.getIamPolicy`,
`resourcemanager.projects.setIamPolicy`, `resourcemanager.organizations.get`,
`resourcemanager.folders.create|get|list|update|delete`.
List/search authorize against `projects/-` (or the optional `parent` query for
list). Folder list/create authorize against the `parent` resource. The root
principal bypasses IAM evaluation. `testIamPermissions` returns the subset of
requested permissions the caller holds; calling it does not require its own
permission.

### Organization

The lab seeds one organization resource name:

`organizations/noctaxris-gcp-org`

Seeded projects report `parent` as that organization. `getAncestry` returns the
project then the organization (no folder unless you create one and wire it
yourself outside this theatre).

### Folders

Create body requires `parent` (`organizations/...` or `folders/...`) and
`displayName`. Delete marks `DELETE_REQUESTED` (soft delete); list omits deleted
folders unless `showDeleted=true`. Patch updates `displayName` (optional
`updateMask=displayName`). Lab returns Folder JSON synchronously rather than an
LRO Operation.

List and search return seeded projects only. Search body field `query` matches
project id or display name (case-insensitive substring); empty query returns all.

Patch updates `displayName` (optional `updateMask=displayName` query). The lab
returns the Project JSON synchronously rather than an LRO Operation.

## Emulator limits

- No create/delete project; only the seeded default project (and any rows added
  via store tooling).
- No labels or tag bindings.
- Nested folder height/fanout constraints are not enforced beyond parent existence.
- Project `name` uses `projects/{projectId}` (string id), not a numeric project number.
- gRPC `Projects` / `Folders` / `Organizations` services are not registered yet; use REST.

## Verification / CLI smoke

```bash
go test ./internal/server/ -run CRM -count=1
gcloud config set api_endpoint_overrides/cloudresourcemanager http://127.0.0.1:4588/
gcloud projects describe noctaxris-gcp-local --format=json
gcloud projects get-iam-policy noctaxris-gcp-local --format=json
gcloud resource-manager folders list --organization=noctaxris-gcp-org --format=json
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:4588/v3/organizations/noctaxris-gcp-org
```

Point `CLOUDSDK_AUTH_ACCESS_TOKEN` (or Application Default Credentials equivalent)
at the emulator root Bearer token.

## SDK note

Go client `cloud.google.com/go/resourcemanager/apiv3` has no official emulator
host env var. Dial the lab with `option.WithEndpoint("127.0.0.1:4588")` and
insecure / WithoutAuthentication as required for local Bearer use.
