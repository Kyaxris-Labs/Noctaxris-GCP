# HTTP(S) load balancing

Lab global HTTP(S) load balancing metadata (backend services, URL maps, target
HTTPS proxies, forwarding rules) plus a loopback dataplane on `:4588`.

## Status

**lab** — Compute v1 global CRUD for `backendServices`, `urlMaps`,
`targetHttpsProxies`, `forwardingRules`; invoke `GET /lb/{project}/{forwardingRule}/{objectPath}` to read lab GCS objects.

## Wire protocol

Control plane (Bearer required):

| Method | Path |
|--------|------|
| `POST` / `GET` / `PATCH` / `DELETE` | `/compute/v1/projects/{p}/global/backendServices[/{name}]` |
| `POST` | `/compute/v1/projects/{p}/global/backendServices/{name}/setSecurityPolicy` |
| `POST` / `GET` / `DELETE` | `/compute/v1/projects/{p}/global/urlMaps[/{name}]` |
| `POST` / `GET` / `PATCH` / `DELETE` | `/compute/v1/projects/{p}/global/targetHttpsProxies[/{name}]` |
| `POST` / `GET` / `DELETE` | `/compute/v1/projects/{p}/global/forwardingRules[/{name}]` |

Dataplane (public on loopback; no Bearer):

| Method | Path |
|--------|------|
| `GET` / `HEAD` | `/lb/{project}/{forwardingRuleName}/{objectPath...}` |

Backends use a lab extension on `backends[]`:

```json
{"gcsBucket": "my-bucket", "objectPrefix": "static"}
```

Chain: forwarding rule `target` → URL map or target HTTPS proxy `urlMap` →
`defaultService` → backend service → GCS object. `securityPolicy` on backend
services and target HTTPS proxies echoes the Cloud Armor policy self link (lab
metadata only). Backend insert/delete and `setSecurityPolicy` return DONE
`compute#operation` bodies; GET uses full compute `selfLink` URLs.

## Authz

Control plane permissions on `projects/{project}`:

- `compute.backendServices.*`, `compute.urlMaps.*`, `compute.targetHttpsProxies.*`, `compute.forwardingRules.*`

## Emulator limits

- Global scope only; no health checks, SSL cert provisioning, or regional L7 proxies
- Dataplane serves lab GCS bytes only (no Internet origin fetch); Armor attach is metadata only

## Deferred depth

- Target pools, instance groups
- HTTPS terminate and certificate manager wire-up

## Verification / CLI smoke

```bash
go test ./internal/services/loadbalancing/ ./internal/server/ -run 'LoadBalancing|GKEEdge|Backend|Proxy' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
# Assume bucket noctaxris-gcp-local object static/hello.txt already exists
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/compute/v1/projects/noctaxris-gcp-local/global/backendServices" \
  -d '{"name":"lab-bs","backends":[{"gcsBucket":"cdn-bucket","objectPrefix":"static"}],"securityPolicy":"projects/noctaxris-gcp-local/global/securityPolicies/lab-armor"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/compute/v1/projects/noctaxris-gcp-local/global/backendServices/lab-bs/setSecurityPolicy" \
  -d '{"securityPolicy":"projects/noctaxris-gcp-local/global/securityPolicies/lab-armor"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/compute/v1/projects/noctaxris-gcp-local/global/urlMaps" \
  -d '{"name":"lab-map","defaultService":"projects/noctaxris-gcp-local/global/backendServices/lab-bs"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/compute/v1/projects/noctaxris-gcp-local/global/forwardingRules" \
  -d '{"name":"lab-fr","target":"projects/noctaxris-gcp-local/global/urlMaps/lab-map"}'
curl -s "http://127.0.0.1:4588/lb/noctaxris-gcp-local/lab-fr/hello.txt"
```

Terraform Armor attach (policy + backend `security_policy` only; no HTTPS proxy / Certificate Manager):

```bash
STACK=lab-lb-armor bash tests/terraform/run.sh   # parity; not in default STACKS
# or: TF_GCP_PARITY=1 bash tests/run-all.sh
```

Dataplane is the shared API listener only (`http://127.0.0.1:4588/lb/...`); no separate host port.
