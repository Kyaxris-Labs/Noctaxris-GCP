# Security Command Center

Lab Security Command Center REST for sources and findings. Stores finding
metadata for forensic inject labs. No continuous detectors, pub/sub finding
notifications, or Security Health Analytics modules.

## Status

**lab** — sources and findings create/get/list/delete under
`/v1/organizations/{org}/...` and `/v1/projects/{project}/...`. Lab
`InjectFindings` is env-gated (default off).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`):

| Method | Path |
|--------|------|
| `POST` | `/v1/organizations/{org}/sources?sourceId=` |
| `GET` | `/v1/organizations/{org}/sources` |
| `GET` | `/v1/organizations/{org}/sources/{id}` |
| `DELETE` | `/v1/organizations/{org}/sources/{id}` |
| `POST` | `/v1/organizations/{org}/sources/{id}/findings?findingId=` |
| `GET` | `/v1/organizations/{org}/sources/{id}/findings` |
| `GET` | `/v1/organizations/{org}/sources/{id}/findings/{fid}` |
| `DELETE` | `/v1/organizations/{org}/sources/{id}/findings/{fid}` |
| `POST` | `/v1/organizations/{org}/sources/{id}/findings/{fid}:setState` (`splitColonAction` on finding segment) |
| `POST` | `/v1/projects/{p}/sources?...` (same shape under projects) |
| `POST` | `/_noctaxris-gcp/lab/securitycenter:injectFindings` |

Finding body fields used: `category` (required), `severity`, `state`
(`ACTIVE`/`INACTIVE`), `resourceName`, `externalUri`, `description`,
`sourceProperties`, `eventTime`. List returns `listFindingsResults` with
`{finding}` wrappers (SCC list shape lite).

### Lab inject

Requires `NOCTAXRIS_GCP_SCC_INJECT=1` on the API process (default off returns
PermissionDenied). Creates the source when missing.

```json
{
  "parent": "organizations/noctaxris-gcp-org",
  "sourceId": "lab-inject",
  "findings": [
    {
      "findingId": "f1",
      "category": "OPEN_FIREWALL",
      "severity": "HIGH",
      "resourceName": "//compute.googleapis.com/projects/noctaxris-gcp-local/global/networks/default"
    }
  ]
}
```

Response: `{"findingNames":["..."],"source":"..."}`.

## Authz

Checked on the parent resource (`organizations/{org}` or `projects/{project}`):

- `securitycenter.sources.create|get|list|delete`
- `securitycenter.findings.create|get|list|delete|setState`

Inject uses `securitycenter.findings.create` after the env gate.

## Emulator limits

- No continuous detectors / Security Health Analytics / Event Threat Detection
- No finding notifications, mute configs, or asset inventory linkage
- No gRPC SecurityCenter API surface
- Inject is lab-only (not a public Google API)

## Deferred depth

- `findings.group` / filter expressions beyond list-all
- Mute configs and security marks mutation
- Organization settings / notification configs

## Verification / CLI smoke

```bash
go test ./internal/services/securitycenter/ ./internal/store/ -run 'SCC|SecurityCenter|Inject' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
ORG=noctaxris-gcp-org
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/organizations/$ORG/sources?sourceId=lab-src" \
  -d '{"displayName":"Lab"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/organizations/$ORG/sources/lab-src/findings?findingId=f1" \
  -d '{"category":"OPEN_FIREWALL","severity":"HIGH"}'
# Inject (API process must have NOCTAXRIS_GCP_SCC_INJECT=1):
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/_noctaxris-gcp/lab/securitycenter:injectFindings" \
  -d '{"parent":"organizations/noctaxris-gcp-org","sourceId":"inj","findings":[{"category":"MALWARE","severity":"CRITICAL","findingId":"x1"}]}'
```
