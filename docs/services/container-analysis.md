# Container Analysis

REST v1 occurrence list/create/get plus vulnerability summary theatre on the
shared listener (`http://127.0.0.1:4588`). There is no separate gcloud
`containeranalysis` command group; clients usually call Artifact Analysis /
Container Analysis through `gcloud artifacts …` with an endpoint override.

## Status

**lab** — project-scoped (and location-scoped alias) occurrence CRUD lite,
Grafeas-style `filter` parsing for list and summary, `pageSize` / `pageToken`
pagination, and `occurrences:vulnerabilitySummary` counts. Binary Authorization
admit still requires an exact stored `resourceUri` match (see
[cloud-run.md](cloud-run.md)).

## Wire protocol

| Method | Path |
|--------|------|
| `GET` / `POST` | `/v1/projects/{p}/occurrences` |
| `GET` | `/v1/projects/{p}/occurrences/{id}` |
| `GET` | `/v1/projects/{p}/occurrences:vulnerabilitySummary` |
| `GET` / `POST` | `/v1/projects/{p}/locations/{loc}/occurrences` |
| `GET` | `/v1/projects/{p}/locations/{loc}/occurrences/{id}` |
| `GET` | `/v1/projects/{p}/locations/{loc}/occurrences:vulnerabilitySummary` |

Location-scoped paths use the same project store (`{loc}` is accepted and
ignored for storage).

### List filter

Query `filter` supports:

- `kind="…"` (spaces around `=` optional)
- `resourceUrl="…"` / `resourceUri="…"` (equality; bare and `https://` forms match each other;
  `@sha256-<hex>` matches stored `@sha256:<hex>`)
- `noteId="…"` (last segment of `noteName`, or full name)
- `has_prefix(resourceUrl,"…")` / `has_prefix(resourceUri,"…")`
- `AND` / `OR` / parentheses

Empty filter returns all project occurrences.

List and vulnerabilitySummary responses rewrite `resourceUri` to the Artifact Registry /
gcloud attach form: `https://` prefix and `@sha256:<hex>` → `@sha256-<hex>`. That lets
`gcloud artifacts docker images list --show-occurrences` key metadata onto image rows.
Stored values (and Binary Authorization admit) keep the original URI.

### Pagination

`pageSize` (default 100, max 1000), `pageToken` (opaque offset string), and
`nextPageToken` when more rows remain.

### Vulnerability summary

`GET …/occurrences:vulnerabilitySummary?filter=…` returns
`{ "counts": [ { "resourceUri", "severity", "totalCount", "fixableCount" }, … ] }`
for `kind=VULNERABILITY` rows. Per-severity rows use `CRITICAL` / `HIGH` /
`MEDIUM` / `LOW` / `MINIMAL`. Each resource also gets a
`SEVERITY_UNSPECIFIED` total row (API total across severities). Counts are
decimal strings. `fixableCount` follows `vulnerability.fixAvailable` or a
non-empty `packageIssue[].fixedVersion`.

## Authz

Checked on `projects/{project}`:

- `containeranalysis.occurrences.list` (list + vulnerabilitySummary)
- `containeranalysis.occurrences.get`
- `containeranalysis.occurrences.create`

Occurrence create also evaluates `containeranalysis.notes.attachOccurrence` on
the request `noteName` (for example `projects/{provider}/notes/{id}`).

Root bypasses. Viewer suffix grants cover get/list.

## Emulator limits

- Notes, discoveries, and On-Demand Scanning are not implemented
- No attestation signature verification
- Vulnerability fields are stored JSON theatre only (no scanner)
- Binary Authorization admit is exact `resource_uri` equality in the store
  (list filters may match https/bare; admit does not)

## Deferred depth

- Notes CRUD and discovery occurrences
- On-Demand Scanning and full Grafeas note graph
- Soft-delete / batch create occurrences

## Verification / CLI smoke

```bash
go test ./internal/services/containeranalysis/ ./internal/services/cloudrun/ \
  -run 'ContainerAnalysis|BinaryAuthorization|Occurrence' -count=1

export CLOUDSDK_AUTH_ACCESS_TOKEN="$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN"
export CLOUDSDK_API_ENDPOINT_OVERRIDES_CONTAINERANALYSIS=http://127.0.0.1:4588/
# or: gcloud config set api_endpoint_overrides/containeranalysis http://127.0.0.1:4588/

# Create an attestation occurrence (curl; gcloud artifacts create paths vary by version):
curl -s -H "Authorization: Bearer $CLOUDSDK_AUTH_ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/occurrences?occurrenceId=att-1" \
  -d '{"resourceUri":"us-docker.pkg.dev/noctaxris-gcp-local/apps/web:1","kind":"ATTESTATION","noteName":"projects/noctaxris-gcp-local/notes/attestor"}'

curl -s -H "Authorization: Bearer $CLOUDSDK_AUTH_ACCESS_TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/occurrences?filter=kind%3D%22ATTESTATION%22%20AND%20resourceUrl%3D%22us-docker.pkg.dev%2Fnoctaxris-gcp-local%2Fapps%2Fweb%3A1%22"

curl -s -H "Authorization: Bearer $CLOUDSDK_AUTH_ACCESS_TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/occurrences:vulnerabilitySummary"
```
