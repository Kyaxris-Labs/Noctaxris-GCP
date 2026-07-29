# Access Context Manager (VPC Service Controls lite)

Lab Access Context Manager REST for access policies and service perimeters.
Optional enforce (`NOCTAXRIS_GCP_VPCSC_ENFORCE=1`) denies cross-perimeter GCS
object upload/copy and Pub/Sub publish (including GCS notification fanout) when
a perimeter restricts `storage.googleapis.com` / `pubsub.googleapis.com`.

## Status

**lab** — accessPolicies + servicePerimeters CRUD theatre; optional dry-run
enforce via env. Not a full VPC-SC dataplane.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`):

| Method | Path |
|--------|------|
| `POST` | `/v1/accessPolicies` (`?policyId=` optional; body `parent`, `title`, `scopes`) |
| `GET` | `/v1/accessPolicies` (`?parent=organizations/...`) |
| `GET` | `/v1/accessPolicies/{policy}` |
| `PATCH` | `/v1/accessPolicies/{policy}?updateMask=` |
| `DELETE` | `/v1/accessPolicies/{policy}` |
| `POST` | `/v1/accessPolicies/{policy}/servicePerimeters?servicePerimeterId=` |
| `GET` | `/v1/accessPolicies/{policy}/servicePerimeters` |
| `GET` | `/v1/accessPolicies/{policy}/servicePerimeters/{perimeter}` |
| `PATCH` | `/v1/accessPolicies/{policy}/servicePerimeters/{perimeter}?updateMask=` |
| `DELETE` | `/v1/accessPolicies/{policy}/servicePerimeters/{perimeter}` |

Mutating calls return a completed Operation (`done: true` + `response`).

Perimeter body fields used by the lab:

| Field | Role |
|-------|------|
| `status` | Enforced config (`resources`, `restrictedServices`) |
| `spec` | Dry-run config |
| `useExplicitDryRunSpec` | When true and `status` empty, `spec` is the dry-run config |

Lab `resources` accept `projects/{projectId}` (project id, not only number).

## Optional enforce

Set `NOCTAXRIS_GCP_VPCSC_ENFORCE=1` (or `true`). Then:

- Cross-project GCS copy and SA-principal upload to another project deny when a
  perimeter covers one side only and lists `storage.googleapis.com`
- Pub/Sub publish denies when the caller SA project and topic project sit across
  such a perimeter for `pubsub.googleapis.com`
- GCS `notificationConfigs` fanout skips publish when bucket and topic projects
  cross a restricting perimeter
- Dry-run-only perimeters (`spec` + `useExplicitDryRunSpec`, empty `status`)
  participate only when enforce is on (optional dry-run enforce)

Default (env unset): CRUD theatre only; no deny.

## Authz

Checked on the policy `parent` (default `organizations/noctaxris-gcp-org`):

- `accesscontextmanager.policies.create|get|list|update|delete`
- `accesscontextmanager.servicePerimeters.create|get|list|update|delete`

`roles/owner` / `roles/editor` grant these; `roles/accesscontextmanager.*` is on
the lab predefined prefix allowlist.

Enable API via Service Usage: `accesscontextmanager.googleapis.com` (not
auto-seeded; enable when gating creates).

## Emulator limits

- No access levels, ingress/egress policies evaluation, or bridge perimeters
- No network / VPC context; enforce is project-membership lite only
- Create/patch/delete return immediate done Operations (no long-running worker)
- No `servicePerimeters:commit` dry-run commit RPC

## Deferred depth

- Access levels CRUD and member/device conditions
- Ingress / egress policy evaluation
- Bridge perimeters and perimeter dry-run commit
- Seed `accesscontextmanager.googleapis.com` in EnsureRoot

## Verification / CLI smoke

```bash
go test ./internal/services/accesscontextmanager/ ./internal/store/ -run 'AccessPolicy|ACM|VPCSC|Perimeter' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/accessPolicies?policyId=lab" \
  -d '{"parent":"organizations/noctaxris-gcp-org","title":"Lab"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/accessPolicies/lab/servicePerimeters?servicePerimeterId=p1" \
  -d '{"title":"p1","status":{"resources":["projects/noctaxris-gcp-local"],"restrictedServices":["storage.googleapis.com","pubsub.googleapis.com"]}}'
```
