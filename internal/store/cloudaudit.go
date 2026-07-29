package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/audit"
)

// Cloud Audit Logs lab log id suffixes (URL-encoded slash as in GCP log names).
const (
	CloudAuditLogIDActivity     = "cloudaudit.googleapis.com%2Factivity"
	CloudAuditLogIDDataAccess   = "cloudaudit.googleapis.com%2Fdata_access"
	CloudAuditLogIDSystemEvent  = "cloudaudit.googleapis.com%2Fsystem_event"
	CloudAuditProtoPayloadType  = "type.googleapis.com/google.cloud.audit.AuditLog"
)

const cloudAuditSchema = `
CREATE TABLE IF NOT EXISTS cloud_audit_entries (
  insert_id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  log_name TEXT NOT NULL,
  severity TEXT NOT NULL DEFAULT 'NOTICE',
  timestamp TEXT NOT NULL,
  proto_payload_json TEXT NOT NULL,
  resource_json TEXT NOT NULL DEFAULT '{}',
  service_name TEXT NOT NULL DEFAULT '',
  method_name TEXT NOT NULL DEFAULT '',
  principal_email TEXT NOT NULL DEFAULT '',
  resource_name TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_cloud_audit_project_ts
  ON cloud_audit_entries (project_id, timestamp, insert_id);
CREATE INDEX IF NOT EXISTS idx_cloud_audit_log_name
  ON cloud_audit_entries (project_id, log_name);
`

// CloudAuditEntry is a stored Cloud Audit Logs–shaped Logging entry (protoPayload lite).
type CloudAuditEntry struct {
	InsertID         string
	ProjectID        string
	LogName          string
	Severity         string
	Timestamp        string
	ProtoPayloadJSON string
	ResourceJSON     string
	ServiceName      string
	MethodName       string
	PrincipalEmail   string
	ResourceName     string
}

// ListCloudAuditFilter is a lab subset for listing CAL entries.
type ListCloudAuditFilter struct {
	ProjectID    string
	ExactLogName string
	TimestampGTE string
	TimestampLT  string
	PageSize     int
	Offset       int
}

// migrateCloudAudit creates the Cloud Audit Logs inject table.
// Wire from Store.migrate (Open) as: if err := s.migrateCloudAudit(); err != nil { return err }
func (s *Store) migrateCloudAudit() error {
	if _, err := s.db.Exec(cloudAuditSchema); err != nil {
		return fmt.Errorf("migrate cloud audit: %w", err)
	}
	return nil
}

// CloudAuditLogName builds projects/{project}/logs/{logID}.
func CloudAuditLogName(projectID, logID string) string {
	return "projects/" + projectID + "/logs/" + logID
}

// IsCloudAuditLogName reports whether logName is a Cloud Audit Logs name.
func IsCloudAuditLogName(logName string) bool {
	return strings.Contains(logName, "cloudaudit.googleapis.com")
}

