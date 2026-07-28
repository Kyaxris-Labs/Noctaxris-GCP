package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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

// --- BigQuery ---

// BQDataset is a BigQuery dataset row.
type BQDataset struct {
	ProjectID    string
	DatasetID    string
	Location     string
	FriendlyName string
	Description  string
	CreatedAt    string
}

// BQTable is a BigQuery table row.
type BQTable struct {
	ProjectID    string
	DatasetID    string
	TableID      string
	SchemaJSON   string
	FriendlyName string
	Description  string
	CreatedAt    string
}

// CreateBQDataset inserts a dataset. created=false means already exists.
func (s *Store) CreateBQDataset(d BQDataset) (*BQDataset, bool, error) {
	d.ProjectID = strings.TrimSpace(d.ProjectID)
	d.DatasetID = strings.TrimSpace(d.DatasetID)
	if d.ProjectID == "" || d.DatasetID == "" {
		return nil, false, fmt.Errorf("project and dataset id required")
	}
	if d.Location == "" {
		d.Location = "US"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	d.CreatedAt = now
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO bq_datasets (project_id, dataset_id, location, friendly_name, description, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		d.ProjectID, d.DatasetID, d.Location, d.FriendlyName, d.Description, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return &d, true, nil
}

// GetBQDataset loads a dataset.
func (s *Store) GetBQDataset(projectID, datasetID string) (*BQDataset, bool, error) {
	var d BQDataset
	err := s.db.QueryRow(
		`SELECT project_id, dataset_id, location, friendly_name, description, created_at
		 FROM bq_datasets WHERE project_id = ? AND dataset_id = ?`,
		projectID, datasetID,
	).Scan(&d.ProjectID, &d.DatasetID, &d.Location, &d.FriendlyName, &d.Description, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &d, true, nil
}

// ListBQDatasets lists datasets for a project.
func (s *Store) ListBQDatasets(projectID string) ([]BQDataset, error) {
	rows, err := s.db.Query(
		`SELECT project_id, dataset_id, location, friendly_name, description, created_at
		 FROM bq_datasets WHERE project_id = ? ORDER BY dataset_id`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BQDataset
	for rows.Next() {
		var d BQDataset
		if err := rows.Scan(&d.ProjectID, &d.DatasetID, &d.Location, &d.FriendlyName, &d.Description, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteBQDataset removes a dataset and its tables/rows.
func (s *Store) DeleteBQDataset(projectID, datasetID string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM bq_rows WHERE project_id = ? AND dataset_id = ?`, projectID, datasetID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM bq_tables WHERE project_id = ? AND dataset_id = ?`, projectID, datasetID); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM bq_datasets WHERE project_id = ? AND dataset_id = ?`, projectID, datasetID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateBQTable inserts a table. created=false means already exists.
func (s *Store) CreateBQTable(t BQTable) (*BQTable, bool, error) {
	t.ProjectID = strings.TrimSpace(t.ProjectID)
	t.DatasetID = strings.TrimSpace(t.DatasetID)
	t.TableID = strings.TrimSpace(t.TableID)
	if t.ProjectID == "" || t.DatasetID == "" || t.TableID == "" {
		return nil, false, fmt.Errorf("project, dataset, and table id required")
	}
	if _, ok, err := s.GetBQDataset(t.ProjectID, t.DatasetID); err != nil {
		return nil, false, err
	} else if !ok {
		return nil, false, fmt.Errorf("dataset not found")
	}
	if t.SchemaJSON == "" {
		t.SchemaJSON = "[]"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	t.CreatedAt = now
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO bq_tables (project_id, dataset_id, table_id, schema_json, friendly_name, description, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ProjectID, t.DatasetID, t.TableID, t.SchemaJSON, t.FriendlyName, t.Description, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return &t, true, nil
}

// GetBQTable loads a table.
func (s *Store) GetBQTable(projectID, datasetID, tableID string) (*BQTable, bool, error) {
	var t BQTable
	err := s.db.QueryRow(
		`SELECT project_id, dataset_id, table_id, schema_json, friendly_name, description, created_at
		 FROM bq_tables WHERE project_id = ? AND dataset_id = ? AND table_id = ?`,
		projectID, datasetID, tableID,
	).Scan(&t.ProjectID, &t.DatasetID, &t.TableID, &t.SchemaJSON, &t.FriendlyName, &t.Description, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &t, true, nil
}

// ListBQTables lists tables in a dataset.
func (s *Store) ListBQTables(projectID, datasetID string) ([]BQTable, error) {
	rows, err := s.db.Query(
		`SELECT project_id, dataset_id, table_id, schema_json, friendly_name, description, created_at
		 FROM bq_tables WHERE project_id = ? AND dataset_id = ? ORDER BY table_id`,
		projectID, datasetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BQTable
	for rows.Next() {
		var t BQTable
		if err := rows.Scan(&t.ProjectID, &t.DatasetID, &t.TableID, &t.SchemaJSON, &t.FriendlyName, &t.Description, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteBQTable removes a table and its rows.
func (s *Store) DeleteBQTable(projectID, datasetID, tableID string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`DELETE FROM bq_rows WHERE project_id = ? AND dataset_id = ? AND table_id = ?`,
		projectID, datasetID, tableID,
	); err != nil {
		return false, err
	}
	res, err := tx.Exec(
		`DELETE FROM bq_tables WHERE project_id = ? AND dataset_id = ? AND table_id = ?`,
		projectID, datasetID, tableID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// InsertBQRows stores streaming insert rows. Cap is enforced by the caller.
func (s *Store) InsertBQRows(projectID, datasetID, tableID string, rows []map[string]any, insertIDs []string) error {
	if _, ok, err := s.GetBQTable(projectID, datasetID, tableID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("table not found")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			return err
		}
		insertID := ""
		if i < len(insertIDs) {
			insertID = insertIDs[i]
		}
		if _, err := tx.Exec(
			`INSERT INTO bq_rows (id, project_id, dataset_id, table_id, insert_id, row_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), projectID, datasetID, tableID, insertID, string(raw), now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListBQRows returns all rows for a table as JSON objects.
func (s *Store) ListBQRows(projectID, datasetID, tableID string) ([]map[string]any, error) {
	return s.ListBQRowsPage(projectID, datasetID, tableID, 0, 0)
}

// ListBQRowsPage returns rows with optional start offset and maxRows (0 = no cap after offset).
func (s *Store) ListBQRowsPage(projectID, datasetID, tableID string, startIndex, maxRows int) ([]map[string]any, error) {
	q := `SELECT row_json FROM bq_rows WHERE project_id = ? AND dataset_id = ? AND table_id = ? ORDER BY created_at, id`
	args := []any{projectID, datasetID, tableID}
	if maxRows > 0 {
		q += ` LIMIT ? OFFSET ?`
		args = append(args, maxRows, startIndex)
	} else if startIndex > 0 {
		q += ` LIMIT -1 OFFSET ?`
		args = append(args, startIndex)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountBQRows returns the number of stored rows for a table.
func (s *Store) CountBQRows(projectID, datasetID, tableID string) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM bq_rows WHERE project_id = ? AND dataset_id = ? AND table_id = ?`,
		projectID, datasetID, tableID,
	).Scan(&n)
	return n, err
}

// BQJob is a stored BigQuery job row.
type BQJob struct {
	ProjectID  string
	JobID      string
	Location   string
	State      string
	Query      string
	DryRun     bool
	ErrorJSON  string
	ResultJSON string
	CreatedAt  string
}

// PutBQJob upserts a job.
func (s *Store) PutBQJob(j BQJob) error {
	j.ProjectID = strings.TrimSpace(j.ProjectID)
	j.JobID = strings.TrimSpace(j.JobID)
	if j.ProjectID == "" || j.JobID == "" {
		return fmt.Errorf("project and job id required")
	}
	if j.Location == "" {
		j.Location = "US"
	}
	if j.State == "" {
		j.State = "DONE"
	}
	if j.CreatedAt == "" {
		j.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	dry := 0
	if j.DryRun {
		dry = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO bq_jobs (project_id, job_id, location, state, query, dry_run, error_json, result_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(project_id, job_id) DO UPDATE SET
		   state = excluded.state,
		   query = excluded.query,
		   dry_run = excluded.dry_run,
		   error_json = excluded.error_json,
		   result_json = excluded.result_json`,
		j.ProjectID, j.JobID, j.Location, j.State, j.Query, dry, j.ErrorJSON, j.ResultJSON, j.CreatedAt,
	)
	return err
}

// GetBQJob loads a job by project and job id.
func (s *Store) GetBQJob(projectID, jobID string) (*BQJob, bool, error) {
	var j BQJob
	var dry int
	err := s.db.QueryRow(
		`SELECT project_id, job_id, location, state, query, dry_run, error_json, result_json, created_at
		 FROM bq_jobs WHERE project_id = ? AND job_id = ?`,
		projectID, jobID,
	).Scan(&j.ProjectID, &j.JobID, &j.Location, &j.State, &j.Query, &dry, &j.ErrorJSON, &j.ResultJSON, &j.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	j.DryRun = dry != 0
	return &j, true, nil
}

// --- Firebase Auth ---

// FirebaseUser is an Identity Toolkit user row.
type FirebaseUser struct {
	LocalID          string
	ProjectID        string
	Email            string
	PasswordHash     string
	DisplayName      string
	Disabled         bool
	CustomAttributes string
	CreatedAt        string
}

// CreateFirebaseUser inserts a user. created=false means email already exists.
func (s *Store) CreateFirebaseUser(u FirebaseUser) (*FirebaseUser, bool, error) {
	u.ProjectID = strings.TrimSpace(u.ProjectID)
	u.Email = strings.TrimSpace(strings.ToLower(u.Email))
	if u.ProjectID == "" || u.Email == "" || u.PasswordHash == "" {
		return nil, false, fmt.Errorf("project, email, and password required")
	}
	if u.LocalID == "" {
		u.LocalID = uuid.NewString()
	}
	if u.CustomAttributes == "" {
		u.CustomAttributes = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	u.CreatedAt = now
	disabled := 0
	if u.Disabled {
		disabled = 1
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO firebase_users
		 (local_id, project_id, email, password_hash, display_name, disabled, custom_attributes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.LocalID, u.ProjectID, u.Email, u.PasswordHash, u.DisplayName, disabled, u.CustomAttributes, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return &u, true, nil
}

// GetFirebaseUserByEmail loads a user by project+email.
func (s *Store) GetFirebaseUserByEmail(projectID, email string) (*FirebaseUser, bool, error) {
	return s.scanFirebaseUser(
		`SELECT local_id, project_id, email, password_hash, display_name, disabled, custom_attributes, created_at
		 FROM firebase_users WHERE project_id = ? AND email = ?`,
		projectID, strings.ToLower(strings.TrimSpace(email)),
	)
}

// GetFirebaseUserByLocalID loads a user by local id.
func (s *Store) GetFirebaseUserByLocalID(localID string) (*FirebaseUser, bool, error) {
	return s.scanFirebaseUser(
		`SELECT local_id, project_id, email, password_hash, display_name, disabled, custom_attributes, created_at
		 FROM firebase_users WHERE local_id = ?`,
		localID,
	)
}

func (s *Store) scanFirebaseUser(q string, args ...any) (*FirebaseUser, bool, error) {
	var u FirebaseUser
	var disabled int
	err := s.db.QueryRow(q, args...).Scan(
		&u.LocalID, &u.ProjectID, &u.Email, &u.PasswordHash, &u.DisplayName, &disabled, &u.CustomAttributes, &u.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	u.Disabled = disabled != 0
	return &u, true, nil
}

// ListFirebaseUsers lists users for a project.
func (s *Store) ListFirebaseUsers(projectID string) ([]FirebaseUser, error) {
	list, _, err := s.ListFirebaseUsersPage(projectID, 0, "")
	return list, err
}

// ListFirebaseUsersPage lists users with optional pageSize and pageToken (local_id cursor).
func (s *Store) ListFirebaseUsersPage(projectID string, pageSize int, pageToken string) ([]FirebaseUser, string, error) {
	if pageSize <= 0 {
		pageSize = 1000
	}
	q := `SELECT local_id, project_id, email, password_hash, display_name, disabled, custom_attributes, created_at
	      FROM firebase_users WHERE project_id = ?`
	args := []any{projectID}
	if pageToken != "" {
		q += ` AND local_id > ?`
		args = append(args, pageToken)
	}
	q += ` ORDER BY local_id LIMIT ?`
	args = append(args, pageSize+1)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []FirebaseUser
	for rows.Next() {
		var u FirebaseUser
		var disabled int
		if err := rows.Scan(&u.LocalID, &u.ProjectID, &u.Email, &u.PasswordHash, &u.DisplayName, &disabled, &u.CustomAttributes, &u.CreatedAt); err != nil {
			return nil, "", err
		}
		u.Disabled = disabled != 0
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > pageSize {
		next = out[pageSize-1].LocalID
		out = out[:pageSize]
	}
	return out, next, nil
}

// FirebaseOOBCode is a password-reset (or similar) out-of-band code.
type FirebaseOOBCode struct {
	OOBCode     string
	ProjectID   string
	Email       string
	RequestType string
	LocalID     string
	CreatedAt   string
	ExpiresAt   string
	Used        bool
}

// CreateFirebaseOOBCode stores a lab OOB code (password reset theatre).
func (s *Store) CreateFirebaseOOBCode(c FirebaseOOBCode) error {
	if c.OOBCode == "" || c.ProjectID == "" || c.Email == "" || c.RequestType == "" {
		return fmt.Errorf("oob code, project, email, and request type required")
	}
	now := time.Now().UTC()
	if c.CreatedAt == "" {
		c.CreatedAt = now.Format(time.RFC3339Nano)
	}
	if c.ExpiresAt == "" {
		c.ExpiresAt = now.Add(time.Hour).Format(time.RFC3339Nano)
	}
	used := 0
	if c.Used {
		used = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO firebase_oob_codes (oob_code, project_id, email, request_type, local_id, created_at, expires_at, used)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.OOBCode, c.ProjectID, strings.ToLower(strings.TrimSpace(c.Email)), c.RequestType, c.LocalID, c.CreatedAt, c.ExpiresAt, used,
	)
	return err
}

// GetFirebaseOOBCode loads an unused, unexpired OOB code.
func (s *Store) GetFirebaseOOBCode(code string) (*FirebaseOOBCode, bool, error) {
	var c FirebaseOOBCode
	var used int
	err := s.db.QueryRow(
		`SELECT oob_code, project_id, email, request_type, local_id, created_at, expires_at, used
		 FROM firebase_oob_codes WHERE oob_code = ?`, code,
	).Scan(&c.OOBCode, &c.ProjectID, &c.Email, &c.RequestType, &c.LocalID, &c.CreatedAt, &c.ExpiresAt, &used)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	c.Used = used != 0
	return &c, true, nil
}

// ConsumeFirebaseOOBCode marks a code used if still valid.
func (s *Store) ConsumeFirebaseOOBCode(code string) (*FirebaseOOBCode, bool, error) {
	c, ok, err := s.GetFirebaseOOBCode(code)
	if err != nil || !ok {
		return nil, ok, err
	}
	if c.Used {
		return nil, false, nil
	}
	exp, err := time.Parse(time.RFC3339Nano, c.ExpiresAt)
	if err != nil {
		exp, err = time.Parse(time.RFC3339, c.ExpiresAt)
		if err != nil {
			return nil, false, err
		}
	}
	if !time.Now().UTC().Before(exp) {
		return nil, false, nil
	}
	res, err := s.db.Exec(`UPDATE firebase_oob_codes SET used = 1 WHERE oob_code = ? AND used = 0`, code)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return nil, false, err
	}
	c.Used = true
	return c, true, nil
}

// UpdateFirebaseUser patches display name / disabled / custom attributes.
func (s *Store) UpdateFirebaseUser(u FirebaseUser) error {
	disabled := 0
	if u.Disabled {
		disabled = 1
	}
	if u.CustomAttributes == "" {
		u.CustomAttributes = "{}"
	}
	_, err := s.db.Exec(
		`UPDATE firebase_users SET display_name = ?, disabled = ?, custom_attributes = ?, password_hash = COALESCE(NULLIF(?, ''), password_hash)
		 WHERE local_id = ?`,
		u.DisplayName, disabled, u.CustomAttributes, u.PasswordHash, u.LocalID,
	)
	return err
}

// DeleteFirebaseUser removes a user by local id.
func (s *Store) DeleteFirebaseUser(localID string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM firebase_users WHERE local_id = ?`, localID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// --- Monitoring ---

// MetricDescriptorRow is a Cloud Monitoring metric descriptor.
type MetricDescriptorRow struct {
	Name        string
	ProjectID   string
	Type        string
	MetricKind  string
	ValueType   string
	Description string
	DisplayName string
	LabelsJSON  string
	CreatedAt   string
}

// CreateMetricDescriptor inserts a descriptor. created=false means type exists.
func (s *Store) CreateMetricDescriptor(d MetricDescriptorRow) (*MetricDescriptorRow, bool, error) {
	d.ProjectID = strings.TrimSpace(d.ProjectID)
	d.Type = strings.TrimSpace(d.Type)
	if d.ProjectID == "" || d.Type == "" {
		return nil, false, fmt.Errorf("project and type required")
	}
	if d.Name == "" {
		d.Name = "projects/" + d.ProjectID + "/metricDescriptors/" + d.Type
	}
	if d.MetricKind == "" {
		d.MetricKind = "GAUGE"
	}
	if d.ValueType == "" {
		d.ValueType = "DOUBLE"
	}
	if d.LabelsJSON == "" {
		d.LabelsJSON = "[]"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	d.CreatedAt = now
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO metric_descriptors
		 (name, project_id, type, metric_kind, value_type, description, display_name, labels_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Name, d.ProjectID, d.Type, d.MetricKind, d.ValueType, d.Description, d.DisplayName, d.LabelsJSON, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return &d, true, nil
}

// GetMetricDescriptor loads by full resource name or type under project.
func (s *Store) GetMetricDescriptor(projectID, typeOrName string) (*MetricDescriptorRow, bool, error) {
	var d MetricDescriptorRow
	err := s.db.QueryRow(
		`SELECT name, project_id, type, metric_kind, value_type, description, display_name, labels_json, created_at
		 FROM metric_descriptors WHERE project_id = ? AND (type = ? OR name = ?)`,
		projectID, typeOrName, typeOrName,
	).Scan(&d.Name, &d.ProjectID, &d.Type, &d.MetricKind, &d.ValueType, &d.Description, &d.DisplayName, &d.LabelsJSON, &d.CreatedAt)
	if err == sql.ErrNoRows {
		// try full name without project filter
		err = s.db.QueryRow(
			`SELECT name, project_id, type, metric_kind, value_type, description, display_name, labels_json, created_at
			 FROM metric_descriptors WHERE name = ?`,
			typeOrName,
		).Scan(&d.Name, &d.ProjectID, &d.Type, &d.MetricKind, &d.ValueType, &d.Description, &d.DisplayName, &d.LabelsJSON, &d.CreatedAt)
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
	}
	if err != nil {
		return nil, false, err
	}
	return &d, true, nil
}

// ListMetricDescriptors lists descriptors for a project.
func (s *Store) ListMetricDescriptors(projectID string) ([]MetricDescriptorRow, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, type, metric_kind, value_type, description, display_name, labels_json, created_at
		 FROM metric_descriptors WHERE project_id = ? ORDER BY type`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricDescriptorRow
	for rows.Next() {
		var d MetricDescriptorRow
		if err := rows.Scan(&d.Name, &d.ProjectID, &d.Type, &d.MetricKind, &d.ValueType, &d.Description, &d.DisplayName, &d.LabelsJSON, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// TimeSeriesPoint is one Monitoring data point.
type TimeSeriesPoint struct {
	ID                 string
	ProjectID          string
	MetricType         string
	ResourceType       string
	ResourceLabelsJSON string
	MetricLabelsJSON   string
	EndTime            string
	StartTime          string
	ValueJSON          string
	CreatedAt          string
}

// CreateTimeSeriesPoints inserts points.
func (s *Store) CreateTimeSeriesPoints(points []TimeSeriesPoint) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, p := range points {
		if p.ID == "" {
			p.ID = uuid.NewString()
		}
		if p.ResourceType == "" {
			p.ResourceType = "global"
		}
		if p.ResourceLabelsJSON == "" {
			p.ResourceLabelsJSON = "{}"
		}
		if p.MetricLabelsJSON == "" {
			p.MetricLabelsJSON = "{}"
		}
		if p.EndTime == "" {
			p.EndTime = now
		}
		if p.ValueJSON == "" {
			p.ValueJSON = "{}"
		}
		if _, err := tx.Exec(
			`INSERT INTO time_series_points
			 (id, project_id, metric_type, resource_type, resource_labels_json, metric_labels_json, end_time, start_time, value_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.ProjectID, p.MetricType, p.ResourceType, p.ResourceLabelsJSON, p.MetricLabelsJSON,
			p.EndTime, p.StartTime, p.ValueJSON, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListTimeSeriesFilter selects points for ListTimeSeries.
type ListTimeSeriesFilter struct {
	ProjectID  string
	MetricType string
	StartTime  string
	EndTime    string
}

// ListTimeSeriesPoints returns points ordered by end_time.
func (s *Store) ListTimeSeriesPoints(f ListTimeSeriesFilter) ([]TimeSeriesPoint, error) {
	q := `SELECT id, project_id, metric_type, resource_type, resource_labels_json, metric_labels_json, end_time, start_time, value_json, created_at
	      FROM time_series_points WHERE project_id = ?`
	args := []any{f.ProjectID}
	if f.MetricType != "" {
		q += ` AND metric_type = ?`
		args = append(args, f.MetricType)
	}
	if f.StartTime != "" {
		q += ` AND end_time >= ?`
		args = append(args, f.StartTime)
	}
	if f.EndTime != "" {
		q += ` AND end_time <= ?`
		args = append(args, f.EndTime)
	}
	q += ` ORDER BY end_time`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TimeSeriesPoint
	for rows.Next() {
		var p TimeSeriesPoint
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.MetricType, &p.ResourceType, &p.ResourceLabelsJSON, &p.MetricLabelsJSON, &p.EndTime, &p.StartTime, &p.ValueJSON, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteTimeSeriesPoints deletes points matching project and optional metric type. Returns rows deleted.
func (s *Store) DeleteTimeSeriesPoints(projectID, metricType string) (int64, error) {
	if projectID == "" {
		return 0, fmt.Errorf("project id required")
	}
	var (
		res sql.Result
		err error
	)
	if metricType == "" {
		res, err = s.db.Exec(`DELETE FROM time_series_points WHERE project_id = ?`, projectID)
	} else {
		res, err = s.db.Exec(`DELETE FROM time_series_points WHERE project_id = ? AND metric_type = ?`, projectID, metricType)
	}
	if err != nil {
		return 0, fmt.Errorf("delete time series: %w", err)
	}
	return res.RowsAffected()
}

// AlertPolicyRow is a Cloud Monitoring alert policy metadata row (theatre).
type AlertPolicyRow struct {
	Name               string
	ProjectID          string
	PolicyID           string
	DisplayName        string
	Enabled            bool
	Combiner           string
	ConditionsJSON     string
	DocumentationJSON  string
	UserLabelsJSON     string
	CreatedAt          string
	UpdatedAt          string
}

// CreateAlertPolicy inserts an alert policy. created=false when already exists.
func (s *Store) CreateAlertPolicy(p AlertPolicyRow) (*AlertPolicyRow, bool, error) {
	p.ProjectID = strings.TrimSpace(p.ProjectID)
	p.PolicyID = strings.TrimSpace(p.PolicyID)
	if p.ProjectID == "" || p.PolicyID == "" {
		return nil, false, fmt.Errorf("project and policy id required")
	}
	if p.Name == "" {
		p.Name = "projects/" + p.ProjectID + "/alertPolicies/" + p.PolicyID
	}
	if p.Combiner == "" {
		p.Combiner = "OR"
	}
	if p.ConditionsJSON == "" {
		p.ConditionsJSON = "[]"
	}
	if p.DocumentationJSON == "" {
		p.DocumentationJSON = "{}"
	}
	if p.UserLabelsJSON == "" {
		p.UserLabelsJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	p.CreatedAt = now
	p.UpdatedAt = now
	enabled := 1
	if !p.Enabled {
		enabled = 0
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO alert_policies
		 (name, project_id, policy_id, display_name, enabled, combiner, conditions_json, documentation_json, user_labels_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.ProjectID, p.PolicyID, p.DisplayName, enabled, p.Combiner, p.ConditionsJSON, p.DocumentationJSON, p.UserLabelsJSON, now, now,
	)
	if err != nil {
		return nil, false, fmt.Errorf("create alert policy: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return &p, true, nil
}

// GetAlertPolicy loads by resource name.
func (s *Store) GetAlertPolicy(name string) (*AlertPolicyRow, bool, error) {
	var p AlertPolicyRow
	var enabled int
	err := s.db.QueryRow(
		`SELECT name, project_id, policy_id, display_name, enabled, combiner, conditions_json, documentation_json, user_labels_json, created_at, updated_at
		 FROM alert_policies WHERE name = ?`, name,
	).Scan(&p.Name, &p.ProjectID, &p.PolicyID, &p.DisplayName, &enabled, &p.Combiner, &p.ConditionsJSON, &p.DocumentationJSON, &p.UserLabelsJSON, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get alert policy: %w", err)
	}
	p.Enabled = enabled != 0
	return &p, true, nil
}

// ListAlertPolicies lists policies for a project.
func (s *Store) ListAlertPolicies(projectID string) ([]AlertPolicyRow, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, policy_id, display_name, enabled, combiner, conditions_json, documentation_json, user_labels_json, created_at, updated_at
		 FROM alert_policies WHERE project_id = ? ORDER BY policy_id`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list alert policies: %w", err)
	}
	defer rows.Close()
	var out []AlertPolicyRow
	for rows.Next() {
		var p AlertPolicyRow
		var enabled int
		if err := rows.Scan(&p.Name, &p.ProjectID, &p.PolicyID, &p.DisplayName, &enabled, &p.Combiner, &p.ConditionsJSON, &p.DocumentationJSON, &p.UserLabelsJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Enabled = enabled != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateAlertPolicy replaces theatre metadata for an existing policy.
func (s *Store) UpdateAlertPolicy(p AlertPolicyRow) (*AlertPolicyRow, bool, error) {
	if p.Name == "" {
		return nil, false, fmt.Errorf("name required")
	}
	if p.Combiner == "" {
		p.Combiner = "OR"
	}
	if p.ConditionsJSON == "" {
		p.ConditionsJSON = "[]"
	}
	if p.DocumentationJSON == "" {
		p.DocumentationJSON = "{}"
	}
	if p.UserLabelsJSON == "" {
		p.UserLabelsJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	enabled := 1
	if !p.Enabled {
		enabled = 0
	}
	res, err := s.db.Exec(
		`UPDATE alert_policies SET display_name = ?, enabled = ?, combiner = ?, conditions_json = ?, documentation_json = ?, user_labels_json = ?, updated_at = ?
		 WHERE name = ?`,
		p.DisplayName, enabled, p.Combiner, p.ConditionsJSON, p.DocumentationJSON, p.UserLabelsJSON, now, p.Name,
	)
	if err != nil {
		return nil, false, fmt.Errorf("update alert policy: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return s.GetAlertPolicy(p.Name)
}

// DeleteAlertPolicy removes a policy by name.
func (s *Store) DeleteAlertPolicy(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM alert_policies WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete alert policy: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// --- Datastore ---

// DatastoreEntity is a Datastore entity row (distinct from Firestore).
type DatastoreEntity struct {
	ProjectID      string
	Namespace      string
	Kind           string
	KeyPath        string
	KeyID          int64
	KeyName        string
	PropertiesJSON string
	UpdatedAt      string
}

// PutDatastoreEntity upserts an entity.
func (s *Store) PutDatastoreEntity(e DatastoreEntity) error {
	e.ProjectID = strings.TrimSpace(e.ProjectID)
	e.Kind = strings.TrimSpace(e.Kind)
	e.KeyPath = strings.TrimSpace(e.KeyPath)
	if e.ProjectID == "" || e.Kind == "" || e.KeyPath == "" {
		return fmt.Errorf("project, kind, and key_path required")
	}
	if e.PropertiesJSON == "" {
		e.PropertiesJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO datastore_entities (project_id, namespace, kind, key_path, key_id, key_name, properties_json, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(project_id, namespace, key_path) DO UPDATE SET
		   kind = excluded.kind,
		   key_id = excluded.key_id,
		   key_name = excluded.key_name,
		   properties_json = excluded.properties_json,
		   updated_at = excluded.updated_at`,
		e.ProjectID, e.Namespace, e.Kind, e.KeyPath, e.KeyID, e.KeyName, e.PropertiesJSON, now,
	)
	return err
}

// GetDatastoreEntity loads by project/namespace/key_path.
func (s *Store) GetDatastoreEntity(projectID, namespace, keyPath string) (*DatastoreEntity, bool, error) {
	var e DatastoreEntity
	err := s.db.QueryRow(
		`SELECT project_id, namespace, kind, key_path, key_id, key_name, properties_json, updated_at
		 FROM datastore_entities WHERE project_id = ? AND namespace = ? AND key_path = ?`,
		projectID, namespace, keyPath,
	).Scan(&e.ProjectID, &e.Namespace, &e.Kind, &e.KeyPath, &e.KeyID, &e.KeyName, &e.PropertiesJSON, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &e, true, nil
}

// DeleteDatastoreEntity removes an entity.
func (s *Store) DeleteDatastoreEntity(projectID, namespace, keyPath string) (bool, error) {
	res, err := s.db.Exec(
		`DELETE FROM datastore_entities WHERE project_id = ? AND namespace = ? AND key_path = ?`,
		projectID, namespace, keyPath,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// QueryDatastoreEntitiesFilter is equality-only RunQuery support.
type QueryDatastoreEntitiesFilter struct {
	ProjectID string
	Namespace string
	Kind      string
	// PropEquals maps property name -> JSON-encoded scalar for equality.
	PropEquals map[string]string
	Limit      int
}

// QueryDatastoreEntities returns entities matching kind + equality filters.
func (s *Store) QueryDatastoreEntities(f QueryDatastoreEntitiesFilter) ([]DatastoreEntity, error) {
	q := `SELECT project_id, namespace, kind, key_path, key_id, key_name, properties_json, updated_at
	      FROM datastore_entities WHERE project_id = ? AND namespace = ? AND kind = ?`
	args := []any{f.ProjectID, f.Namespace, f.Kind}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DatastoreEntity
	for rows.Next() {
		var e DatastoreEntity
		if err := rows.Scan(&e.ProjectID, &e.Namespace, &e.Kind, &e.KeyPath, &e.KeyID, &e.KeyName, &e.PropertiesJSON, &e.UpdatedAt); err != nil {
			return nil, err
		}
		if len(f.PropEquals) > 0 {
			var props map[string]any
			if err := json.Unmarshal([]byte(e.PropertiesJSON), &props); err != nil {
				continue
			}
			match := true
			for k, wantRaw := range f.PropEquals {
				got, ok := props[k]
				if !ok {
					match = false
					break
				}
				gotRaw, _ := json.Marshal(got)
				if string(gotRaw) != wantRaw && fmt.Sprint(got) != strings.Trim(wantRaw, `"`) {
					// also compare unquoted string
					var want any
					_ = json.Unmarshal([]byte(wantRaw), &want)
					if fmt.Sprint(got) != fmt.Sprint(want) {
						match = false
						break
					}
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, e)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, rows.Err()
}

// NextDatastoreID allocates a numeric id (lab counter via max+1).
func (s *Store) NextDatastoreID(projectID, namespace, kind string) (int64, error) {
	var max sql.NullInt64
	err := s.db.QueryRow(
		`SELECT MAX(key_id) FROM datastore_entities WHERE project_id = ? AND namespace = ? AND kind = ?`,
		projectID, namespace, kind,
	).Scan(&max)
	if err != nil {
		return 0, err
	}
	if !max.Valid || max.Int64 < 1 {
		return 1, nil
	}
	return max.Int64 + 1, nil
}

// PutDatastoreTransaction registers a lab transaction token (no isolation).
func (s *Store) PutDatastoreTransaction(token, projectID, databaseID string) error {
	if token == "" || projectID == "" {
		return fmt.Errorf("datastore transaction requires token and project")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO datastore_transactions (token, project_id, database_id, created_at) VALUES (?, ?, ?, ?)`,
		token, projectID, databaseID, now,
	)
	if err != nil {
		return fmt.Errorf("put datastore transaction: %w", err)
	}
	return nil
}

// ConsumeDatastoreTransaction deletes and validates a token for project. ok is false when missing.
func (s *Store) ConsumeDatastoreTransaction(token, projectID string) (bool, error) {
	if token == "" {
		return false, nil
	}
	res, err := s.db.Exec(
		`DELETE FROM datastore_transactions WHERE token = ? AND project_id = ?`,
		token, projectID,
	)
	if err != nil {
		return false, fmt.Errorf("consume datastore transaction: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteDatastoreTransaction removes a token. ok is false when missing.
func (s *Store) DeleteDatastoreTransaction(token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	res, err := s.db.Exec(`DELETE FROM datastore_transactions WHERE token = ?`, token)
	if err != nil {
		return false, fmt.Errorf("delete datastore transaction: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// --- Eventarc ---

// EventarcTrigger is a trigger row.
type EventarcTrigger struct {
	Name            string
	ProjectID       string
	Location        string
	TriggerID       string
	FiltersJSON     string
	DestinationJSON string
	TransportJSON   string
	Channel         string
	CreatedAt       string
}

// EventarcChannel is a channel stub row.
type EventarcChannel struct {
	Name        string
	ProjectID   string
	Location    string
	ChannelID   string
	UID         string
	Provider    string
	PubsubTopic string
	State       string
	CreatedAt   string
}

// CreateEventarcTrigger inserts a trigger. created=false means already exists.
func (s *Store) CreateEventarcTrigger(t EventarcTrigger) (*EventarcTrigger, bool, error) {
	t.ProjectID = strings.TrimSpace(t.ProjectID)
	t.Location = strings.TrimSpace(t.Location)
	t.TriggerID = strings.TrimSpace(t.TriggerID)
	if t.ProjectID == "" || t.Location == "" || t.TriggerID == "" {
		return nil, false, fmt.Errorf("project, location, and trigger id required")
	}
	if t.Name == "" {
		t.Name = "projects/" + t.ProjectID + "/locations/" + t.Location + "/triggers/" + t.TriggerID
	}
	if t.FiltersJSON == "" {
		t.FiltersJSON = "[]"
	}
	if t.DestinationJSON == "" {
		t.DestinationJSON = "{}"
	}
	if t.TransportJSON == "" {
		t.TransportJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	t.CreatedAt = now
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO eventarc_triggers
		 (name, project_id, location, trigger_id, filters_json, destination_json, transport_json, channel, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.ProjectID, t.Location, t.TriggerID, t.FiltersJSON, t.DestinationJSON, t.TransportJSON, t.Channel, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return &t, true, nil
}

// GetEventarcTrigger loads by resource name.
func (s *Store) GetEventarcTrigger(name string) (*EventarcTrigger, bool, error) {
	var t EventarcTrigger
	err := s.db.QueryRow(
		`SELECT name, project_id, location, trigger_id, filters_json, destination_json, transport_json, channel, created_at
		 FROM eventarc_triggers WHERE name = ?`,
		name,
	).Scan(&t.Name, &t.ProjectID, &t.Location, &t.TriggerID, &t.FiltersJSON, &t.DestinationJSON, &t.TransportJSON, &t.Channel, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &t, true, nil
}

// ListEventarcTriggers lists triggers for project+location ("-" lists all locations).
func (s *Store) ListEventarcTriggers(projectID, location string) ([]EventarcTrigger, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if location == "" || location == "-" {
		rows, err = s.db.Query(
			`SELECT name, project_id, location, trigger_id, filters_json, destination_json, transport_json, channel, created_at
			 FROM eventarc_triggers WHERE project_id = ? ORDER BY name`,
			projectID,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT name, project_id, location, trigger_id, filters_json, destination_json, transport_json, channel, created_at
			 FROM eventarc_triggers WHERE project_id = ? AND location = ? ORDER BY name`,
			projectID, location,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventarcTrigger
	for rows.Next() {
		var t EventarcTrigger
		if err := rows.Scan(&t.Name, &t.ProjectID, &t.Location, &t.TriggerID, &t.FiltersJSON, &t.DestinationJSON, &t.TransportJSON, &t.Channel, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteEventarcTrigger removes a trigger.
func (s *Store) DeleteEventarcTrigger(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM eventarc_triggers WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateEventarcChannel inserts a channel stub. created=false means already exists.
func (s *Store) CreateEventarcChannel(c EventarcChannel) (*EventarcChannel, bool, error) {
	c.ProjectID = strings.TrimSpace(c.ProjectID)
	c.Location = strings.TrimSpace(c.Location)
	c.ChannelID = strings.TrimSpace(c.ChannelID)
	if c.ProjectID == "" || c.Location == "" || c.ChannelID == "" {
		return nil, false, fmt.Errorf("project, location, and channel id required")
	}
	if c.Name == "" {
		c.Name = "projects/" + c.ProjectID + "/locations/" + c.Location + "/channels/" + c.ChannelID
	}
	if c.UID == "" {
		c.UID = uuid.NewString()
	}
	if c.State == "" {
		c.State = "ACTIVE"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	c.CreatedAt = now
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO eventarc_channels
		 (name, project_id, location, channel_id, uid, provider, pubsub_topic, state, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.ProjectID, c.Location, c.ChannelID, c.UID, c.Provider, c.PubsubTopic, c.State, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return &c, true, nil
}

// GetEventarcChannel loads by resource name.
func (s *Store) GetEventarcChannel(name string) (*EventarcChannel, bool, error) {
	var c EventarcChannel
	err := s.db.QueryRow(
		`SELECT name, project_id, location, channel_id, uid, provider, pubsub_topic, state, created_at
		 FROM eventarc_channels WHERE name = ?`, name,
	).Scan(&c.Name, &c.ProjectID, &c.Location, &c.ChannelID, &c.UID, &c.Provider, &c.PubsubTopic, &c.State, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &c, true, nil
}

// ListEventarcChannels lists channels for project+location.
func (s *Store) ListEventarcChannels(projectID, location string) ([]EventarcChannel, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, channel_id, uid, provider, pubsub_topic, state, created_at
		 FROM eventarc_channels WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventarcChannel
	for rows.Next() {
		var c EventarcChannel
		if err := rows.Scan(&c.Name, &c.ProjectID, &c.Location, &c.ChannelID, &c.UID, &c.Provider, &c.PubsubTopic, &c.State, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteEventarcChannel removes a channel.
func (s *Store) DeleteEventarcChannel(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM eventarc_channels WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

type eventFilter struct {
	Attribute string            `json:"attribute"`
	Value     string            `json:"value"`
	Operator  string            `json:"operator"`
	Values    map[string]string `json:"values"`
}

type eventDestination struct {
	HTTPEndpoint *struct {
		URI string `json:"uri"`
	} `json:"httpEndpoint"`
	CloudRunService *struct {
		Service string `json:"service"`
		Region  string `json:"region"`
		Path    string `json:"path"`
	} `json:"cloudRunService"`
}

type eventTransport struct {
	Pubsub *struct {
		Topic string `json:"topic"`
	} `json:"pubsub"`
}

// DeliverEventarcForPubSub best-effort POSTs matching triggers for a Pub/Sub publish.
func (s *Store) DeliverEventarcForPubSub(topic string, data []byte, attributes map[string]string) {
	triggers, err := s.listAllEventarcTriggers()
	if err != nil {
		return
	}
	attrs := map[string]string{"type": "google.cloud.pubsub.topic.v1.messagePublished"}
	for k, v := range attributes {
		attrs[k] = v
	}
	payload := map[string]any{
		"specversion":     "1.0",
		"type":            "google.cloud.pubsub.topic.v1.messagePublished",
		"source":          "//pubsub.googleapis.com/" + topic,
		"id":              uuid.NewString(),
		"time":            time.Now().UTC().Format(time.RFC3339Nano),
		"datacontenttype": "application/json",
		"data": map[string]any{
			"message": map[string]any{
				"data":       data,
				"attributes": attributes,
			},
			"subscription": "",
		},
	}
	for _, t := range triggers {
		if !eventarcMatches(t, attrs, topic, "") {
			continue
		}
		s.deliverEventarc(t, payload)
	}
}

// DeliverEventarcForGCSFinalize best-effort POSTs matching triggers for object finalize.
func (s *Store) DeliverEventarcForGCSFinalize(bucket, objectName string, generation int64, size int64, contentType string) {
	triggers, err := s.listAllEventarcTriggers()
	if err != nil {
		return
	}
	attrs := map[string]string{
		"type":   "google.cloud.storage.object.v1.finalized",
		"bucket": bucket,
	}
	payload := map[string]any{
		"specversion":     "1.0",
		"type":            "google.cloud.storage.object.v1.finalized",
		"source":          "//storage.googleapis.com/projects/_/buckets/" + bucket,
		"id":              uuid.NewString(),
		"time":            time.Now().UTC().Format(time.RFC3339Nano),
		"datacontenttype": "application/json",
		"data": map[string]any{
			"bucket":      bucket,
			"name":        objectName,
			"generation":  strconv.FormatInt(generation, 10),
			"size":        strconv.FormatInt(size, 10),
			"contentType": contentType,
		},
	}
	for _, t := range triggers {
		if !eventarcMatches(t, attrs, "", bucket) {
			continue
		}
		s.deliverEventarc(t, payload)
	}
}

func (s *Store) listAllEventarcTriggers() ([]EventarcTrigger, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, trigger_id, filters_json, destination_json, transport_json, channel, created_at
		 FROM eventarc_triggers`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventarcTrigger
	for rows.Next() {
		var t EventarcTrigger
		if err := rows.Scan(&t.Name, &t.ProjectID, &t.Location, &t.TriggerID, &t.FiltersJSON, &t.DestinationJSON, &t.TransportJSON, &t.Channel, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func eventarcMatches(t EventarcTrigger, attrs map[string]string, pubsubTopic, gcsBucket string) bool {
	var filters []eventFilter
	if err := json.Unmarshal([]byte(t.FiltersJSON), &filters); err != nil {
		return false
	}
	if len(filters) == 0 {
		return false
	}
	for _, f := range filters {
		switch f.Attribute {
		case "type":
			if attrs["type"] != f.Value {
				return false
			}
		case "bucket":
			if gcsBucket != f.Value && attrs["bucket"] != f.Value {
				return false
			}
		default:
			if len(f.Values) > 0 {
				for k, v := range f.Values {
					if attrs[k] != v {
						return false
					}
				}
				continue
			}
			if f.Value != "" && attrs[f.Attribute] != f.Value {
				return false
			}
		}
	}
	// Pub/Sub transport topic filter when present.
	var transport eventTransport
	_ = json.Unmarshal([]byte(t.TransportJSON), &transport)
	if transport.Pubsub != nil && transport.Pubsub.Topic != "" && pubsubTopic != "" {
		if transport.Pubsub.Topic != pubsubTopic {
			return false
		}
	}
	return true
}

func (s *Store) deliverEventarc(t EventarcTrigger, payload map[string]any) {
	uri := s.resolveEventarcURI(t)
	if uri == "" {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 3 * time.Second}
	doPost := func() error {
		req, err := http.NewRequest(http.MethodPost, uri, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("ce-specversion", "1.0")
		if typ, ok := payload["type"].(string); ok {
			req.Header.Set("ce-type", typ)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode >= 500 {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return nil
	}
	if err := doPost(); err != nil {
		_ = doPost() // one retry on failed deliver
	}
}

func (s *Store) resolveEventarcURI(t EventarcTrigger) string {
	var dest eventDestination
	if err := json.Unmarshal([]byte(t.DestinationJSON), &dest); err != nil {
		return ""
	}
	if dest.HTTPEndpoint != nil && dest.HTTPEndpoint.URI != "" {
		return dest.HTTPEndpoint.URI
	}
	if dest.CloudRunService != nil && dest.CloudRunService.Service != "" {
		region := dest.CloudRunService.Region
		if region == "" {
			region = t.Location
		}
		path := dest.CloudRunService.Path
		if path == "" {
			path = "/"
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		runName := fmt.Sprintf("projects/%s/locations/%s/services/%s", t.ProjectID, region, dest.CloudRunService.Service)
		if rs, ok, err := s.GetRunService(runName); err == nil && ok && rs.URI != "" {
			return strings.TrimRight(rs.URI, "/") + path
		}
		return fmt.Sprintf("http://127.0.0.1:4588/v2/projects/%s/locations/%s/services/%s:invoke%s",
			t.ProjectID, region, dest.CloudRunService.Service, path)
	}
	return ""
}
