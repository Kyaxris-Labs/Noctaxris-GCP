# Cloud Run

Lab Cloud Run Admin API v2 REST for services, revisions, and jobs, plus a
Knative Serving v1 facade over the same store (for `gcloud run`). Service
create/update on v2 returns a completed Operation (`done: true` + `response`);
GET returns the service. Terraform:
`cloud_run_v2_custom_endpoint = "http://127.0.0.1:4588/v2/"` (see
`tests/terraform/stacks/lab-run`).

## Mock vs nested `:invoke`

| Mode | When | Behavior |
|------|------|----------|
| Mock (default) | `NOCTAXRIS_GCP_DOCKER_HOST` empty | In-process theatre only; no container start; no host `docker.sock` |
| Mock (forced) | `template.labResponseBody` set (or `RESPONSE_BODY` env) | Nested path skipped even if Docker host is configured |
| Nested (opt-in) | `NOCTAXRIS_GCP_DOCKER_HOST` + `NOCTAXRIS_GCP_DOCKER_CERT_PATH` set; no `labResponseBody` | `DockerInvoker` one-shot via TLS DinD; host `docker.sock` / `unix://` / `npipe://` refused |
| Nested soft-fail | Nested dial/run fails; `NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED` unset | Falls back to mock; response may include `engine` detail |
| Nested fail-closed | Nested dial/run fails; `NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED=1` or `true` | Hard error; no mock fallback |

## Status

**lab** — services CRUD with traffic metadata, jobs CRUD theatre, revision list,
service IAM get/set, Knative Serving v1 list/get/create/replace/delete,
`:invoke` mock with status/delay theatre and opt-in nested DinD one-shot
(`NOCTAXRIS_GCP_DOCKER_HOST`).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

### Admin API v2

| Method | Path |
|--------|------|
| `POST` | `/v2/projects/{p}/locations/{loc}/services?serviceId=` |
| `GET` | `/v2/projects/{p}/locations/{loc}/services` |
| `GET` | `/v2/projects/{p}/locations/{loc}/services/{svc}` |
| `PATCH` | `/v2/projects/{p}/locations/{loc}/services/{svc}` |
| `DELETE` | `/v2/projects/{p}/locations/{loc}/services/{svc}` |
| `GET` | `/v2/projects/{p}/locations/{loc}/services/{svc}/revisions` |
| `POST`/`GET` | `/v2/projects/{p}/locations/{loc}/services/{svc}:invoke` |
| `GET`/`POST` | `.../services/{svc}:getIamPolicy` / `:setIamPolicy` |
| `POST` | `/v2/projects/{p}/locations/{loc}/jobs?jobId=` |
| `GET` | `/v2/projects/{p}/locations/{loc}/jobs` |
| `GET`/`PATCH`/`DELETE` | `/v2/projects/{p}/locations/{loc}/jobs/{job}` |
| `GET` | `/computeMetadata/v1` and `/computeMetadata/v1/...` (`Metadata-Flavor: Google`) |

### Knative Serving v1

Namespace is the project id. Location comes from the request `Host`: a leading
`{region}-` prefix when present (for example `us-central1-run.googleapis.com` or
`us-central1-127.0.0.1`), otherwise `us-central1`. Responses use Knative
envelopes (`apiVersion` / `kind` `Service` / `ServiceList` / `Revision` /
`RevisionList`). Conditions use status `True` / `False`. Revision names are the
short form (`{service}-{generation}`). `template.serviceAccount` maps to
`spec.template.spec.serviceAccountName`.

| Method | Path |
|--------|------|
| `GET` / `POST` | `/apis/serving.knative.dev/v1/namespaces/{project}/services` |
| `GET` / `PUT` / `DELETE` | `/apis/serving.knative.dev/v1/namespaces/{project}/services/{name}` |
| `GET` | `/apis/serving.knative.dev/v1/namespaces/{project}/revisions` (`labelSelector=serving.knative.dev/service=ID`) |
| `GET` / `DELETE` | `/apis/serving.knative.dev/v1/namespaces/{project}/revisions/{name}` |
| `GET` | `/apis/serving.knative.dev/v1/namespaces/{project}/configurations` and `.../configurations/{name}` (read-only Service mirrors) |
| `GET` | `/apis/serving.knative.dev/v1/namespaces/{project}/routes` and `.../routes/{name}` (read-only Service mirrors) |

When using `gcloud config set api_endpoint_overrides/run http://127.0.0.1:4588/`,
gcloud may send `Host: us-central1-127.0.0.1`. Point that host at loopback
(hosts file or cloud-hosts TLS) so the region prefix resolves.

Create/patch may include `traffic` (percent allocation to latest/revision). Optional lab fields:

| Field | Effect on `:invoke` |
|-------|---------------------|
| `template.labResponseBody` | Static JSON body |
| env `RESPONSE_BODY` | Same, if `labResponseBody` unset |
| `template.labStatusCode` | HTTP status (default 200) |
| env `RESPONSE_STATUS` | Same as `labStatusCode` |
| `template.labDelayMs` | Sleep theatre before respond (capped at 5000) |
| env `RESPONSE_DELAY_MS` | Same as `labDelayMs` |

