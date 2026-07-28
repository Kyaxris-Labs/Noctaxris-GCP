# Changelog

## [Unreleased]

### Added

### Changed

### Fixed

## [0.2.0] - 2026-07-28

### Added

- Cloud Run Admin API v2 lab: services CRUD, revision metadata, in-process `:invoke` mock (no container / no host `docker.sock`)
- Cloud Functions v2 lab: control-plane CRUD, ACTIVE theatre, optional HTTP `:invoke` stub
- Cloud Scheduler v1 lab: jobs CRUD, cron storage, best-effort HTTP and Pub/Sub fire
- Cloud Tasks v2 lab: queues/tasks CRUD, scheduleTime storage, best-effort HTTP dispatch / `:run`
- BigQuery lab: datasets/tables CRUD, `insertAll`, limited `jobs.query` (SELECT/WHERE/LIMIT on inserted rows)
- Firebase Auth lab: Identity Toolkit email/password and admin user CRUD; unsigned lab custom tokens
- Cloud Monitoring lab: metric descriptors and time series write/list with basic aligners
- Cloud Datastore lab: gRPC Lookup / Commit / equality RunQuery (`DATASTORE_EMULATOR_HOST`)
- Eventarc lab: triggers for Pub/Sub publish and GCS finalize with best-effort HTTP / Cloud Run delivery
- `registerExpandCompute` and `registerExpandAnalytics` wiring from `server.New`

### Changed

- Deepened existing identity, data, secrets, Firestore, KMS, and Logging lab surfaces (IAM enable/disable and SA IAM; CRM project patch; Service Usage batchEnable; GCS bucket IAM / compose / copy / metadata; Pub/Sub REST mirror and push theatre; Secret Manager patch and per-secret IAM; Firestore field masks and richer filters; KMS version list/get/restore; Logging list/delete logs)
- Docs, README Services table, architecture registration table, and client endpoint override lists cover the full lab matrix
