package store

import (
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

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
