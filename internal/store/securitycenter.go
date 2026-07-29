package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const securityCenterSchema = `
CREATE TABLE IF NOT EXISTS scc_sources (
  name TEXT PRIMARY KEY,
  parent TEXT NOT NULL,
  source_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (parent, source_id)
);

CREATE INDEX IF NOT EXISTS idx_scc_sources_parent
  ON scc_sources (parent);

CREATE TABLE IF NOT EXISTS scc_findings (
  name TEXT PRIMARY KEY,
  parent TEXT NOT NULL,
  source_name TEXT NOT NULL,
  finding_id TEXT NOT NULL,
  resource_name TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  category TEXT NOT NULL DEFAULT '',
  severity TEXT NOT NULL DEFAULT 'SEVERITY_UNSPECIFIED',
  external_uri TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  source_properties_json TEXT NOT NULL DEFAULT '{}',
  event_time TEXT NOT NULL DEFAULT '',
  create_time TEXT NOT NULL,
  UNIQUE (source_name, finding_id),
  FOREIGN KEY (source_name) REFERENCES scc_sources(name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_scc_findings_source
  ON scc_findings (source_name);

CREATE INDEX IF NOT EXISTS idx_scc_findings_parent
  ON scc_findings (parent);
`

// migrateSecurityCenter applies SCC sources/findings tables.
// Wire from Store.migrate() when integrating Cut C (do not leave orphaned).
func (s *Store) migrateSecurityCenter() error {
	if _, err := s.db.Exec(securityCenterSchema); err != nil {
		return fmt.Errorf("apply security center schema: %w", err)
	}
	return nil
}

func (s *Store) ensureSecurityCenter() error {
	return s.migrateSecurityCenter()
}

// SCCSource is a Security Command Center source row.
type SCCSource struct {
	Name        string
	Parent      string
	SourceID    string
	DisplayName string
	Description string
	CreatedAt   string
}

// SCCFinding is a Security Command Center finding row (lite fields).
type SCCFinding struct {
	Name                 string
	Parent               string
	SourceName           string
	FindingID            string
	ResourceName         string
	State                string
	Category             string
	Severity             string
	ExternalURI          string
	Description          string
	SourcePropertiesJSON string
	EventTime            string
	CreateTime           string
}

// SCCSourceResourceName builds {parent}/sources/{sourceId}.
func SCCSourceResourceName(parent, sourceID string) string {
	return strings.TrimSuffix(parent, "/") + "/sources/" + sourceID
}

// SCCFindingResourceName builds {sourceName}/findings/{findingId}.
func SCCFindingResourceName(sourceName, findingID string) string {
	return strings.TrimSuffix(sourceName, "/") + "/findings/" + findingID
}

