# Cloud Resource Manager

Lab-complete project get and project IAM policy methods on the Resource Manager
v3 REST surface.

## Lab actions

| Action | Method | Path |
|--------|--------|------|
| Get project | `GET` | `/v3/projects/{project}` |
| Get IAM policy | `POST` | `/v3/projects/{project}:getIamPolicy` |
| Set IAM policy | `POST` | `/v3/projects/{project}:setIamPolicy` |
| Test IAM permissions | `POST` | `/v3/projects/{project}:testIamPermissions` |

Permissions checked (except `testIamPermissions`): `resourcemanager.projects.get`,
`resourcemanager.projects.getIamPolicy`, `resourcemanager.projects.setIamPolicy`.
The root principal bypasses IAM evaluation. `testIamPermissions` returns the
subset of requested permissions the caller holds; calling it does not require
its own permission.

## Emulator limits

- Single seeded project (`NOCTAXRIS_GCP_PROJECT`); no create/list/delete project.
- No organizations, folders, or tag bindings.
- Project `name` uses `projects/{projectId}` (string id), not a numeric project number.
- gRPC `Projects` service is not registered yet; use REST.

## gcloud smoke

```bash
gcloud config set api_endpoint_overrides/cloudresourcemanager http://127.0.0.1:4588/
gcloud projects describe noctaxris-gcp-local --format=json
gcloud projects get-iam-policy noctaxris-gcp-local --format=json
```

Point `CLOUDSDK_AUTH_ACCESS_TOKEN` (or Application Default Credentials equivalent)
at the emulator root Bearer token.

## SDK note

Go client `cloud.google.com/go/resourcemanager/apiv3` has no official emulator
host env var. Dial the lab with `option.WithEndpoint("127.0.0.1:4588")` and
insecure / WithoutAuthentication as required for local Bearer use.
