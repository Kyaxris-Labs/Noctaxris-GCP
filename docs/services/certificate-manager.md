# Certificate Manager

Lab Certificate Manager REST for certificates and certificate maps. Stores
metadata (managed domains / self-managed flags); PEM private keys are not
returned. No CA issuance, DNS authorization, or load-balancer attachment.

## Status

**lab** — certificates and certificateMaps create/get/list/delete under
`/v1/projects/{p}/locations/{loc}/...`. Location `global` is supported.
Create returns a completed Operation (`done: true` + `response`).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`):

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/certificates?certificateId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/certificates` |
| `GET` | `/v1/projects/{p}/locations/{loc}/certificates/{id}` |
| `DELETE` | `/v1/projects/{p}/locations/{loc}/certificates/{id}` |
| `POST` | `/v1/projects/{p}/locations/{loc}/certificateMaps?certificateMapId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/certificateMaps` |
| `GET` | `/v1/projects/{p}/locations/{loc}/certificateMaps/{id}` |
| `DELETE` | `/v1/projects/{p}/locations/{loc}/certificateMaps/{id}` |
| `GET` | `/v1/projects/{p}/locations/{loc}/operations/{operation}` |

Create certificate body fields used: `description`, `labels`, `scope`,
`managed` (domains; lab sets `state=ACTIVE`), or `selfManaged` (PEM stored as
redacted theatre flags only). Create map body: `description`, `labels`.

Create certificate / certificateMap returns a completed Operation:

```json
{"name":"projects/.../locations/.../operations/create-{id}","done":true,"response":{"@type":"...","name":"projects/.../certificates/{id}",...}}
```

`GET` of the certificate or map by resource name still returns the resource.
`GET .../operations/{operation}` returns `{name, done: true}` so provider poll
paths succeed immediately. Colon methods use `splitColonAction` (none mounted).

## Authz

Checked on `projects/{project}`:

- `certificatemanager.certs.create|get|list|delete`
- `certificatemanager.certmaps.create|get|list|delete`
- `certificatemanager.operations.get`

Seeded Service Usage: `certificatemanager.googleapis.com`.

## Emulator limits

- No real certificate issuance, renewal, or DNS authorization
- No certificate map entries / GCLB target wiring
- Self-managed PEM private key material is not persisted in cleartext responses
- Create Operation is completed immediately (no async worker / poll queue)

## Deferred depth

- `patch` on certificates / maps; certificateMapEntries CRUD
- DnsAuthorizations / CertificateIssuanceConfigs

## Verification / CLI smoke

```bash
go test ./internal/services/certificatemanager/ ./internal/store/ ./internal/server/ -run 'Cert|Certificate' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/certificates?certificateId=lab-cert" \
  -d '{"description":"lab","managed":{"domains":["example.com"]}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/certificateMaps?certificateMapId=lab-map" \
  -d '{"description":"lab map"}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/certificates"
```

```bash
gcloud config set api_endpoint_overrides/certificatemanager http://127.0.0.1:4588/
```
