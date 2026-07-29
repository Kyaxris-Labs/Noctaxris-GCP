# Cloud CDN

Lab CDN distributions CRUD and a public edge path on `:4588` that caches theatre
headers while reading from lab GCS or an HTTP(S) LB forwarding rule.

## Status

**lab** — distributions CRUD; edge `GET /cdn/{distributionId}/{objectPath...}`.

## Wire protocol

Control plane (Bearer required):

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/global/distributions?distributionId={id}` |
| `GET` | `/v1/projects/{p}/global/distributions` |
| `GET` | `/v1/projects/{p}/global/distributions/{distribution}` |
| `DELETE` | `/v1/projects/{p}/global/distributions/{distribution}` |

Edge (public on loopback):

| Method | Path |
|--------|------|
| `GET` / `HEAD` | `/cdn/{distributionId}/{objectPath...}` |

Origin examples:

```json
{"origin": {"gcs": {"bucket": "edge-bucket", "objectPrefix": "assets"}}}
```

```json
{"origin": {"lb": {"project": "noctaxris-gcp-local", "forwardingRule": "lab-fr"}}}
```

## Authz

Uses lab CDN permissions mapped to compute backend bucket verbs on `projects/{project}`:

- `compute.backendBuckets.create|get|list|delete`

## Emulator limits

- No cache invalidation API; `Cache-Control: public, max-age=3600` on edge responses only
- No geographic PoPs; single-process edge on the API listener

## Deferred depth

- Signed URLs, cache modes, and backend bucket Compute resources
- Negative caching and origin shield

## Verification / CLI smoke

```bash
go test ./internal/services/cdn/ ./internal/server/ -run 'CDN|GKEEdge' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/global/distributions?distributionId=lab-cdn" \
  -d '{"origin":{"gcs":{"bucket":"edge-bucket","objectPrefix":"assets"}}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/global/distributions"
curl -s "http://127.0.0.1:4588/cdn/lab-cdn/app.js"
```

Edge is the shared API listener only (`http://127.0.0.1:4588/cdn/...`); no separate host port.
