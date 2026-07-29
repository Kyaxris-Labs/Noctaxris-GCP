# Cloud Resource Manager

Lab-complete project get/list/search/patch (displayName + labels) and project IAM
policy methods on the Resource Manager REST surface, plus v1 getAncestry theatre,
a seeded organization with org IAM lite, folders CRUD including move, undelete,
and search, and TagKeys / TagBindings lite.

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
| Org get IAM policy | `POST` | `/v3/organizations/{organization}:getIamPolicy` |
| Org set IAM policy | `POST` | `/v3/organizations/{organization}:setIamPolicy` |
| List folders | `GET` | `/v3/folders?parent=` |
| Search folders | `GET` | `/v3/folders:search?query=` |
| Create folder | `POST` | `/v3/folders` |
| Get folder | `GET` | `/v3/folders/{folder}` |
| Patch folder | `PATCH` | `/v3/folders/{folder}` |
| Delete folder | `DELETE` | `/v3/folders/{folder}` |
| Move folder | `POST` | `/v3/folders/{folder}:move` |
| Undelete folder | `POST` | `/v3/folders/{folder}:undelete` |
| Folder get IAM policy | `POST` | `/v3/folders/{folder}:getIamPolicy` |
| Folder set IAM policy | `POST` | `/v3/folders/{folder}:setIamPolicy` |
| Create tag key | `POST` | `/v3/tagKeys` |
| List tag keys | `GET` | `/v3/tagKeys?parent=` |
| Get tag key | `GET` | `/v3/tagKeys/{tagKey}` |
| Delete tag key | `DELETE` | `/v3/tagKeys/{tagKey}` |
| Create tag binding | `POST` | `/v3/tagBindings` |
| List tag bindings | `GET` | `/v3/tagBindings?parent=` |
| Get tag binding | `GET` | `/v3/tagBindings/{tagBinding}` |
| Delete tag binding | `DELETE` | `/v3/tagBindings/{tagBinding}` |

Permissions checked (except `testIamPermissions`): `resourcemanager.projects.get`,
`resourcemanager.projects.list`, `resourcemanager.projects.search`,
`resourcemanager.projects.update`, `resourcemanager.projects.getIamPolicy`,
`resourcemanager.projects.setIamPolicy`, `resourcemanager.organizations.get`,
`resourcemanager.organizations.getIamPolicy`,
`resourcemanager.organizations.setIamPolicy`,
`resourcemanager.folders.create|get|list|update|delete|move|undelete`,
`resourcemanager.folders.getIamPolicy`, `resourcemanager.folders.setIamPolicy`,
`resourcemanager.tagKeys.create|get|list|delete`,
`resourcemanager.tagBindings.create|get|list|delete`.
List/search authorize against `projects/-` (or the optional `parent` query for
list). Folder list/create authorize against the `parent` resource; move checks
the folder and `destinationParent`. Folder search authorizes against `folders/-`.
Tag key create/list authorize against the TagKey `parent`; tag binding create/list
authorize against the binding `parent`. The root principal bypasses IAM
evaluation. `testIamPermissions` returns the subset of requested permissions the
caller holds; calling it does not require its own permission.

### Organization

The lab seeds one organization resource name:

`organizations/noctaxris-gcp-org`

Seeded projects report `parent` as that organization. `getAncestry` returns the
project then the organization (no folder unless you create one and wire it
yourself outside this theatre). Org get/set IAM policy lite uses the shared
policy store.

### Folders

Create body requires `parent` (`organizations/...` or `folders/...`) and
`displayName`. Delete marks `DELETE_REQUESTED` (soft delete); list omits deleted
folders unless `showDeleted=true`. Undelete restores `ACTIVE`. Move body requires
`destinationParent`. Search (`GET /v3/folders:search`) matches display name /
parent / state substring, or lite query forms `displayName=`, `parent=`,
`state=`. Patch updates `displayName` (optional `updateMask=displayName`). Lab
returns Folder JSON synchronously rather than an LRO Operation.

### Tag keys and bindings

TagKey create body requires `parent` (`organizations/...` or `projects/...`) and
`shortName`. Namespaced name is `{parentId}/{shortName}`. TagBinding create
accepts `parent` plus `tagValueNamespacedName` (or `tagValue` as a namespaced
string). The lab allocates `tagValues/{id}` and `tagBindings/{id}` without a
separate TagValues CRUD surface. No effective-tag policy evaluation.

List and search return seeded projects only. Search body field `query` matches
project id or display name (case-insensitive substring); empty query returns all.

Patch project updates `displayName` and/or `labels` (`updateMask=displayName`,
`labels`, or both). The lab returns the Project JSON synchronously rather than
an LRO Operation.

## Emulator limits

- No create/delete project; only the seeded default project (and any rows added
  via store tooling).
- TagValues are not a first-class CRUD API; bindings store namespaced names.
- Nested folder height/fanout constraints are not enforced beyond parent existence.
- Project `name` uses `projects/{projectId}` (string id), not a numeric project number.
- gRPC `Projects` / `Folders` / `Organizations` services are not registered yet; use REST.

## Deferred depth

- Project create / delete (seeded projects only; no CRM project lifecycle)
- TagValues first-class CRUD (bindings store namespaced names only)
- gRPC Projects / Folders / Organizations service registration

## Verification / CLI smoke

```bash
go test ./internal/services/resourcemanager/ ./internal/server/ -run 'CRM|Tag' -count=1
gcloud config set api_endpoint_overrides/cloudresourcemanager http://127.0.0.1:4588/
gcloud projects describe noctaxris-gcp-local --format=json
gcloud resource-manager folders list --organization=noctaxris-gcp-org --format=json
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"parent":"organizations/noctaxris-gcp-org","shortName":"env"}' \
  http://127.0.0.1:4588/v3/tagKeys
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"parent":"projects/noctaxris-gcp-local","tagValueNamespacedName":"noctaxris-gcp-org/env/prod"}' \
  http://127.0.0.1:4588/v3/tagBindings
```

Point `CLOUDSDK_AUTH_ACCESS_TOKEN` (or Application Default Credentials equivalent)
at the emulator root Bearer token.

## SDK note

Go client `cloud.google.com/go/resourcemanager/apiv3` has no official emulator
host env var. Dial the lab with `option.WithEndpoint("127.0.0.1:4588")` and
insecure / WithoutAuthentication as required for local Bearer use.
