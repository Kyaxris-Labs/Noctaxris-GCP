package store

import (
	"fmt"
	"strings"
)

const analyticsSchema = `
CREATE TABLE IF NOT EXISTS bq_datasets (
  project_id TEXT NOT NULL,
  dataset_id TEXT NOT NULL,
  location TEXT NOT NULL DEFAULT 'US',
  friendly_name TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  PRIMARY KEY (project_id, dataset_id)
);

CREATE TABLE IF NOT EXISTS bq_tables (
  project_id TEXT NOT NULL,
  dataset_id TEXT NOT NULL,
  table_id TEXT NOT NULL,
  schema_json TEXT NOT NULL DEFAULT '[]',
  friendly_name TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  PRIMARY KEY (project_id, dataset_id, table_id)
);

CREATE TABLE IF NOT EXISTS bq_rows (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  dataset_id TEXT NOT NULL,
  table_id TEXT NOT NULL,
  insert_id TEXT NOT NULL DEFAULT '',
  row_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_bq_rows_table ON bq_rows (project_id, dataset_id, table_id);

CREATE TABLE IF NOT EXISTS bq_jobs (
  project_id TEXT NOT NULL,
  job_id TEXT NOT NULL,
  location TEXT NOT NULL DEFAULT 'US',
  state TEXT NOT NULL DEFAULT 'DONE',
  query TEXT NOT NULL DEFAULT '',
  dry_run INTEGER NOT NULL DEFAULT 0,
  error_json TEXT NOT NULL DEFAULT '',
  result_json TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  PRIMARY KEY (project_id, job_id)
);

CREATE TABLE IF NOT EXISTS firebase_users (
  local_id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  email TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  disabled INTEGER NOT NULL DEFAULT 0,
  custom_attributes TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, email)
);

CREATE TABLE IF NOT EXISTS metric_descriptors (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  type TEXT NOT NULL,
  metric_kind TEXT NOT NULL DEFAULT 'GAUGE',
  value_type TEXT NOT NULL DEFAULT 'DOUBLE',
  description TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '',
  labels_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, type)
);

CREATE TABLE IF NOT EXISTS time_series_points (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  metric_type TEXT NOT NULL,
  resource_type TEXT NOT NULL DEFAULT 'global',
  resource_labels_json TEXT NOT NULL DEFAULT '{}',
  metric_labels_json TEXT NOT NULL DEFAULT '{}',
  end_time TEXT NOT NULL,
  start_time TEXT NOT NULL DEFAULT '',
  value_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ts_points ON time_series_points (project_id, metric_type, end_time);

CREATE TABLE IF NOT EXISTS alert_policies (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  policy_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  combiner TEXT NOT NULL DEFAULT 'OR',
  conditions_json TEXT NOT NULL DEFAULT '[]',
  documentation_json TEXT NOT NULL DEFAULT '{}',
  user_labels_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (project_id, policy_id)
);

CREATE TABLE IF NOT EXISTS datastore_entities (
  project_id TEXT NOT NULL,
  namespace TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  key_path TEXT NOT NULL,
  key_id INTEGER NOT NULL DEFAULT 0,
  key_name TEXT NOT NULL DEFAULT '',
  properties_json TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (project_id, namespace, key_path)
);

CREATE TABLE IF NOT EXISTS datastore_transactions (
  token TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  database_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS eventarc_triggers (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  trigger_id TEXT NOT NULL,
  filters_json TEXT NOT NULL DEFAULT '[]',
  destination_json TEXT NOT NULL DEFAULT '{}',
  transport_json TEXT NOT NULL DEFAULT '{}',
  channel TEXT NOT NULL DEFAULT '',
  service_account TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, location, trigger_id)
);

CREATE TABLE IF NOT EXISTS eventarc_channels (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  channel_id TEXT NOT NULL,
  uid TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT '',
  pubsub_topic TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, location, channel_id)
);

CREATE TABLE IF NOT EXISTS firebase_oob_codes (
  oob_code TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  email TEXT NOT NULL,
  request_type TEXT NOT NULL,
  local_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used INTEGER NOT NULL DEFAULT 0
);
`

func (s *Store) migrateAnalytics() error {
	if _, err := s.db.Exec(analyticsSchema); err != nil {
		return fmt.Errorf("apply analytics schema: %w", err)
	}
	alters := []string{
		`ALTER TABLE eventarc_triggers ADD COLUMN channel TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range alters {
		if _, err := s.db.Exec(stmt); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migrate analytics column: %w", err)
			}
		}
	}
	return nil
}
