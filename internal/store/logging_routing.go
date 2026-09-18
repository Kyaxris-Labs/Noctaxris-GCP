package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const loggingRoutingSchema = `
CREATE TABLE IF NOT EXISTS log_exclusions (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  exclusion_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  filter TEXT NOT NULL DEFAULT '',
  disabled INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  UNIQUE (project_id, exclusion_id)
);

CREATE TABLE IF NOT EXISTS log_buckets (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  bucket_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, location, bucket_id)
);

CREATE TABLE IF NOT EXISTS log_views (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  bucket_id TEXT NOT NULL,
  view_id TEXT NOT NULL,
  filter TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, location, bucket_id, view_id)
);
`

func (s *Store) migrateLoggingRouting() error {
	if _, err := s.db.Exec(loggingRoutingSchema); err != nil {
		return fmt.Errorf("migrate logging routing: %w", err)
	}
	return nil
}

// LogExclusion is a Cloud Logging exclusion on _Default.
type LogExclusion struct {
	Name        string `json:"name"`
	ProjectID   string `json:"-"`
	ExclusionID string `json:"-"`
	Description string `json:"description"`
	Filter      string `json:"filter"`
	Disabled    bool   `json:"disabled"`
	CreatedAt   string `json:"createTime"`
}

// LogBucket is a Logging bucket (_Required / _Default).
type LogBucket struct {
	Name        string
	ProjectID   string
	Location    string
	BucketID    string
	Description string
	CreatedAt   string
}

// LogView is a Logging view on a bucket.
type LogView struct {
	Name      string `json:"name"`
	ProjectID string `json:"-"`
	Location  string `json:"-"`
	BucketID  string `json:"-"`
	ViewID    string `json:"-"`
	Filter    string `json:"filter"`
	CreatedAt string `json:"createTime"`
}

