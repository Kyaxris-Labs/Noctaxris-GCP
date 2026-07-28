# Cloud KMS

Lab-complete Cloud KMS v1 REST for symmetric encrypt/decrypt keys.

## Status

**lab** — key rings, crypto keys (`ENCRYPT_DECRYPT`), version list/get, encrypt/decrypt (SOFTWARE AES-GCM), destroy and restore version.

## Location

Lab default location is **`global`**. `us-central1` also works as a location string; create resources under the location you pass in the path.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

Colon method suffixes (`:encrypt`, `:decrypt`, `:destroy`, `:restore`) are parsed from the trailing path segment because ServeMux wildcards cannot embed `:` inside a segment.

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/keyRings?keyRingId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/keyRings` |
| `GET` | `/v1/projects/{p}/locations/{loc}/keyRings/{ring}` |
| `POST` | `/v1/projects/{p}/locations/{loc}/keyRings/{ring}/cryptoKeys?cryptoKeyId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/keyRings/{ring}/cryptoKeys` |
| `GET` | `/v1/projects/{p}/locations/{loc}/keyRings/{ring}/cryptoKeys/{key}` |
| `GET` | `.../cryptoKeys/{key}/cryptoKeyVersions` |
| `GET` | `.../cryptoKeyVersions/{n}` |
| `POST` | `.../cryptoKeys/{key}:encrypt` |
| `POST` | `.../cryptoKeys/{key}:decrypt` |
| `POST` | `.../cryptoKeyVersions/{n}:destroy` |
| `POST` | `.../cryptoKeyVersions/{n}:restore` |
| `POST` | `.../cryptoKeyVersions/{n}:encrypt` / `:decrypt` |

Encrypt/decrypt request bodies use base64 `plaintext` / `ciphertext` (Google JSON API shape).

Key material is AES-256-GCM. Raw key bytes are sealed at rest with the store master key (`Seal`).

Destroying a version sets state `DESTROYED`. `GetCryptoKey.primary.state` reflects that. Later encrypt/decrypt return `FAILED_PRECONDITION`. Lab `:restore` returns the version to `ENABLED`.

## Authz

Checked on `projects/{project}`:

- `cloudkms.keyRings.create|get|list`
- `cloudkms.cryptoKeys.create|get|list`
- `cloudkms.cryptoKeyVersions.get|list|useToEncrypt|useToDecrypt|destroy|restore`

## Client configuration

No official `*_EMULATOR_HOST` for KMS. Point the SDK with `WithEndpoint`:

```go
option.WithEndpoint("127.0.0.1:4588"),
option.WithoutAuthentication(), // still send Authorization: Bearer in practice for this emulator
option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
```

For REST clients, use base URL `http://127.0.0.1:4588/` and Bearer auth.

gcloud:

```bash
gcloud config set api_endpoint_overrides/cloudkms http://127.0.0.1:4588/
```

## Deferred depth

- Asymmetric keys, MAC, raw encrypt variants
- Import jobs, automatic rotation, IAM on key resources
- gRPC `KeyManagementService` (REST is the lab path; protos not wired)

## Verification / CLI smoke

```bash
go test ./internal/services/kms/ ./internal/server/ -run KMS -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/keyRings?keyRingId=demo" \
  -d '{}'
```
