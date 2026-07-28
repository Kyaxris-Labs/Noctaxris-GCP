package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const workflowsSpannerSchema = `
CREATE TABLE IF NOT EXISTS wf_workflows (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  workflow_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  source_contents TEXT NOT NULL DEFAULT '',
  service_account TEXT NOT NULL DEFAULT '',
  revision_id TEXT NOT NULL DEFAULT '000001-lab',
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  labels_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (project_id, location, workflow_id)
);

CREATE TABLE IF NOT EXISTS wf_executions (
  name TEXT PRIMARY KEY,
  workflow_name TEXT NOT NULL,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  workflow_id TEXT NOT NULL,
  execution_id TEXT NOT NULL,
  argument TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'SUCCEEDED',
  workflow_revision_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  start_time TEXT NOT NULL DEFAULT '',
  end_time TEXT NOT NULL DEFAULT '',
  UNIQUE (workflow_name, execution_id)
);

CREATE INDEX IF NOT EXISTS idx_wf_executions_workflow ON wf_executions (workflow_name);

CREATE TABLE IF NOT EXISTS spanner_instances (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '',
  node_count INTEGER NOT NULL DEFAULT 1,
  processing_units INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT 'READY',
  labels_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (project_id, instance_id)
);

CREATE TABLE IF NOT EXISTS spanner_databases (
  name TEXT PRIMARY KEY,
  instance_name TEXT NOT NULL,
  project_id TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  database_id TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'READY',
  create_statement TEXT NOT NULL DEFAULT '',
  extra_statements_json TEXT NOT NULL DEFAULT '[]',
  ddl_statements_json TEXT NOT NULL DEFAULT '[]',
  dialect TEXT NOT NULL DEFAULT 'GOOGLE_STANDARD_SQL',
  created_at TEXT NOT NULL,
  UNIQUE (instance_name, database_id)
);

CREATE TABLE IF NOT EXISTS spanner_sessions (
  name TEXT PRIMARY KEY,
  database_name TEXT NOT NULL,
  project_id TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  database_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (database_name, session_id)
);

CREATE TABLE IF NOT EXISTS spanner_rows (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  database_name TEXT NOT NULL,
  table_name TEXT NOT NULL,
  key_json TEXT NOT NULL DEFAULT '[]',
  columns_json TEXT NOT NULL DEFAULT '[]',
  values_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_spanner_rows_db_table ON spanner_rows (database_name, table_name);
`

func (s *Store) migrateWorkflowsSpanner() error {
	if _, err := s.db.Exec(workflowsSpannerSchema); err != nil {
		return fmt.Errorf("apply workflows/spanner schema: %w", err)
	}
	return nil
}

// --- Workflows ---

// Workflow is a Cloud Workflows workflow row.
type Workflow struct {
	Name            string
	ProjectID       string
	Location        string
	WorkflowID      string
	Description     string
	SourceContents  string
	ServiceAccount  string
	RevisionID      string
	State           string
	LabelsJSON      string
	CreatedAt       string
	UpdatedAt       string
}

// WorkflowExecution is a Workflows execution row (theatre: immediate SUCCEEDED).
type WorkflowExecution struct {
	Name               string
	WorkflowName       string
	ProjectID          string
	Location           string
	WorkflowID         string
	ExecutionID        string
	Argument           string
	Result             string
	State              string
	WorkflowRevisionID string
	CreatedAt          string
	StartTime          string
	EndTime            string
}

