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
  created_at TEXT NOT NULL
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
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (bucket, name, generation)
);

CREATE TABLE IF NOT EXISTS pubsub_topics (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pubsub_subscriptions (
  name TEXT PRIMARY KEY,
  topic TEXT NOT NULL,
  project_id TEXT NOT NULL,
  ack_deadline_seconds INTEGER NOT NULL DEFAULT 10,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pubsub_messages (
  id TEXT PRIMARY KEY,
  subscription TEXT NOT NULL,
  topic TEXT NOT NULL,
  data BLOB NOT NULL,
  attributes_json TEXT NOT NULL DEFAULT '{}',
  publish_time TEXT NOT NULL,
  ack_id TEXT NOT NULL UNIQUE,
  ack_deadline TEXT,
  delivered INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS secrets (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
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
  created_at TEXT NOT NULL
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
`

const schemaVersion = 1
