# Cloud Functions

Lab Cloud Functions v2 REST control plane. Create without `storageSource` is
`ACTIVE` immediately. Create with `buildConfig.source.storageSource` starts
`DEPLOYING` until the generateUploadUrl path accepts a source zip, then flips to
`ACTIVE`. No Cloud Build; optional HTTP `:invoke` stub returns configured JSON.
Create with `eventTrigger` / `eventarcTrigger` (or Eventarc-shaped
`eventFilters`) auto-inserts an Eventarc trigger whose destination is the
function (`cloudFunction`).

## Status

**lab** — functions CRUD, patch merge, `generateUploadUrl` + source upload accept
theatre, function IAM get/set, `:invoke` stub, Eventarc event-trigger wiring.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v2/projects/{p}/locations/{loc}/functions?functionId=` |
| `POST` | `/v2/projects/{p}/locations/{loc}/functions:generateUploadUrl` |
| `PUT`/`POST` | `/v2/projects/{p}/locations/{loc}/functions:upload/{uploadId}` |
| `GET` | `/v2/projects/{p}/locations/{loc}/functions` |
| `GET` | `/v2/projects/{p}/locations/{loc}/functions/{fn}` |
| `PATCH` | `/v2/projects/{p}/locations/{loc}/functions/{fn}` |
| `DELETE` | `/v2/projects/{p}/locations/{loc}/functions/{fn}` |
| `POST`/`GET` | `/v2/projects/{p}/locations/{loc}/functions/{fn}:invoke` |
| `GET`/`POST` | `.../functions/{fn}:getIamPolicy` / `:setIamPolicy` |

Optional request field `labResponse` (object or string) sets the invoke body. `buildConfig` / `serviceConfig` / `labels` / `description` are echoed on get when present. Patch merges JSON fields into stored config.

`generateUploadUrl` returns a lab `uploadUrl` and `storageSource` object names.
`PUT`/`POST` to `uploadUrl` accepts body bytes (theatre; not executed) and
activates matching `DEPLOYING` functions.

### Event triggers

On create, these shapes insert Eventarc trigger
`projects/{p}/locations/{loc}/triggers/function-{functionId}` with
`destination.cloudFunction` set to the function resource name:

| Create body | Notes |
|-------------|--------|
| `eventTrigger.eventType` (+ optional `eventFilters`, `pubsubTopic`, `triggerRegion`, `channel`) | Cloud Functions v2 shape; `trigger` echoed on get |
| `eventarcTrigger` | Alias of `eventTrigger` |
| Top-level `eventFilters` without `destination` | Eventarc-shaped; destination forced to this function |

Supported event types match Eventarc lab: Pub/Sub `messagePublished` and GCS
`object.v1.finalized`. Matching Pub/Sub publish / GCS finalize delivers
in-process to the function invoke theatre (records invoke; no Bearer required
for that path).

## Authz

Checked on `projects/{project}` for control-plane actions:

- `cloudfunctions.functions.create|get|list|update|delete|getIamPolicy|setIamPolicy`

`:invoke` uses `EvaluateAny` on the **function resource** and the project
(`cloudfunctions.functions.invoke`). A non-root principal with only
`roles/cloudfunctions.invoker` on the function IAM policy can invoke; without a
project or function Invoker binding, invoke is denied. Root still bypasses.

## Emulator limits

- `:invoke` is an in-process stub only: returns stored `labResponse` JSON (or a
  small default). No container, no Cloud Run routing, no nested DinD, no cold start
- Upload zip is accepted and tracked; no extract, build, or runtime
- Eventarc wiring on create covers Pub/Sub and GCS finalize only; other event
  types are rejected
- Function IAM get/set is stored; Invoker is evaluated on `:invoke` only
- Delete function does not cascade-delete the auto-created Eventarc trigger

## Deferred depth

- Real Cloud Build packaging
- Cascade delete of wired Eventarc triggers; authenticated HTTP deliver to
  `:invoke` (Bearer mint)
- 1st gen API

## Verification / CLI smoke

```bash
go test ./internal/services/cloudfunctions/ ./internal/kernel/authz/ ./internal/server/ -run 'CloudFunctions|RunAndFunctionsInvoker|EventarcCloudFunction' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
UP=$(curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/functions:generateUploadUrl" \
  -d '{}')
echo "$UP"
# PUT body to uploadUrl from the response, then create with matching storageSource
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/functions?functionId=fn1" \
  -d '{"labResponse":{"result":"ok"},"eventTrigger":{"eventType":"google.cloud.pubsub.topic.v1.messagePublished","pubsubTopic":"projects/noctaxris-gcp-local/topics/fn-events"}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/functions/fn1:setIamPolicy" \
  -d '{"policy":{"bindings":[{"role":"roles/cloudfunctions.invoker","members":["serviceAccount:fn-invoker@example.com"]}],"etag":"ACAB"}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/triggers/function-fn1"
```
