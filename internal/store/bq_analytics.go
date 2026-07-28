package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

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