// EnsureDefaultLogRouting seeds _Required / _Default buckets, sinks, and the
// _Default data-access exclusion. _Required keeps Admin Activity.
func (s *Store) EnsureDefaultLogRouting(projectID string) error {
	if err := s.migrateLoggingRouting(); err != nil {
		return err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("project id required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	loc := "global"
	requiredName := "projects/" + projectID + "/locations/" + loc + "/buckets/_Required"
	defaultName := "projects/" + projectID + "/locations/" + loc + "/buckets/_Default"
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO log_buckets (name, project_id, location, bucket_id, description, created_at)
		 VALUES (?, ?, ?, '_Required', 'Admin Activity', ?)`,
		requiredName, projectID, loc, now,
	); err != nil {
		return err
	}
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO log_buckets (name, project_id, location, bucket_id, description, created_at)
		 VALUES (?, ?, ?, '_Default', 'Default logs', ?)`,
		defaultName, projectID, loc, now,
	); err != nil {
		return err
	}
	if _, _, err := s.CreateLogSink(LogSink{
		ProjectID: projectID, SinkID: "_Required",
		Destination: requiredName,
		Filter:      `LOG_ID("cloudaudit.googleapis.com/activity")`,
	}); err != nil {
		return err
	}
	if _, _, err := s.CreateLogSink(LogSink{
		ProjectID: projectID, SinkID: "_Default",
		Destination: defaultName, Filter: "",
	}); err != nil {
		return err
	}
	if _, _, err := s.CreateLogExclusion(LogExclusion{
		ProjectID: projectID, ExclusionID: "default-data-access",
		Description: "Drop Data Access from _Default",
		Filter:      `LOG_ID("cloudaudit.googleapis.com/data_access")`,
	}); err != nil {
		return err
	}
	if _, _, err := s.CreateLogView(LogView{
		ProjectID: projectID, Location: loc, BucketID: "_Required", ViewID: "_AllLogs",
		Filter: "",
	}); err != nil {
		return err
	}
	return nil
}

func (s *Store) CreateLogExclusion(e LogExclusion) (*LogExclusion, bool, error) {
	if err := s.migrateLoggingRouting(); err != nil {
		return nil, false, err
	}
	e.ProjectID = strings.TrimSpace(e.ProjectID)
	e.ExclusionID = strings.TrimSpace(e.ExclusionID)
	if e.ProjectID == "" || e.ExclusionID == "" {
		return nil, false, fmt.Errorf("project and exclusion id required")
	}
	e.Name = "projects/" + e.ProjectID + "/exclusions/" + e.ExclusionID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	e.CreatedAt = now
	disabled := 0
	if e.Disabled {
		disabled = 1
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO log_exclusions (name, project_id, exclusion_id, description, filter, disabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Name, e.ProjectID, e.ExclusionID, e.Description, e.Filter, disabled, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, false, nil
	}
	return &e, true, nil
}

func (s *Store) GetLogExclusion(name string) (*LogExclusion, bool, error) {
	if err := s.migrateLoggingRouting(); err != nil {
		return nil, false, err
	}
	var e LogExclusion
	var disabled int
	err := s.db.QueryRow(
		`SELECT name, project_id, exclusion_id, description, filter, disabled, created_at FROM log_exclusions WHERE name = ?`,
		name,
	).Scan(&e.Name, &e.ProjectID, &e.ExclusionID, &e.Description, &e.Filter, &disabled, &e.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	e.Disabled = disabled != 0
	return &e, true, nil
}

func (s *Store) ListLogExclusions(projectID string) ([]LogExclusion, error) {
	if err := s.migrateLoggingRouting(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT name, project_id, exclusion_id, description, filter, disabled, created_at
		 FROM log_exclusions WHERE project_id = ? ORDER BY exclusion_id`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LogExclusion
	for rows.Next() {
		var e LogExclusion
		var disabled int
		if err := rows.Scan(&e.Name, &e.ProjectID, &e.ExclusionID, &e.Description, &e.Filter, &disabled, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Disabled = disabled != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) DeleteLogExclusion(name string) (bool, error) {
	if err := s.migrateLoggingRouting(); err != nil {
		return false, err
	}
	res, err := s.db.Exec(`DELETE FROM log_exclusions WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) ListLogBuckets(projectID, location string) ([]LogBucket, error) {
	if err := s.migrateLoggingRouting(); err != nil {
		return nil, err
	}
	q := `SELECT name, project_id, location, bucket_id, description, created_at FROM log_buckets WHERE project_id = ?`
	args := []any{projectID}
	if location != "" && location != "-" {
		q += ` AND location = ?`
		args = append(args, location)
	}
	q += ` ORDER BY bucket_id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LogBucket
	for rows.Next() {
		var b LogBucket
		if err := rows.Scan(&b.Name, &b.ProjectID, &b.Location, &b.BucketID, &b.Description, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetLogBucket(name string) (*LogBucket, bool, error) {
	if err := s.migrateLoggingRouting(); err != nil {
		return nil, false, err
	}
	var b LogBucket
	err := s.db.QueryRow(
		`SELECT name, project_id, location, bucket_id, description, created_at FROM log_buckets WHERE name = ?`,
		name,
	).Scan(&b.Name, &b.ProjectID, &b.Location, &b.BucketID, &b.Description, &b.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &b, true, nil
}

func (s *Store) CreateLogView(v LogView) (*LogView, bool, error) {
	if err := s.migrateLoggingRouting(); err != nil {
		return nil, false, err
	}
	if v.ProjectID == "" || v.Location == "" || v.BucketID == "" || v.ViewID == "" {
		return nil, false, fmt.Errorf("view parent and viewId required")
	}
	v.Name = "projects/" + v.ProjectID + "/locations/" + v.Location + "/buckets/" + v.BucketID + "/views/" + v.ViewID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	v.CreatedAt = now
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO log_views (name, project_id, location, bucket_id, view_id, filter, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		v.Name, v.ProjectID, v.Location, v.BucketID, v.ViewID, v.Filter, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, false, nil
	}
	return &v, true, nil
}

func (s *Store) GetLogView(name string) (*LogView, bool, error) {
	if err := s.migrateLoggingRouting(); err != nil {
		return nil, false, err
	}
	var v LogView
	err := s.db.QueryRow(
		`SELECT name, project_id, location, bucket_id, view_id, filter, created_at FROM log_views WHERE name = ?`,
		name,
	).Scan(&v.Name, &v.ProjectID, &v.Location, &v.BucketID, &v.ViewID, &v.Filter, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &v, true, nil
}

func (s *Store) ListLogViews(parent string) ([]LogView, error) {
	if err := s.migrateLoggingRouting(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT name, project_id, location, bucket_id, view_id, filter, created_at FROM log_views WHERE name LIKE ? ORDER BY view_id`,
		parent+"/views/%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LogView
	for rows.Next() {
		var v LogView
		if err := rows.Scan(&v.Name, &v.ProjectID, &v.Location, &v.BucketID, &v.ViewID, &v.Filter, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