Otherwise invoke returns `{"ok":true,"service":"..."}` without template env. Nested engine detail also omits env. Last invoke stores method, path, query, headers (Authorization omitted), and body.

Jobs are control-plane theatre only (template stored; no execution).

Create and patch on services and jobs consult Binary Authorization for every
non-empty container image in the template, including job
`template.template.containers` (Google Cloud applies an `ENFORCED` policy to
both). Any image without a matching Container Analysis occurrence
(`resourceUri`) is 403. Empty container list (or no image) under `ENFORCED` is
also deny. The lab does not verify attestation signatures. Default (no policy,
or a mode without `ENFORCED`) admits. Occurrence `POST` still requires a Bearer
principal with `containeranalysis.occurrences.create` (not a public path).

Metadata IMDS accepts `Metadata-Flavor: Google`. Identity is
`runtime@{project}.iam.gserviceaccount.com`. Email and account listing are
available on the shared listener. `.../token` mints a Bearer for that SA only
when the TCP peer is link-local (GCE IMDS shape). Host alone on the shared
listener is not enough (wall-clock expiry; token value is random). Nested step
identity uses `labtoken.Mint` / `CLOUDSDK_AUTH_ACCESS_TOKEN`, not this route.

Related REST (same listener):

| Method | Path |
|--------|------|
| `GET` / `POST` | `/v1/projects/{p}/occurrences` (Container Analysis; see [container-analysis.md](container-analysis.md)) |
| `GET` | `/v1/projects/{p}/occurrences/{id}` |
| `GET` | `/v1/projects/{p}/occurrences:vulnerabilitySummary` |
| `GET` / `PUT` | `/v1/projects/{p}/policy` (Binary Authorization) |

Point gcloud Artifact Analysis clients at the lab with
`api_endpoint_overrides/containeranalysis` (or
`CLOUDSDK_API_ENDPOINT_OVERRIDES_CONTAINERANALYSIS`). List filters may match
bare and `https://` `resourceUrl` forms; Binary Authorization admit still
requires an exact stored `resourceUri`.

## Authz

Checked on `projects/{project}` for control-plane actions:

- `run.services.create|get|list|update|delete|getIamPolicy|setIamPolicy`
- `run.revisions.get|list|delete` (Knative revision paths; viewer gets get/list)
- `run.jobs.create|get|list|update|delete`

`:invoke` uses `EvaluateAny` on the **service resource** and the project
(`run.routes.invoke`). A non-root principal with only
`roles/run.invoker` on the service IAM policy can invoke; without a project or
service Invoker binding, invoke is denied. Root skips IAM evaluation.

Knative create/replace runs the same Binary Authorization `admitTemplate` check
as Admin API v2.

## Emulator limits

- Default invoke never starts a container (`NOCTAXRIS_GCP_DOCKER_HOST` empty)
- Nested DinD is opt-in via allowlisted `tcp://` host + TLS cert dir only; host
  `docker.sock` / `unix://` / `npipe://` are rejected
- Nested dial/run failures soft-fail to mock (`engine.detail`) unless
  `NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED` is `1`/`true` (hard error, no mock)
- `template.labResponseBody` forces mock even when the engine is configured
- Domain mappings and worker pools are not implemented
- Service IAM get/set is stored; Invoker is evaluated on `:invoke` only (no
  public unauthenticated invoke without a binding)

## Deferred depth

- Traffic percent enforcement beyond stored metadata
- Official gRPC `run.googleapis.com` surface
- Knative Domains and Jobs v1

## Verification / CLI smoke

```bash
go test ./internal/services/cloudrun/ ./internal/compute/ ./internal/kernel/authz/ ./internal/server/ -run 'CloudRun|Knative|MockInvoker|RunAndFunctionsInvoker|BinaryAuthorization' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
# Mock path (labResponseBody skips nested even if DOCKER_HOST is set):
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/services?serviceId=demo" \
  -d '{"template":{"containers":[{"image":"demo"}],"labResponseBody":"{\"ok\":true}","labStatusCode":200},"traffic":[{"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST","percent":100}]}'
curl -s -H "Authorization: Bearer $TOKEN" -H "Host: us-central1-127.0.0.1" \
  "http://127.0.0.1:4588/apis/serving.knative.dev/v1/namespaces/noctaxris-gcp-local/services"
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/services/demo:setIamPolicy" \
  -d '{"policy":{"bindings":[{"role":"roles/run.invoker","members":["serviceAccount:invoker@example.com"]}],"etag":"ACAB"}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v2/projects/noctaxris-gcp-local/locations/us-central1/services/demo:invoke" \
  -d '{}'
# gcloud (after api_endpoint_overrides/run + Host quirk hosts/cloud-hosts):
# gcloud run services list --project=noctaxris-gcp-local --region=us-central1
# Nested path: default compose.yaml engine + service without labResponseBody; see docs/configuration.md
```
