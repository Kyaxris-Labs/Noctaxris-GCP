# Service Usage

Lab-complete enable, disable, batchEnable, get, and list for project services.

## Lab actions

| Action | Method | Path |
|--------|--------|------|
| List services | `GET` | `/v1/projects/{project}/services` |
| Get service | `GET` | `/v1/projects/{project}/services/{service}` |
| Enable | `POST` | `/v1/projects/{project}/services/{service}:enable` |
| Disable | `POST` | `/v1/projects/{project}/services/{service}:disable` |
| Batch enable | `POST` | `/v1/projects/{project}/services:batchEnable` |

Service names look like `storage.googleapis.com`. Resource names are
`projects/{project}/services/{service}`.

List accepts `filter=state:ENABLED` or `filter=state:DISABLED`.

Batch enable body: `{"serviceIds":["storage.googleapis.com",...]}` (max 20).
The store update is atomic.

Permissions: `serviceusage.services.list|get|enable|disable` on
`projects/{project}`.

EnsureRoot seeds Wave 1 APIs as `ENABLED` for the default project.

## Emulator limits

- No batchGet / batchDisable.
- Enable/disable/batchEnable return a completed Operation immediately (no async LRO worker).
- Get of an unknown service returns `DISABLED` rather than only catalog hits.
- Config payloads are minimal (`name` only).
- gRPC `ServiceUsage` is not registered yet; use REST.

## gcloud smoke

```bash
gcloud config set api_endpoint_overrides/serviceusage http://127.0.0.1:4588/
gcloud services list --enabled --project=noctaxris-gcp-local
gcloud services enable storage.googleapis.com --project=noctaxris-gcp-local
gcloud services disable storage.googleapis.com --project=noctaxris-gcp-local
```

## SDK note

Go client `cloud.google.com/go/serviceusage/apiv1` has no official emulator host
env var. Use `option.WithEndpoint("127.0.0.1:4588")` against the lab listener.
