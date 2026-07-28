# Architecture

Noctaxris-GCP is a single Go process that multiplexes Google-shaped REST and gRPC
on one loopback port.

```mermaid
flowchart TB
  subgraph clients [Clients]
    SDK[GCP SDKs / gcloud / curl]
  end

  subgraph process [noctaxris-gcp]
    H2C[h2c listener :4588]
    MW[Request ID + Bearer authn]
    REST[REST ServeMux]
    GRPC[grpc.Server]
    AUTHZ[IAM authz evaluator]
    STORE[(SQLite state.db)]
    AEAD[ChaCha20-Poly1305]
    AUDIT[audit.jsonl]

    subgraph services [Registered services]
      ID[CRM / IAM / Service Usage]
      DATA[GCS / Pub/Sub / Secret Manager]
      DOC[Firestore / KMS / Logging]
      CMP[Run / Functions / Scheduler / Tasks]
      AN[BQ / Firebase Auth / Monitoring / Datastore / Eventarc]
      APPS[Artifact Registry / Cloud Build / Workflows / Spanner / App Engine]
      CDATA[Compute Engine / Bigtable / Memorystore / DNS / Dataflow]
    end
  end

  subgraph volumes [Volumes]
    DATAVOL[noctaxris-gcp-data]
    SECRETS[noctaxris-gcp-secrets / master.key]
  end

  SDK -->|127.0.0.1:4588| H2C
  H2C --> MW
  MW -->|application/grpc| GRPC
  MW -->|other| REST
  REST --> ID
  REST --> DATA
  REST --> DOC
  REST --> CMP
  REST --> AN
  REST --> APPS
  REST --> CDATA
  GRPC --> DATA
  GRPC --> DOC
  GRPC --> AN
  ID --> AUTHZ
  DATA --> AUTHZ
  DOC --> AUTHZ
  CMP --> AUTHZ
  AN --> AUTHZ
  APPS --> AUTHZ
  CDATA --> AUTHZ
  AUTHZ --> STORE
  STORE --> DATAVOL
  AEAD --> SECRETS
  STORE --> AEAD
  REST --> AUDIT
  AUDIT --> DATAVOL
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

`server.New` wires registration helpers after health routes, in this order:

| Helper | Surface |
|--------|---------|
| `registerIdentity` | Cloud Resource Manager (projects, org seed, folders), IAM Admin, Service Usage (REST); creates gRPC server + Bearer interceptors |
| `registerData` | Cloud Storage (REST), Pub/Sub (gRPC + REST), Secret Manager (REST + gRPC) |
| `registerDocsCrypto` | Firestore (gRPC), Cloud KMS (REST), Cloud Logging (REST) |
| `registerServerless` | Cloud Run, Cloud Functions, Cloud Scheduler, Cloud Tasks (REST) |
| `registerAnalytics` | BigQuery (REST), Firebase Auth / Identity Toolkit (REST), Cloud Monitoring (REST), Datastore (gRPC), Eventarc (REST) |
| `registerAppsBuild` | Artifact Registry, Cloud Build, Workflows, Cloud Spanner, App Engine (REST) |
| `registerComputeData` | Compute Engine (incl. VPC/firewall), Bigtable Admin, Memorystore Redis, Cloud DNS, Dataflow (REST) |

## Request path

1. Assign `X-Request-Id` when missing.
2. Skip auth for health / ready / version.
3. For REST: require Bearer; attach principal to context.
4. For gRPC: Bearer interceptor attaches principal; handlers re-check IAM.
5. Handlers call authz with `(principal, permission, resource)`.
6. Persist through the store; seal sensitive columns with the master key.