// CreateSCCSource inserts a source. created=false means already exists.
func (s *Store) CreateSCCSource(src SCCSource) (bool, error) {
	if err := s.ensureSecurityCenter(); err != nil {
		return false, err
	}
	if src.Name == "" || src.Parent == "" || src.SourceID == "" {
		return false, fmt.Errorf("scc source requires name, parent, and source id")
	}
	if src.DisplayName == "" {
		src.DisplayName = src.SourceID
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if src.CreatedAt == "" {
		src.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO scc_sources
		 (name, parent, source_id, display_name, description, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		src.Name, src.Parent, src.SourceID, src.DisplayName, src.Description, src.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create scc source: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetSCCSource loads a source by resource name.
func (s *Store) GetSCCSource(name string) (SCCSource, bool, error) {
	if err := s.ensureSecurityCenter(); err != nil {
		return SCCSource{}, false, err
	}
	var src SCCSource
	err := s.db.QueryRow(
		`SELECT name, parent, source_id, display_name, description, created_at
		 FROM scc_sources WHERE name = ?`, name,
	).Scan(&src.Name, &src.Parent, &src.SourceID, &src.DisplayName, &src.Description, &src.CreatedAt)
	if err == sql.ErrNoRows {
		return SCCSource{}, false, nil
	}
	if err != nil {
		return SCCSource{}, false, fmt.Errorf("get scc source: %w", err)
	}
	return src, true, nil
}

// ListSCCSources lists sources under a parent (organizations/... or projects/...).
func (s *Store) ListSCCSources(parent string) ([]SCCSource, error) {
	if err := s.ensureSecurityCenter(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT name, parent, source_id, display_name, description, created_at
		 FROM scc_sources WHERE parent = ? ORDER BY source_id`, parent,
	)
	if err != nil {
		return nil, fmt.Errorf("list scc sources: %w", err)
	}
	defer rows.Close()
	var out []SCCSource
	for rows.Next() {
		var src SCCSource
		if err := rows.Scan(&src.Name, &src.Parent, &src.SourceID, &src.DisplayName, &src.Description, &src.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan scc source: %w", err)
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// DeleteSCCSource removes a source and its findings.
func (s *Store) DeleteSCCSource(name string) (bool, error) {
	if err := s.ensureSecurityCenter(); err != nil {
		return false, err
	}
	if _, err := s.db.Exec(`DELETE FROM scc_findings WHERE source_name = ?`, name); err != nil {
		return false, fmt.Errorf("delete scc findings for source: %w", err)
	}
	res, err := s.db.Exec(`DELETE FROM scc_sources WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete scc source: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateSCCFinding inserts a finding. created=false means already exists.
func (s *Store) CreateSCCFinding(f SCCFinding) (bool, error) {
	if err := s.ensureSecurityCenter(); err != nil {
		return false, err
	}
	if f.Name == "" || f.Parent == "" || f.SourceName == "" || f.FindingID == "" {
		return false, fmt.Errorf("scc finding requires name, parent, source, and finding id")
	}
	if f.State == "" {
		f.State = "ACTIVE"
	}
	if f.Severity == "" {
		f.Severity = "SEVERITY_UNSPECIFIED"
	}
	if f.SourcePropertiesJSON == "" {
		f.SourcePropertiesJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if f.CreateTime == "" {
		f.CreateTime = now
	}
	if f.EventTime == "" {
		f.EventTime = f.CreateTime
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO scc_findings
		 (name, parent, source_name, finding_id, resource_name, state, category, severity,
		  external_uri, description, source_properties_json, event_time, create_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.Name, f.Parent, f.SourceName, f.FindingID, f.ResourceName, f.State, f.Category, f.Severity,
		f.ExternalURI, f.Description, f.SourcePropertiesJSON, f.EventTime, f.CreateTime,
	)
	if err != nil {
		return false, fmt.Errorf("create scc finding: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetSCCFinding loads a finding by resource name.
func (s *Store) GetSCCFinding(name string) (SCCFinding, bool, error) {
	if err := s.ensureSecurityCenter(); err != nil {
		return SCCFinding{}, false, err
	}
	var f SCCFinding
	err := s.db.QueryRow(
		`SELECT name, parent, source_name, finding_id, resource_name, state, category, severity,
		        external_uri, description, source_properties_json, event_time, create_time
		 FROM scc_findings WHERE name = ?`, name,
	).Scan(&f.Name, &f.Parent, &f.SourceName, &f.FindingID, &f.ResourceName, &f.State, &f.Category, &f.Severity,
		&f.ExternalURI, &f.Description, &f.SourcePropertiesJSON, &f.EventTime, &f.CreateTime)
	if err == sql.ErrNoRows {
		return SCCFinding{}, false, nil
	}
	if err != nil {
		return SCCFinding{}, false, fmt.Errorf("get scc finding: %w", err)
	}
	return f, true, nil
}

// ListSCCFindings lists findings under a source resource name.
func (s *Store) ListSCCFindings(sourceName string) ([]SCCFinding, error) {
	if err := s.ensureSecurityCenter(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT name, parent, source_name, finding_id, resource_name, state, category, severity,
		        external_uri, description, source_properties_json, event_time, create_time
		 FROM scc_findings WHERE source_name = ? ORDER BY finding_id`, sourceName,
	)
	if err != nil {
		return nil, fmt.Errorf("list scc findings: %w", err)
	}
	defer rows.Close()
	var out []SCCFinding
	for rows.Next() {
		var f SCCFinding
		if err := rows.Scan(&f.Name, &f.Parent, &f.SourceName, &f.FindingID, &f.ResourceName, &f.State, &f.Category, &f.Severity,
			&f.ExternalURI, &f.Description, &f.SourcePropertiesJSON, &f.EventTime, &f.CreateTime); err != nil {
			return nil, fmt.Errorf("scan scc finding: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpdateSCCFindingState sets finding state (ACTIVE/INACTIVE). ok=false if missing.
func (s *Store) UpdateSCCFindingState(name, state string) (SCCFinding, bool, error) {
	if err := s.ensureSecurityCenter(); err != nil {
		return SCCFinding{}, false, err
	}
	if state == "" {
		return SCCFinding{}, false, fmt.Errorf("state required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE scc_findings SET state = ?, event_time = ? WHERE name = ?`,
		state, now, name,
	)
	if err != nil {
		return SCCFinding{}, false, fmt.Errorf("update scc finding state: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return SCCFinding{}, false, err
	}
	if n == 0 {
		return SCCFinding{}, false, nil
	}
	return s.GetSCCFinding(name)
}

// DeleteSCCFinding removes a finding.
func (s *Store) DeleteSCCFinding(name string) (bool, error) {
	if err := s.ensureSecurityCenter(); err != nil {
		return false, err
	}
	res, err := s.db.Exec(`DELETE FROM scc_findings WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete scc finding: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
