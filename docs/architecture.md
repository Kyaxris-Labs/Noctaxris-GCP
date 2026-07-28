# Architecture

Noctaxris-GCP is a single Go process that multiplexes Google-shaped REST and gRPC
on one loopback port.

```mermaid
flowchart TB
  subgraph clients [Clients]
    SDK[GCP SDKs / gcloud / curl]
  end

  subgraph process [noctaxris-gcp]
    H2C[h2c HTTP server]
    MW[Request ID + Bearer authn]
    REST[REST ServeMux]
    GRPC[grpc.Server]
    AUTHZ[IAM authz evaluator]
    STORE[(SQLite state.db)]
    AEAD[ChaCha20-Poly1305]
    AUDIT[audit.jsonl]
  end

  subgraph volumes [Volumes]
    DATA[noctaxris-gcp-data]
    SECRETS[noctaxris-gcp-secrets / master.key]
  end

  SDK -->|127.0.0.1:4588| H2C
  H2C --> MW
  MW -->|application/grpc| GRPC
  MW -->|other| REST
  REST --> AUTHZ
  GRPC --> AUTHZ
  AUTHZ --> STORE
  STORE --> DATA
  AEAD --> SECRETS
  STORE --> AEAD
  REST --> AUDIT
  AUDIT --> DATA
```

## Kernel packages

| Package | Role |
|---------|------|
| `internal/config` | `NOCTAXRIS_GCP_*` load + loopback / TLS gate |
| `internal/kernel/authn` | Bearer extraction; root vs registered tokens |
| `internal/kernel/authz` | IAM policy Evaluate / testIamPermissions |
| `internal/kernel/audit` | JSONL audit writer |
| `internal/store` | SQLite schema, Seal/Unseal, EnsureRoot |
| `internal/gcperrors` | Google REST JSON errors + gRPC status helpers |
| `internal/server` | h2c routing, health, middleware, service registration |

## Service registration

`server.New` wires registration helpers after health routes:

| Helper | Surface |
|--------|---------|
| `registerIdentity` | Cloud Resource Manager, IAM Admin, Service Usage (REST); creates gRPC server + Bearer interceptors |
| `registerData` | Cloud Storage (REST), Pub/Sub (gRPC), Secret Manager (REST + gRPC) |
| `registerDocsCrypto` | Firestore (gRPC), Cloud KMS (REST), Cloud Logging (REST) |
| `registerExpandCompute` | Cloud Run, Cloud Functions, Cloud Scheduler, Cloud Tasks (REST) |
| `registerExpandAnalytics` | BigQuery (REST), Firebase Auth / Identity Toolkit (REST), Cloud Monitoring (REST), Datastore (gRPC), Eventarc (REST) |

## Request path

1. Assign `X-Request-Id` when missing.
2. Skip auth for health / ready / version.
3. For REST: require Bearer; attach principal to context.
4. For gRPC: Bearer interceptor attaches principal; handlers re-check IAM.
5. Handlers call authz with `(principal, permission, resource)`.
6. Persist through the store; seal sensitive columns with the master key.