// CreateWorkflow inserts a workflow. created=false means already exists.
func (s *Store) CreateWorkflow(w Workflow) (bool, error) {
	if w.Name == "" || w.ProjectID == "" || w.Location == "" || w.WorkflowID == "" {
		return false, fmt.Errorf("workflow requires name, project, location, and workflow id")
	}
	if w.State == "" {
		w.State = "ACTIVE"
	}
	if w.RevisionID == "" {
		w.RevisionID = "000001-lab"
	}
	if w.LabelsJSON == "" {
		w.LabelsJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if w.CreatedAt == "" {
		w.CreatedAt = now
	}
	if w.UpdatedAt == "" {
		w.UpdatedAt = w.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO wf_workflows
		 (name, project_id, location, workflow_id, description, source_contents, service_account,
		  revision_id, state, labels_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.Name, w.ProjectID, w.Location, w.WorkflowID, w.Description, w.SourceContents, w.ServiceAccount,
		w.RevisionID, w.State, w.LabelsJSON, w.CreatedAt, w.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create workflow: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetWorkflow loads a workflow by name.
func (s *Store) GetWorkflow(name string) (Workflow, bool, error) {
	var w Workflow
	err := s.db.QueryRow(
		`SELECT name, project_id, location, workflow_id, description, source_contents, service_account,
		        revision_id, state, labels_json, created_at, updated_at
		 FROM wf_workflows WHERE name = ?`, name,
	).Scan(
		&w.Name, &w.ProjectID, &w.Location, &w.WorkflowID, &w.Description, &w.SourceContents, &w.ServiceAccount,
		&w.RevisionID, &w.State, &w.LabelsJSON, &w.CreatedAt, &w.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return Workflow{}, false, nil
	}
	if err != nil {
		return Workflow{}, false, fmt.Errorf("get workflow: %w", err)
	}
	return w, true, nil
}

// ListWorkflows lists workflows under project/location.
func (s *Store) ListWorkflows(projectID, location string) ([]Workflow, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, workflow_id, description, source_contents, service_account,
		        revision_id, state, labels_json, created_at, updated_at
		 FROM wf_workflows WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()
	var out []Workflow
	for rows.Next() {
		var w Workflow
		if err := rows.Scan(
			&w.Name, &w.ProjectID, &w.Location, &w.WorkflowID, &w.Description, &w.SourceContents, &w.ServiceAccount,
			&w.RevisionID, &w.State, &w.LabelsJSON, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// DeleteWorkflow deletes a workflow and its executions.
func (s *Store) DeleteWorkflow(name string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM wf_executions WHERE workflow_name = ?`, name); err != nil {
		return false, fmt.Errorf("delete workflow executions: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM wf_workflows WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete workflow: %w", err)
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

// CreateWorkflowExecution inserts an execution. created=false means already exists.
func (s *Store) CreateWorkflowExecution(e WorkflowExecution) (bool, error) {
	if e.Name == "" || e.WorkflowName == "" || e.ExecutionID == "" {
		return false, fmt.Errorf("execution requires name, workflow name, and execution id")
	}
	if e.State == "" {
		e.State = "SUCCEEDED"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if e.CreatedAt == "" {
		e.CreatedAt = now
	}
	if e.StartTime == "" {
		e.StartTime = e.CreatedAt
	}
	if e.EndTime == "" {
		e.EndTime = e.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO wf_executions
		 (name, workflow_name, project_id, location, workflow_id, execution_id, argument, result,
		  state, workflow_revision_id, created_at, start_time, end_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Name, e.WorkflowName, e.ProjectID, e.Location, e.WorkflowID, e.ExecutionID, e.Argument, e.Result,
		e.State, e.WorkflowRevisionID, e.CreatedAt, e.StartTime, e.EndTime,
	)
	if err != nil {
		return false, fmt.Errorf("create workflow execution: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetWorkflowExecution loads an execution by name.
func (s *Store) GetWorkflowExecution(name string) (WorkflowExecution, bool, error) {
	var e WorkflowExecution
	err := s.db.QueryRow(
		`SELECT name, workflow_name, project_id, location, workflow_id, execution_id, argument, result,
		        state, workflow_revision_id, created_at, start_time, end_time
		 FROM wf_executions WHERE name = ?`, name,
	).Scan(
		&e.Name, &e.WorkflowName, &e.ProjectID, &e.Location, &e.WorkflowID, &e.ExecutionID, &e.Argument, &e.Result,
		&e.State, &e.WorkflowRevisionID, &e.CreatedAt, &e.StartTime, &e.EndTime,
	)
	if err == sql.ErrNoRows {
		return WorkflowExecution{}, false, nil
	}
	if err != nil {
		return WorkflowExecution{}, false, fmt.Errorf("get workflow execution: %w", err)
	}
	return e, true, nil
}

// ListWorkflowExecutions lists executions for a workflow.
func (s *Store) ListWorkflowExecutions(workflowName string) ([]WorkflowExecution, error) {
	rows, err := s.db.Query(
		`SELECT name, workflow_name, project_id, location, workflow_id, execution_id, argument, result,
		        state, workflow_revision_id, created_at, start_time, end_time
		 FROM wf_executions WHERE workflow_name = ? ORDER BY created_at DESC`,
		workflowName,
	)
	if err != nil {
		return nil, fmt.Errorf("list workflow executions: %w", err)
	}
	defer rows.Close()
	var out []WorkflowExecution
	for rows.Next() {
		var e WorkflowExecution
		if err := rows.Scan(
			&e.Name, &e.WorkflowName, &e.ProjectID, &e.Location, &e.WorkflowID, &e.ExecutionID, &e.Argument, &e.Result,
			&e.State, &e.WorkflowRevisionID, &e.CreatedAt, &e.StartTime, &e.EndTime,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- Spanner ---

// SpannerInstance is a Spanner instance admin row.
type SpannerInstance struct {
	Name            string
	ProjectID       string
	InstanceID      string
	Config          string
	DisplayName     string
	NodeCount       int
	ProcessingUnits int
	State           string
	LabelsJSON      string
	CreatedAt       string
	UpdatedAt       string
}

// SpannerDatabase is a Spanner database admin row.
type SpannerDatabase struct {
	Name                string
	InstanceName        string
	ProjectID           string
	InstanceID          string
	DatabaseID          string
	State               string
	CreateStatement     string
	ExtraStatementsJSON string
	DDLStatementsJSON   string
	Dialect             string
	CreatedAt           string
}

// SpannerSession is a Spanner session theatre row.
type SpannerSession struct {
	Name         string
	DatabaseName string
	ProjectID    string
	InstanceID   string
	DatabaseID   string
	SessionID    string
	CreatedAt    string
}

// CreateSpannerInstance inserts an instance. created=false means already exists.
func (s *Store) CreateSpannerInstance(inst SpannerInstance) (bool, error) {
	if inst.Name == "" || inst.ProjectID == "" || inst.InstanceID == "" {
		return false, fmt.Errorf("spanner instance requires name, project, and instance id")
	}
	if inst.State == "" {
		inst.State = "READY"
	}
	if inst.DisplayName == "" {
		inst.DisplayName = inst.InstanceID
	}
	if inst.LabelsJSON == "" {
		inst.LabelsJSON = "{}"
	}
	if inst.NodeCount == 0 && inst.ProcessingUnits == 0 {
		inst.NodeCount = 1
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if inst.CreatedAt == "" {
		inst.CreatedAt = now
	}
	if inst.UpdatedAt == "" {
		inst.UpdatedAt = inst.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO spanner_instances
		 (name, project_id, instance_id, config, display_name, node_count, processing_units,
		  state, labels_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.Name, inst.ProjectID, inst.InstanceID, inst.Config, inst.DisplayName, inst.NodeCount, inst.ProcessingUnits,
		inst.State, inst.LabelsJSON, inst.CreatedAt, inst.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create spanner instance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetSpannerInstance loads an instance by name.
func (s *Store) GetSpannerInstance(name string) (SpannerInstance, bool, error) {
	var inst SpannerInstance
	err := s.db.QueryRow(
		`SELECT name, project_id, instance_id, config, display_name, node_count, processing_units,
		        state, labels_json, created_at, updated_at
		 FROM spanner_instances WHERE name = ?`, name,
	).Scan(
		&inst.Name, &inst.ProjectID, &inst.InstanceID, &inst.Config, &inst.DisplayName, &inst.NodeCount, &inst.ProcessingUnits,
		&inst.State, &inst.LabelsJSON, &inst.CreatedAt, &inst.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return SpannerInstance{}, false, nil
	}
	if err != nil {
		return SpannerInstance{}, false, fmt.Errorf("get spanner instance: %w", err)
	}
	return inst, true, nil
}

// ListSpannerInstances lists instances under a project.
func (s *Store) ListSpannerInstances(projectID string) ([]SpannerInstance, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, instance_id, config, display_name, node_count, processing_units,
		        state, labels_json, created_at, updated_at
		 FROM spanner_instances WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list spanner instances: %w", err)
	}
	defer rows.Close()
	var out []SpannerInstance
	for rows.Next() {
		var inst SpannerInstance
		if err := rows.Scan(
			&inst.Name, &inst.ProjectID, &inst.InstanceID, &inst.Config, &inst.DisplayName, &inst.NodeCount, &inst.ProcessingUnits,
			&inst.State, &inst.LabelsJSON, &inst.CreatedAt, &inst.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// DeleteSpannerInstance deletes an instance and nested databases/sessions.
func (s *Store) DeleteSpannerInstance(name string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`DELETE FROM spanner_rows WHERE database_name IN (SELECT name FROM spanner_databases WHERE instance_name = ?)`,
		name,
	); err != nil {
		return false, fmt.Errorf("delete spanner rows: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM spanner_sessions WHERE database_name IN (SELECT name FROM spanner_databases WHERE instance_name = ?)`,
		name,
	); err != nil {
		return false, fmt.Errorf("delete spanner sessions: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM spanner_databases WHERE instance_name = ?`, name); err != nil {
		return false, fmt.Errorf("delete spanner databases: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM spanner_instances WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete spanner instance: %w", err)
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

// CreateSpannerDatabase inserts a database. created=false means already exists.
func (s *Store) CreateSpannerDatabase(db SpannerDatabase) (bool, error) {
	if db.Name == "" || db.InstanceName == "" || db.DatabaseID == "" {
		return false, fmt.Errorf("spanner database requires name, instance name, and database id")
	}
	if db.State == "" {
		db.State = "READY"
	}
	if db.Dialect == "" {
		db.Dialect = "GOOGLE_STANDARD_SQL"
	}
	if db.ExtraStatementsJSON == "" {
		db.ExtraStatementsJSON = "[]"
	}
	if db.DDLStatementsJSON == "" {
		db.DDLStatementsJSON = "[]"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if db.CreatedAt == "" {
		db.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO spanner_databases
		 (name, instance_name, project_id, instance_id, database_id, state, create_statement,
		  extra_statements_json, ddl_statements_json, dialect, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		db.Name, db.InstanceName, db.ProjectID, db.InstanceID, db.DatabaseID, db.State, db.CreateStatement,
		db.ExtraStatementsJSON, db.DDLStatementsJSON, db.Dialect, db.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create spanner database: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetSpannerDatabase loads a database by name.
func (s *Store) GetSpannerDatabase(name string) (SpannerDatabase, bool, error) {
	var db SpannerDatabase
	err := s.db.QueryRow(
		`SELECT name, instance_name, project_id, instance_id, database_id, state, create_statement,
		        extra_statements_json, COALESCE(ddl_statements_json, '[]'), dialect, created_at
		 FROM spanner_databases WHERE name = ?`, name,
	).Scan(
		&db.Name, &db.InstanceName, &db.ProjectID, &db.InstanceID, &db.DatabaseID, &db.State, &db.CreateStatement,
		&db.ExtraStatementsJSON, &db.DDLStatementsJSON, &db.Dialect, &db.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return SpannerDatabase{}, false, nil
	}
	if err != nil {
		return SpannerDatabase{}, false, fmt.Errorf("get spanner database: %w", err)
	}
	return db, true, nil
}

// ListSpannerDatabases lists databases under an instance.
func (s *Store) ListSpannerDatabases(instanceName string) ([]SpannerDatabase, error) {
	rows, err := s.db.Query(
		`SELECT name, instance_name, project_id, instance_id, database_id, state, create_statement,
		        extra_statements_json, COALESCE(ddl_statements_json, '[]'), dialect, created_at
		 FROM spanner_databases WHERE instance_name = ? ORDER BY name`,
		instanceName,
	)
	if err != nil {
		return nil, fmt.Errorf("list spanner databases: %w", err)
	}
	defer rows.Close()
	var out []SpannerDatabase
	for rows.Next() {
		var db SpannerDatabase
		if err := rows.Scan(
			&db.Name, &db.InstanceName, &db.ProjectID, &db.InstanceID, &db.DatabaseID, &db.State, &db.CreateStatement,
			&db.ExtraStatementsJSON, &db.DDLStatementsJSON, &db.Dialect, &db.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, db)
	}
	return out, rows.Err()
}

// AppendSpannerDDL stores DDL statements theatre-side (no schema engine).
func (s *Store) AppendSpannerDDL(name string, statements []string) (SpannerDatabase, bool, error) {
	db, ok, err := s.GetSpannerDatabase(name)
	if err != nil || !ok {
		return SpannerDatabase{}, ok, err
	}
	var existing []string
	if db.DDLStatementsJSON != "" && db.DDLStatementsJSON != "[]" {
		_ = json.Unmarshal([]byte(db.DDLStatementsJSON), &existing)
	}
	existing = append(existing, statements...)
	raw, err := json.Marshal(existing)
	if err != nil {
		return SpannerDatabase{}, false, err
	}
	res, err := s.db.Exec(
		`UPDATE spanner_databases SET ddl_statements_json = ? WHERE name = ?`,
		string(raw), name,
	)
	if err != nil {
		return SpannerDatabase{}, false, fmt.Errorf("append spanner ddl: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return SpannerDatabase{}, false, err
	}
	if n == 0 {
		return SpannerDatabase{}, false, nil
	}
	return s.GetSpannerDatabase(name)
}

// DeleteSpannerDatabase deletes a database and its sessions.
func (s *Store) DeleteSpannerDatabase(name string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM spanner_rows WHERE database_name = ?`, name); err != nil {
		return false, fmt.Errorf("delete spanner rows: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM spanner_sessions WHERE database_name = ?`, name); err != nil {
		return false, fmt.Errorf("delete spanner sessions: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM spanner_databases WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete spanner database: %w", err)
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

// CreateSpannerSession inserts a session theatre row.
func (s *Store) CreateSpannerSession(sess SpannerSession) (SpannerSession, bool, error) {
	if sess.DatabaseName == "" || sess.ProjectID == "" || sess.InstanceID == "" || sess.DatabaseID == "" {
		return SpannerSession{}, false, fmt.Errorf("spanner session requires database and ids")
	}
	if sess.SessionID == "" {
		sess.SessionID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	if sess.Name == "" {
		sess.Name = sess.DatabaseName + "/sessions/" + sess.SessionID
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if sess.CreatedAt == "" {
		sess.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO spanner_sessions
		 (name, database_name, project_id, instance_id, database_id, session_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.Name, sess.DatabaseName, sess.ProjectID, sess.InstanceID, sess.DatabaseID, sess.SessionID, sess.CreatedAt,
	)
	if err != nil {
		return SpannerSession{}, false, fmt.Errorf("create spanner session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return SpannerSession{}, false, err
	}
	if n == 0 {
		return SpannerSession{}, false, nil
	}
	return sess, true, nil
}

// GetSpannerSession loads a session by name.
func (s *Store) GetSpannerSession(name string) (SpannerSession, bool, error) {
	var sess SpannerSession
	err := s.db.QueryRow(
		`SELECT name, database_name, project_id, instance_id, database_id, session_id, created_at
		 FROM spanner_sessions WHERE name = ?`, name,
	).Scan(
		&sess.Name, &sess.DatabaseName, &sess.ProjectID, &sess.InstanceID, &sess.DatabaseID, &sess.SessionID, &sess.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return SpannerSession{}, false, nil
	}
	if err != nil {
		return SpannerSession{}, false, fmt.Errorf("get spanner session: %w", err)
	}
	return sess, true, nil
}

// SpannerRow is a SQLite-backed Spanner mutation insert theatre row.
type SpannerRow struct {
	ID           int64
	DatabaseName string
	TableName    string
	KeyJSON      string
	ColumnsJSON  string
	ValuesJSON   string
	CreatedAt    string
}

// InsertSpannerRows stores mutation insert rows for a database/table.
func (s *Store) InsertSpannerRows(databaseName, tableName string, columns []string, valueRows [][]string) error {
	databaseName = strings.TrimSpace(databaseName)
	tableName = strings.TrimSpace(tableName)
	if databaseName == "" || tableName == "" {
		return fmt.Errorf("spanner insert requires database and table")
	}
	if len(columns) == 0 {
		return fmt.Errorf("spanner insert requires columns")
	}
	if _, ok, err := s.GetSpannerDatabase(databaseName); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("database not found")
	}
	colsRaw, err := json.Marshal(columns)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, vals := range valueRows {
		if len(vals) != len(columns) {
			return fmt.Errorf("value count %d does not match columns %d", len(vals), len(columns))
		}
		keyVals := vals
		if len(vals) > 0 {
			keyVals = vals[:1]
		}
		keyRaw, err := json.Marshal(keyVals)
		if err != nil {
			return err
		}
		valRaw, err := json.Marshal(vals)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO spanner_rows (database_name, table_name, key_json, columns_json, values_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			databaseName, tableName, string(keyRaw), string(colsRaw), string(valRaw), now,
		); err != nil {
			return fmt.Errorf("insert spanner row: %w", err)
		}
	}
	return tx.Commit()
}

// ListSpannerRows returns inserted rows for a table.
func (s *Store) ListSpannerRows(databaseName, tableName string) ([]SpannerRow, error) {
	rows, err := s.db.Query(
		`SELECT id, database_name, table_name, key_json, columns_json, values_json, created_at
		 FROM spanner_rows WHERE database_name = ? AND table_name = ? ORDER BY id`,
		databaseName, tableName,
	)
	if err != nil {
		return nil, fmt.Errorf("list spanner rows: %w", err)
	}
	defer rows.Close()
	var out []SpannerRow
	for rows.Next() {
		var r SpannerRow
		if err := rows.Scan(&r.ID, &r.DatabaseName, &r.TableName, &r.KeyJSON, &r.ColumnsJSON, &r.ValuesJSON, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
