# Cloud DNS

Lab Cloud DNS REST for managed zones, resource record sets, and Changes
theatre (Terraform `google_dns_record_set`). Zones store `dnsName` and
`visibility`; records store `name` / `type` / `ttl` / `rrdatas`.
There is no authoritative DNS server and no query answering.

## Status

**lab** — managedZones create/get/list/delete; rrsets create/get/list/delete;
Changes create/get/list applies additions/deletions to stored rrsets and returns
`status: "done"`. Zone create seeds NS + SOA records (metadata only). Change
history is in-process only (cleared on restart).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/dns/v1/projects/{p}/managedZones` |
| `GET` | `/dns/v1/projects/{p}/managedZones` |
| `GET` | `/dns/v1/projects/{p}/managedZones/{zone}` |
| `DELETE` | `/dns/v1/projects/{p}/managedZones/{zone}` |
| `POST` | `/dns/v1/projects/{p}/managedZones/{zone}/rrsets` |
| `GET` | `/dns/v1/projects/{p}/managedZones/{zone}/rrsets` (`name`, `type` filters optional) |
| `GET` | `/dns/v1/projects/{p}/managedZones/{zone}/rrsets/{name}/{type}` |
| `DELETE` | `/dns/v1/projects/{p}/managedZones/{zone}/rrsets/{name}/{type}` |
| `POST` | `/dns/v1/projects/{p}/managedZones/{zone}/changes` |
| `GET` | `/dns/v1/projects/{p}/managedZones/{zone}/changes` |
| `GET` | `/dns/v1/projects/{p}/managedZones/{zone}/changes/{changeId}` |

Create zone body fields used: `name`, `dnsName`, `description`, `visibility`
(`public` or `private`). Create rrset body: `name`, `type`, `ttl`, `rrdatas`.
Change body: `additions` / `deletions` arrays of rrset objects. Lab applies
deletions then additions (not a SQLite transaction); missing deletions are
ignored. Response includes `id`, `status: "done"`, `startTime`, and the change
contents.

## Authz

Checked on `projects/{project}`:

- `dns.managedZones.create|get|list|delete`
- `dns.resourceRecordSets.create|get|list|delete`
- `dns.changes.create|get|list`

Seeded Service Usage: `dns.googleapis.com`.

## Emulator limits

- No DNS query plane or DNSSEC signing
- Change history is memory-only (not durable across restart)
- No privateVisibilityConfig / forwarding / peering / Service Directory wiring
- Delete zone also deletes stored rrsets
- Name servers are lab strings (`ns-cloud-a*.noctaxris-gcp.lab.`)

## Deferred depth

- Durable change history, strict deletion matching, patch/update zone, IAM on zones
- Routing policies, health-checked targets, DNSSEC keys

## Verification / CLI smoke

```bash
go test ./internal/services/dns/ ./internal/store/ ./internal/server/ -run DNS -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/dns/v1/projects/noctaxris-gcp-local/managedZones" \
  -d '{"name":"example-zone","dnsName":"example.com.","visibility":"public"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/dns/v1/projects/noctaxris-gcp-local/managedZones/example-zone/changes" \
  -d '{"additions":[{"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["1.2.3.4"]}],"deletions":[]}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/dns/v1/projects/noctaxris-gcp-local/managedZones/example-zone/rrsets"
```

```bash
gcloud config set api_endpoint_overrides/dns http://127.0.0.1:4588/
STACK=lab-dns bash tests/terraform/run.sh
# dns_custom_endpoint = "http://127.0.0.1:4588/dns/v1/"
# lab-dns includes google_dns_record_set (Changes.create); soft-skips without endpoint/token/ready.
```

```bash
go test ./tests/sdk/go/ -run TestDNSChangesRrsetSmoke -count=1
```
