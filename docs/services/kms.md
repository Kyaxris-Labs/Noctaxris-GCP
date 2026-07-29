# Cloud KMS

Lab-complete Cloud KMS v1 REST for symmetric encrypt/decrypt and SOFTWARE RSA sign/verify.

## Status

**lab** — key rings; crypto keys (`ENCRYPT_DECRYPT`, `ASYMMETRIC_SIGN` with `RSA_SIGN_PSS_2048_SHA256`); version list/get; encrypt/decrypt (AES-GCM); asymmetricSign + GetPublicKey; UpdateCryptoKey (labels); destroy/restore version; cryptoKey getIamPolicy/setIamPolicy.

## Location

Lab default location is **`global`**. `us-central1` also works as a location string; create resources under the location you pass in the path.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

Colon method suffixes (`:encrypt`, `:decrypt`, `:destroy`, `:restore`, `:asymmetricSign`, `:getIamPolicy`, `:setIamPolicy`) are parsed from the trailing path segment because ServeMux wildcards cannot embed `:` inside a segment.

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/keyRings?keyRingId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/keyRings` |
| `GET` | `/v1/projects/{p}/locations/{loc}/keyRings/{ring}` |
| `POST` | `/v1/projects/{p}/locations/{loc}/keyRings/{ring}/cryptoKeys?cryptoKeyId=` |
| `GET` | `/v1/projects/{p}/locations/{loc}/keyRings/{ring}/cryptoKeys` |
| `GET` | `/v1/projects/{p}/locations/{loc}/keyRings/{ring}/cryptoKeys/{key}` |
| `PATCH` | `.../cryptoKeys/{key}?updateMask=labels` |
| `POST` | `.../cryptoKeys/{key}:getIamPolicy` / `:setIamPolicy` |
| `GET` | `.../cryptoKeys/{key}/cryptoKeyVersions` |
| `GET` | `.../cryptoKeyVersions/{n}` |
| `GET` | `.../cryptoKeyVersions/{n}/publicKey` |
| `POST` | `.../cryptoKeys/{key}:encrypt` / `:decrypt` |
| `POST` | `.../cryptoKeyVersions/{n}:asymmetricSign` |
| `POST` | `.../cryptoKeyVersions/{n}:destroy` / `:restore` |
| `POST` | `.../cryptoKeyVersions/{n}:encrypt` / `:decrypt` |

### Symmetric (`ENCRYPT_DECRYPT`)

Request bodies use base64 `plaintext` / `ciphertext`. Key material is AES-256-GCM, sealed at rest with the store master key.

`GetPublicKey` on symmetric keys returns `FAILED_PRECONDITION` (no public key).

### Asymmetric (`ASYMMETRIC_SIGN`)

Create with:

```json
{
  "purpose": "ASYMMETRIC_SIGN",
  "versionTemplate": {
    "algorithm": "RSA_SIGN_PSS_2048_SHA256",
    "protectionLevel": "SOFTWARE"
  }
}
```

`asymmetricSign` accepts `digest.sha256` (base64, 32 bytes) or base64 `data` (hashed with SHA-256). Signature is RSA-PSS with salt length equal to hash. Clients verify locally with `GetPublicKey` PEM.

### UpdateCryptoKey / IAM

`PATCH` updates `labels` when `updateMask` is empty or contains `labels`. Per-cryptoKey IAM uses the shared `iam_policies` table keyed by the cryptoKey resource name.

Destroying a version sets state `DESTROYED`. Later crypto ops return `FAILED_PRECONDITION`. Lab `:restore` returns the version to `ENABLED`.

## Authz

Checked on `projects/{project}`:

- `cloudkms.keyRings.create|get|list`
- `cloudkms.cryptoKeys.create|get|list|update|getIamPolicy|setIamPolicy`
- `cloudkms.cryptoKeyVersions.get|list|useToEncrypt|useToDecrypt|useToSign|viewPublicKey|destroy|restore`

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

## Emulator limits

- SOFTWARE protection only; symmetric key material is AES-256-GCM sealed with the process master key at rest
- Secret Manager and other services may store a `kmsKeyName` for CMEK theatre; those names are not enforced through this KMS encrypt/decrypt path (payloads still use the lab master key)

## Deferred depth

- `ASYMMETRIC_DECRYPT`, MAC keys, HSM protection levels, import jobs, automatic rotation
- gRPC `KeyManagementService` (REST is the lab path; protos not wired)

## Verification / CLI smoke

```bash
go test ./internal/services/kms/ ./internal/server/ -run KMS -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/keyRings?keyRingId=demo" \
  -d '{}'
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"purpose":"ENCRYPT_DECRYPT"}' \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/keyRings/demo/cryptoKeys?cryptoKeyId=lab"
PLAIN=$(printf 'hello-kms' | base64 -w0 2>/dev/null || printf 'hello-kms' | base64)
CT=$(curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"plaintext\":\"$PLAIN\"}" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/keyRings/demo/cryptoKeys/lab:encrypt" | jq -r .ciphertext)
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"ciphertext\":\"$CT\"}" \
  "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/global/keyRings/demo/cryptoKeys/lab:decrypt"
```
