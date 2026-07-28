package store

const schema = `
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS iam_policies (
  resource TEXT PRIMARY KEY,
  etag TEXT NOT NULL,
  policy_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS service_accounts (
  project_id TEXT NOT NULL,
  email TEXT PRIMARY KEY,
  unique_id TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  disabled INTEGER NOT NULL DEFAULT 0,
  deleted_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sa_keys (
  name TEXT PRIMARY KEY,
  sa_email TEXT NOT NULL,
  key_algorithm TEXT NOT NULL DEFAULT 'KEY_ALG_RSA_2048',
  private_key_type TEXT NOT NULL DEFAULT 'TYPE_GOOGLE_CREDENTIALS_FILE',
  private_key_data BLOB NOT NULL,
  valid_after_time TEXT NOT NULL,
  valid_before_time TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS access_tokens (
  token_hash TEXT PRIMARY KEY,
  principal_email TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS service_usage (
  project_id TEXT NOT NULL,
  service_name TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'ENABLED',
  PRIMARY KEY (project_id, service_name)
);

CREATE TABLE IF NOT EXISTS buckets (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL DEFAULT 'US',
  storage_class TEXT NOT NULL DEFAULT 'STANDARD',
  labels_json TEXT NOT NULL DEFAULT '{}',
  metageneration INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS objects (
  bucket TEXT NOT NULL,
  name TEXT NOT NULL,
  generation INTEGER NOT NULL DEFAULT 1,
  size INTEGER NOT NULL DEFAULT 0,
  content_type TEXT NOT NULL DEFAULT 'application/octet-stream',
  blob_path TEXT NOT NULL,
  md5_hash TEXT NOT NULL DEFAULT '',
  crc32c TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  cache_control TEXT NOT NULL DEFAULT '',
  content_disposition TEXT NOT NULL DEFAULT '',
  content_encoding TEXT NOT NULL DEFAULT '',
  content_language TEXT NOT NULL DEFAULT '',
  metageneration INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (bucket, name, generation)
);

CREATE TABLE IF NOT EXISTS pubsub_topics (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  labels_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pubsub_subscriptions (
  name TEXT PRIMARY KEY,
  topic TEXT NOT NULL,
  project_id TEXT NOT NULL,
  ack_deadline_seconds INTEGER NOT NULL DEFAULT 10,
  push_endpoint TEXT NOT NULL DEFAULT '',
  labels_json TEXT NOT NULL DEFAULT '{}',
  filter TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pubsub_messages (
  id TEXT NOT NULL,
  subscription TEXT NOT NULL,
  topic TEXT NOT NULL,
  data BLOB NOT NULL,
  attributes_json TEXT NOT NULL DEFAULT '{}',
  publish_time TEXT NOT NULL,
  ack_id TEXT PRIMARY KEY,
  ack_deadline TEXT,
  delivered INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS secrets (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  labels_json TEXT NOT NULL DEFAULT '{}',
  annotations_json TEXT NOT NULL DEFAULT '{}',
  replication_json TEXT NOT NULL DEFAULT '{}',
  cmek_kms_key_name TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gcs_upload_sessions (
  upload_id TEXT PRIMARY KEY,
  bucket TEXT NOT NULL,
  name TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT 'application/octet-stream',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS secret_versions (
  name TEXT PRIMARY KEY,
  secret_name TEXT NOT NULL,
  version_id TEXT NOT NULL,
  payload_ciphertext BLOB NOT NULL,
  state TEXT NOT NULL DEFAULT 'ENABLED',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS firestore_docs (
  path TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  collection_id TEXT NOT NULL,
  document_id TEXT NOT NULL,
  fields_json TEXT NOT NULL,
  create_time TEXT NOT NULL,
  update_time TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS firestore_transactions (
  token TEXT PRIMARY KEY,
  database TEXT NOT NULL,
  project_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS kms_keyrings (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS kms_keys (
  name TEXT PRIMARY KEY,
  keyring TEXT NOT NULL,
  purpose TEXT NOT NULL DEFAULT 'ENCRYPT_DECRYPT',
  algorithm TEXT NOT NULL DEFAULT 'GOOGLE_SYMMETRIC_ENCRYPTION',
  labels_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS log_sinks (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  sink_id TEXT NOT NULL,
  destination TEXT NOT NULL DEFAULT '',
  filter TEXT NOT NULL DEFAULT '',
  writer_identity TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (project_id, sink_id)
);

CREATE TABLE IF NOT EXISTS kms_key_versions (
  name TEXT PRIMARY KEY,
  crypto_key TEXT NOT NULL,
  version_id TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'ENABLED',
  key_material_ciphertext BLOB NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS log_entries (
  insert_id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  log_name TEXT NOT NULL,
  severity TEXT NOT NULL DEFAULT 'DEFAULT',
  timestamp TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  resource_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS run_services (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  service_id TEXT NOT NULL,
  uid TEXT NOT NULL,
  generation INTEGER NOT NULL DEFAULT 1,
  template_json TEXT NOT NULL DEFAULT '{}',
  uri TEXT NOT NULL DEFAULT '',
  latest_revision TEXT NOT NULL DEFAULT '',
  lab_response_body TEXT NOT NULL DEFAULT '',
  last_invoke_json TEXT NOT NULL DEFAULT '',
  traffic_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS run_revisions (
  name TEXT PRIMARY KEY,
  service_name TEXT NOT NULL,
  generation INTEGER NOT NULL,
  template_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS run_jobs (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  job_id TEXT NOT NULL,
  uid TEXT NOT NULL,
  generation INTEGER NOT NULL DEFAULT 1,
  template_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cloud_functions (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  function_id TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  config_json TEXT NOT NULL DEFAULT '{}',
  uri TEXT NOT NULL DEFAULT '',
  lab_response_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS scheduler_jobs (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  job_id TEXT NOT NULL,
  schedule TEXT NOT NULL DEFAULT '',
  time_zone TEXT NOT NULL DEFAULT 'UTC',
  state TEXT NOT NULL DEFAULT 'ENABLED',
  http_target_json TEXT NOT NULL DEFAULT '',
  pubsub_target_json TEXT NOT NULL DEFAULT '',
  oidc_audience TEXT NOT NULL DEFAULT '',
  next_run_time TEXT NOT NULL DEFAULT '',
  last_attempt_time TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cloud_tasks_queues (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  queue_id TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'RUNNING',
  rate_limits_json TEXT NOT NULL DEFAULT '{}',
  retry_config_json TEXT NOT NULL DEFAULT '{}',
  app_engine_routing_override_json TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cloud_tasks (
  name TEXT PRIMARY KEY,
  queue_name TEXT NOT NULL,
  schedule_time TEXT NOT NULL,
  create_time TEXT NOT NULL,
  http_request_json TEXT NOT NULL DEFAULT '',
  app_engine_http_request_json TEXT NOT NULL DEFAULT '',
  dispatch_count INTEGER NOT NULL DEFAULT 0,
  response_count INTEGER NOT NULL DEFAULT 0
);
`

const schemaVersion = 1