// WriteCloudAuditEntries inserts CAL entries. Duplicate insert_id rows are ignored.
func (s *Store) WriteCloudAuditEntries(entries []CloudAuditEntry) error {
	if err := s.migrateCloudAudit(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO cloud_audit_entries
		 (insert_id, project_id, log_name, severity, timestamp, proto_payload_json, resource_json,
		  service_name, method_name, principal_email, resource_name)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare cloud audit insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		if e.InsertID == "" || e.ProjectID == "" || e.LogName == "" {
			return fmt.Errorf("cloud audit entry requires insert_id, project_id, and log_name")
		}
		if e.Severity == "" {
			e.Severity = "NOTICE"
		}
		if e.Timestamp == "" {
			e.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if e.ProtoPayloadJSON == "" {
			e.ProtoPayloadJSON = "{}"
		}
		if e.ResourceJSON == "" {
			e.ResourceJSON = "{}"
		}
		if e.ServiceName == "" || e.MethodName == "" || e.PrincipalEmail == "" || e.ResourceName == "" {
			svc, method, principal, resource := extractProtoLite(e.ProtoPayloadJSON)
			if e.ServiceName == "" {
				e.ServiceName = svc
			}
			if e.MethodName == "" {
				e.MethodName = method
			}
			if e.PrincipalEmail == "" {
				e.PrincipalEmail = principal
			}
			if e.ResourceName == "" {
				e.ResourceName = resource
			}
		}
		if _, err := stmt.Exec(
			e.InsertID, e.ProjectID, e.LogName, e.Severity, e.Timestamp,
			e.ProtoPayloadJSON, e.ResourceJSON,
			e.ServiceName, e.MethodName, e.PrincipalEmail, e.ResourceName,
		); err != nil {
			return fmt.Errorf("insert cloud audit entry: %w", err)
		}
	}
	return tx.Commit()
}

// ListCloudAuditEntries returns matching CAL rows ordered by timestamp ascending.
func (s *Store) ListCloudAuditEntries(f ListCloudAuditFilter) ([]CloudAuditEntry, error) {
	if err := s.migrateCloudAudit(); err != nil {
		return nil, err
	}
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

	q := `SELECT insert_id, project_id, log_name, severity, timestamp, proto_payload_json, resource_json,
	             service_name, method_name, principal_email, resource_name
	      FROM cloud_audit_entries WHERE project_id = ?`
	args := []any{f.ProjectID}
	if f.ExactLogName != "" {
		q += ` AND log_name = ?`
		args = append(args, f.ExactLogName)
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
		return nil, fmt.Errorf("list cloud audit entries: %w", err)
	}
	defer rows.Close()

	var out []CloudAuditEntry
	for rows.Next() {
		var e CloudAuditEntry
		if err := rows.Scan(
			&e.InsertID, &e.ProjectID, &e.LogName, &e.Severity, &e.Timestamp,
			&e.ProtoPayloadJSON, &e.ResourceJSON,
			&e.ServiceName, &e.MethodName, &e.PrincipalEmail, &e.ResourceName,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListCloudAuditAsLogEntries maps CAL rows into LogEntry for Logging entries:list.
func (s *Store) ListCloudAuditAsLogEntries(f ListCloudAuditFilter) ([]LogEntry, error) {
	rows, err := s.ListCloudAuditEntries(f)
	if err != nil {
		return nil, err
	}
	out := make([]LogEntry, 0, len(rows))
	for _, e := range rows {
		out = append(out, LogEntry{
			InsertID:     e.InsertID,
			ProjectID:    e.ProjectID,
			LogName:      e.LogName,
			Severity:     e.Severity,
			Timestamp:    e.Timestamp,
			PayloadJSON:  string(mustProtoPayloadJSON(e.ProtoPayloadJSON)),
			ResourceJSON: e.ResourceJSON,
		})
	}
	return out, nil
}

// ListCloudAuditLogNames returns distinct CAL log names under a project.
func (s *Store) ListCloudAuditLogNames(projectID string) ([]string, error) {
	if err := s.migrateCloudAudit(); err != nil {
		return nil, err
	}
	if projectID == "" {
		return nil, fmt.Errorf("project id required")
	}
	rows, err := s.db.Query(
		`SELECT DISTINCT log_name FROM cloud_audit_entries WHERE project_id = ? ORDER BY log_name`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list cloud audit log names: %w", err)
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

// WriteCloudAuditFromKernelEvent mirrors a live audit.Writer event into the CAL table.
func (s *Store) WriteCloudAuditFromKernelEvent(projectID string, ev audit.Event) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("project id required")
	}
	insertID := ev.InsertID
	if insertID == "" {
		insertID = fmt.Sprintf("live-%d", time.Now().UTC().UnixNano())
	}
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	sev := ev.Severity
	if sev == "" {
		sev = "NOTICE"
	}
	proto := map[string]any{
		"@type":         CloudAuditProtoPayloadType,
		"serviceName":   ev.ServiceName,
		"methodName":    ev.MethodName,
		"resourceName":  ev.ResourceName,
		"authenticationInfo": map[string]any{
			"principalEmail": ev.PrincipalEmail,
		},
	}
	if ev.Permission != "" || ev.Granted != nil {
		authz := map[string]any{"permission": ev.Permission}
		if ev.Granted != nil {
			authz["granted"] = *ev.Granted
		}
		proto["authorizationInfo"] = []any{authz}
	}
	if ev.StatusCode != 0 {
		proto["status"] = map[string]any{"code": ev.StatusCode}
	}
	if ev.RequestID != "" {
		proto["requestMetadata"] = map[string]any{"requestId": ev.RequestID}
	}
	if ev.Message != "" {
		proto["status"] = map[string]any{"code": ev.StatusCode, "message": ev.Message}
	}
	protoJSON, err := json.Marshal(proto)
	if err != nil {
		return fmt.Errorf("marshal protoPayload: %w", err)
	}
	return s.WriteCloudAuditEntries([]CloudAuditEntry{{
		InsertID:         insertID,
		ProjectID:        projectID,
		LogName:          CloudAuditLogName(projectID, CloudAuditLogIDActivity),
		Severity:         sev,
		Timestamp:        ts.UTC().Format(time.RFC3339Nano),
		ProtoPayloadJSON: string(protoJSON),
		ResourceJSON:     `{"type":"audited_resource"}`,
		ServiceName:      ev.ServiceName,
		MethodName:       ev.MethodName,
		PrincipalEmail:   ev.PrincipalEmail,
		ResourceName:     ev.ResourceName,
	}})
}

func mustProtoPayloadJSON(protoPayloadJSON string) []byte {
	if protoPayloadJSON == "" {
		protoPayloadJSON = "{}"
	}
	var proto any
	if err := json.Unmarshal([]byte(protoPayloadJSON), &proto); err != nil {
		proto = map[string]any{}
	}
	b, err := json.Marshal(map[string]any{"protoPayload": proto})
	if err != nil {
		return []byte(`{"protoPayload":{}}`)
	}
	return b
}

func extractProtoLite(protoPayloadJSON string) (service, method, principal, resource string) {
	var p struct {
		ServiceName         string `json:"serviceName"`
		MethodName          string `json:"methodName"`
		ResourceName        string `json:"resourceName"`
		AuthenticationInfo  struct {
			PrincipalEmail string `json:"principalEmail"`
		} `json:"authenticationInfo"`
	}
	_ = json.Unmarshal([]byte(protoPayloadJSON), &p)
	return p.ServiceName, p.MethodName, p.AuthenticationInfo.PrincipalEmail, p.ResourceName
}
