# Vertex AI

Lab Vertex AI publisher-model predict and generateContent. Allowlisted model ids
only; responses are canned JSON. No real models or GPU backends.

## Status

**lab** — `predict` and `generateContent` on `publishers/google/models/{model}`;
unknown `modelId` fails closed (404).

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`).

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/{p}/locations/{loc}/publishers/google/models/{model}:predict` |
| `POST` | `/v1/projects/{p}/locations/{loc}/publishers/google/models/{model}:generateContent` |

Colon methods are parsed from the `{model}` path segment (Go ServeMux forbids
`{id}:action` patterns).

### Allowlisted model ids

| Model id |
|----------|
| `gemini-1.5-flash` |
| `gemini-1.5-pro` |
| `gemini-2.0-flash` |
| `text-embedding-004` |
| `text-bison` |
| `text-bison@001` |

Publisher must be `google`. Any other publisher or model id returns 404.

## Authz

Checked on `projects/{project}`:

- `aiplatform.endpoints.predict` (both `:predict` and `:generateContent`)

Seeded Service Usage: `aiplatform.googleapis.com`.

## Emulator limits

- Canned JSON only; request `instances` / `contents` are accepted and ignored
- No tuned endpoints, embeddings fidelity, streaming, or tool-calling
- No real Gemini / PaLM inference

## Deferred depth

- Streaming generateContent / server-sent events
- Endpoint deploy + online prediction beyond publisher models
- Embedding vector shape fidelity for `text-embedding-004`

## Verification / CLI smoke

```bash
go test ./internal/services/vertexai/ ./internal/server/ -run VertexAI -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/publishers/google/models/gemini-1.5-flash:generateContent" \
  -d '{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}'
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/locations/us-central1/publishers/google/models/text-bison:predict" \
  -d '{"instances":[{"content":"hi"}]}'
```

```bash
gcloud config set api_endpoint_overrides/aiplatform http://127.0.0.1:4588/
```
