package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// LogEntry is a stored Cloud Logging entry.
type LogEntry struct {
	InsertID     string
	ProjectID    string
	LogName      string
	Severity     string
	Timestamp    string
	PayloadJSON  string
	ResourceJSON string
}

// WriteLogEntries inserts log entries. Duplicate insert_id rows are ignored.
func (s *Store) WriteLogEntries(entries []LogEntry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO log_entries
		 (insert_id, project_id, log_name, severity, timestamp, payload_json, resource_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare log insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		if e.InsertID == "" || e.ProjectID == "" || e.LogName == "" {
			return fmt.Errorf("log entry requires insert_id, project_id, and log_name")
		}
		if e.Severity == "" {
			e.Severity = "DEFAULT"
		}
		if e.Timestamp == "" {
			e.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if e.PayloadJSON == "" {
			e.PayloadJSON = "{}"
		}
		if e.ResourceJSON == "" {
			e.ResourceJSON = "{}"
		}
		if _, err := stmt.Exec(e.InsertID, e.ProjectID, e.LogName, e.Severity, e.Timestamp, e.PayloadJSON, e.ResourceJSON); err != nil {
			return fmt.Errorf("insert log entry: %w", err)
		}
	}
	return tx.Commit()
}

// ListLogEntriesFilter is a lab subset of Logging filters.
type ListLogEntriesFilter struct {
	ProjectID          string
	ExactLogName       string
	TextPayloadContain string
	Severity           string
	TimestampGTE       string
	TimestampLT        string
	PageSize           int
	// Offset is a simple numeric page token (lab).
	Offset int
}

// ListLogEntries returns matching entries ordered by timestamp ascending.
func (s *Store) ListLogEntries(f ListLogEntriesFilter) ([]LogEntry, error) {
	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	if f.ProjectID == "" {
		return nil, fmt.Errorf("project id required")
	}

	q := `SELECT insert_id, project_id, log_name, severity, timestamp, payload_json, resource_json
	      FROM log_entries WHERE project_id = ?`
	args := []any{f.ProjectID}
	if f.ExactLogName != "" {
		q += ` AND log_name = ?`
		args = append(args, f.ExactLogName)
	}
	if f.TextPayloadContain != "" {
		q += ` AND payload_json LIKE ?`
		args = append(args, "%"+escapeLike(f.TextPayloadContain)+"%")
	}
	if f.Severity != "" {
		q += ` AND UPPER(severity) = UPPER(?)`
		args = append(args, f.Severity)
	}
	if f.TimestampGTE != "" {
		q += ` AND timestamp >= ?`
		args = append(args, f.TimestampGTE)
	}
	if f.TimestampLT != "" {
		q += ` AND timestamp < ?`
		args = append(args, f.TimestampLT)
	}
	q += ` ORDER BY timestamp ASC, insert_id ASC LIMIT ? OFFSET ?`
	args = append(args, pageSize, f.Offset)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list log entries: %w", err)
	}
	defer rows.Close()

	var out []LogEntry
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.InsertID, &e.ProjectID, &e.LogName, &e.Severity, &e.Timestamp, &e.PayloadJSON, &e.ResourceJSON); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteLogEntries deletes all entries for a full log name. Returns rows deleted.
func (s *Store) DeleteLogEntries(projectID, logName string) (int64, error) {
	if projectID == "" || logName == "" {
		return 0, fmt.Errorf("project id and log name required")
	}
	res, err := s.db.Exec(
		`DELETE FROM log_entries WHERE project_id = ? AND log_name = ?`,
		projectID, logName,
	)
	if err != nil {
		return 0, fmt.Errorf("delete log entries: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ListLogNames returns distinct log names under a project.
func (s *Store) ListLogNames(projectID string) ([]string, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project id required")
	}
	rows, err := s.db.Query(
		`SELECT DISTINCT log_name FROM log_entries WHERE project_id = ? ORDER BY log_name`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list log names: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// LogSink is a Cloud Logging sink metadata row (no real export).
type LogSink struct {
	Name           string
	ProjectID      string
	SinkID         string
	Destination    string
	Filter         string
	WriterIdentity string
	CreatedAt      string
	UpdatedAt      string
}

// CreateLogSink inserts a sink. created=false when name already exists.
func (s *Store) CreateLogSink(sink LogSink) (*LogSink, bool, error) {
	sink.ProjectID = strings.TrimSpace(sink.ProjectID)
	sink.SinkID = strings.TrimSpace(sink.SinkID)
	if sink.ProjectID == "" || sink.SinkID == "" {
		return nil, false, fmt.Errorf("project and sink id required")
	}
	if sink.Name == "" {
		sink.Name = "projects/" + sink.ProjectID + "/sinks/" + sink.SinkID
	}
	if sink.WriterIdentity == "" {
		sink.WriterIdentity = "serviceAccount:cloud-logs@" + sink.ProjectID + ".iam.gserviceaccount.com"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sink.CreatedAt = now
	sink.UpdatedAt = now
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO log_sinks
		 (name, project_id, sink_id, destination, filter, writer_identity, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sink.Name, sink.ProjectID, sink.SinkID, sink.Destination, sink.Filter, sink.WriterIdentity, now, now,
	)
	if err != nil {
		return nil, false, fmt.Errorf("create log sink: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return &sink, true, nil
}

// GetLogSink loads a sink by resource name.
func (s *Store) GetLogSink(name string) (*LogSink, bool, error) {
	var sk LogSink
	err := s.db.QueryRow(
		`SELECT name, project_id, sink_id, destination, filter, writer_identity, created_at, updated_at
		 FROM log_sinks WHERE name = ?`, name,
	).Scan(&sk.Name, &sk.ProjectID, &sk.SinkID, &sk.Destination, &sk.Filter, &sk.WriterIdentity, &sk.CreatedAt, &sk.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get log sink: %w", err)
	}
	return &sk, true, nil
}

// ListLogSinks lists sinks under a project.
func (s *Store) ListLogSinks(projectID string) ([]LogSink, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, sink_id, destination, filter, writer_identity, created_at, updated_at
		 FROM log_sinks WHERE project_id = ? ORDER BY sink_id`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list log sinks: %w", err)
	}
	defer rows.Close()
	var out []LogSink
	for rows.Next() {
		var sk LogSink
		if err := rows.Scan(&sk.Name, &sk.ProjectID, &sk.SinkID, &sk.Destination, &sk.Filter, &sk.WriterIdentity, &sk.CreatedAt, &sk.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

// UpdateLogSink replaces destination and filter for an existing sink.
func (s *Store) UpdateLogSink(name, destination, filter string) (*LogSink, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE log_sinks SET destination = ?, filter = ?, updated_at = ? WHERE name = ?`,
		destination, filter, now, name,
	)
	if err != nil {
		return nil, false, fmt.Errorf("update log sink: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return s.GetLogSink(name)
}

// DeleteLogSink removes a sink by name.
func (s *Store) DeleteLogSink(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM log_sinks WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete log sink: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
