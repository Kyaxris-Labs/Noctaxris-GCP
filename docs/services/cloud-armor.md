# Cloud Armor

Lab Cloud Armor via Compute Engine `securityPolicies` REST. Policies store rules
with official `SRC_IPS_V1` match shapes plus a lab `byteMatchSet` theatre for
URI path / single-header CONTAINS or EXACTLY evaluation. `:validate` previews
allow/deny for a sample request. No real edge enforcement or backend association.

## Status

**lab** — securityPolicies insert/get/list/delete; `addRule` / `removeRule`;
`:validate` ByteMatchSet + default-rule eval.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`), Compute-shaped:

| Method | Path |
|--------|------|
| `POST` | `/compute/v1/projects/{p}/global/securityPolicies` |
| `GET` | `/compute/v1/projects/{p}/global/securityPolicies` |
| `GET` | `/compute/v1/projects/{p}/global/securityPolicies/{policy}` |
| `DELETE` | `/compute/v1/projects/{p}/global/securityPolicies/{policy}` |
| `POST` | `/compute/v1/projects/{p}/global/securityPolicies/{policy}/addRule` |
| `POST` | `/compute/v1/projects/{p}/global/securityPolicies/{policy}/removeRule?priority=` |
| `POST` | `/compute/v1/projects/{p}/global/securityPolicies/{policy}:validate` |

Insert/delete/`addRule`/`removeRule` return a DONE `compute#operation`. Create
seeds the required default rule (priority `2147483647`, action `allow`,
`srcIpRanges: ["*"]`) when omitted.

Lab `match.byteMatchSet` fields used by `:validate`:

| Field | Values |
|-------|--------|
| `fieldToMatch` | `UriPath` or `SingleHeader` (with `headerName`) |
| `positionalConstraint` | `CONTAINS` or `EXACTLY` |
| `searchString` | needle |

Sample `:validate` body: `uriPath`, optional `headers` map, optional `srcIp`.
Rules evaluate lowest priority number first; `preview: true` rules are skipped
(not enforced). No match after all rules is fail-closed deny.

Colon methods use path-segment `splitColonAction` (ServeMux cannot embed `:`).

## Authz

Checked on `projects/{project}`:

- `compute.securityPolicies.create|get|list|delete|update`

Seeded Service Usage: `compute.googleapis.com` (Armor is the Compute API).

## Emulator limits

- No backend service / target proxy attachment
- No preconfigured WAF / Adaptive Protection / rate-limit enforcement
- `SRC_IPS_V1` matches `*` or exact IP string only (no CIDR parse)
- CEL `match.expr` is stored but not evaluated
- Private keys N/A; no DDoS dataplane

## Deferred depth

- `patch` / `patchRule` / `getRule` / `aggregatedList`
- Regional security policies
- `listPreconfiguredExpressionSets` catalogue

`setLabels` returns a DONE compute operation and persists labels on the policy
JSON (hashicorp/google still POSTs setLabels after create even with
`add_terraform_attribution_label = false`).

## Verification / CLI smoke

```bash
go test ./internal/services/cloudarmor/ ./internal/store/ ./internal/server/ -run 'Armor|SecurityPolicy|CloudArmor' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/compute/v1/projects/noctaxris-gcp-local/global/securityPolicies" \
  -d '{"name":"lab-armor"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/compute/v1/projects/noctaxris-gcp-local/global/securityPolicies/lab-armor/addRule" \
  -d '{"priority":1000,"action":"deny(403)","match":{"byteMatchSet":{"fieldToMatch":"UriPath","positionalConstraint":"CONTAINS","searchString":"/admin"}}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/compute/v1/projects/noctaxris-gcp-local/global/securityPolicies/lab-armor:validate" \
  -d '{"uriPath":"/admin"}'
```

```bash
gcloud config set api_endpoint_overrides/compute http://127.0.0.1:4588/
```
