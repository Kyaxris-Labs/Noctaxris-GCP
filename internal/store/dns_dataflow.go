package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const dnsDataflowSchema = `
CREATE TABLE IF NOT EXISTS dns_managed_zones (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  zone_id TEXT NOT NULL,
  numeric_id TEXT NOT NULL,
  dns_name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  visibility TEXT NOT NULL DEFAULT 'public',
  name_servers_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, zone_id)
);

CREATE INDEX IF NOT EXISTS idx_dns_zones_project ON dns_managed_zones (project_id);

CREATE TABLE IF NOT EXISTS dns_rrsets (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  zone_name TEXT NOT NULL,
  zone_id TEXT NOT NULL,
  rrset_name TEXT NOT NULL,
  rrset_type TEXT NOT NULL,
  ttl INTEGER NOT NULL DEFAULT 300,
  rrdatas_json TEXT NOT NULL DEFAULT '[]',
  UNIQUE (zone_name, rrset_name, rrset_type)
);

CREATE INDEX IF NOT EXISTS idx_dns_rrsets_zone ON dns_rrsets (zone_name);

CREATE TABLE IF NOT EXISTS dataflow_jobs (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  job_id TEXT NOT NULL,
  job_name TEXT NOT NULL DEFAULT '',
  job_type TEXT NOT NULL DEFAULT 'JOB_TYPE_BATCH',
  current_state TEXT NOT NULL DEFAULT 'JOB_STATE_RUNNING',
  job_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  current_state_time TEXT NOT NULL DEFAULT '',
  start_time TEXT NOT NULL DEFAULT '',
  UNIQUE (project_id, location, job_id)
);

CREATE INDEX IF NOT EXISTS idx_dataflow_jobs_project_loc ON dataflow_jobs (project_id, location);
CREATE INDEX IF NOT EXISTS idx_dataflow_jobs_project ON dataflow_jobs (project_id);
`

func (s *Store) migrateDNSDataflow() error {
	if _, err := s.db.Exec(dnsDataflowSchema); err != nil {
		return fmt.Errorf("apply dns/dataflow schema: %w", err)
	}
	return nil
}

// DNSManagedZone is a Cloud DNS managedZones row.
type DNSManagedZone struct {
	Name            string
	ProjectID       string
	ZoneID          string
	NumericID       string
	DNSName         string
	Description     string
	Visibility      string
	NameServersJSON string
	CreatedAt       string
}

// DNSRrset is a Cloud DNS resourceRecordSet row.
type DNSRrset struct {
	ID         string
	ProjectID  string
	ZoneName   string
	ZoneID     string
	RrsetName  string
	RrsetType  string
	TTL        int64
	RrdatasJSON string
}

// DataflowJob is a Dataflow jobs theatre row.
type DataflowJob struct {
	Name             string
	ProjectID        string
	Location         string
	JobID            string
	JobName          string
	JobType          string
	CurrentState     string
	JobJSON          string
	CreatedAt        string
	CurrentStateTime string
	StartTime        string
}

// CreateDNSManagedZone inserts a managed zone. created=false means already exists.
func (s *Store) CreateDNSManagedZone(z DNSManagedZone) (bool, error) {
	if z.Name == "" || z.ProjectID == "" || z.ZoneID == "" {
		return false, fmt.Errorf("dns managed zone name/project/zone id required")
	}
	if z.Visibility == "" {
		z.Visibility = "public"
	}
	if z.NameServersJSON == "" {
		z.NameServersJSON = "[]"
	}
	if z.CreatedAt == "" {
		z.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if z.NumericID == "" {
		z.NumericID = strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO dns_managed_zones
		 (name, project_id, zone_id, numeric_id, dns_name, description, visibility, name_servers_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		z.Name, z.ProjectID, z.ZoneID, z.NumericID, z.DNSName, z.Description,
		z.Visibility, z.NameServersJSON, z.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create dns managed zone: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetDNSManagedZone returns a zone by resource name.
func (s *Store) GetDNSManagedZone(name string) (DNSManagedZone, bool, error) {
	var z DNSManagedZone
	err := s.db.QueryRow(
		`SELECT name, project_id, zone_id, numeric_id, dns_name, description, visibility, name_servers_json, created_at
		 FROM dns_managed_zones WHERE name = ?`, name,
	).Scan(&z.Name, &z.ProjectID, &z.ZoneID, &z.NumericID, &z.DNSName, &z.Description,
		&z.Visibility, &z.NameServersJSON, &z.CreatedAt)
	if err == sql.ErrNoRows {
		return DNSManagedZone{}, false, nil
	}
	if err != nil {
		return DNSManagedZone{}, false, fmt.Errorf("get dns managed zone: %w", err)
	}
	return z, true, nil
}

// GetDNSManagedZoneByProjectID returns a zone by project + zone id (or numeric id).
func (s *Store) GetDNSManagedZoneByProjectID(projectID, zoneID string) (DNSManagedZone, bool, error) {
	var z DNSManagedZone
	err := s.db.QueryRow(
		`SELECT name, project_id, zone_id, numeric_id, dns_name, description, visibility, name_servers_json, created_at
		 FROM dns_managed_zones WHERE project_id = ? AND (zone_id = ? OR numeric_id = ?)`,
		projectID, zoneID, zoneID,
	).Scan(&z.Name, &z.ProjectID, &z.ZoneID, &z.NumericID, &z.DNSName, &z.Description,
		&z.Visibility, &z.NameServersJSON, &z.CreatedAt)
	if err == sql.ErrNoRows {
		return DNSManagedZone{}, false, nil
	}
	if err != nil {
		return DNSManagedZone{}, false, fmt.Errorf("get dns managed zone by id: %w", err)
	}
	return z, true, nil
}

// ListDNSManagedZones lists zones for a project.
func (s *Store) ListDNSManagedZones(projectID string) ([]DNSManagedZone, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, zone_id, numeric_id, dns_name, description, visibility, name_servers_json, created_at
		 FROM dns_managed_zones WHERE project_id = ? ORDER BY zone_id`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list dns managed zones: %w", err)
	}
	defer rows.Close()
	var out []DNSManagedZone
	for rows.Next() {
		var z DNSManagedZone
		if err := rows.Scan(&z.Name, &z.ProjectID, &z.ZoneID, &z.NumericID, &z.DNSName, &z.Description,
			&z.Visibility, &z.NameServersJSON, &z.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan dns managed zone: %w", err)
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// DeleteDNSManagedZone deletes a zone and its rrsets. ok=false if missing.
func (s *Store) DeleteDNSManagedZone(name string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM dns_rrsets WHERE zone_name = ?`, name); err != nil {
		return false, fmt.Errorf("delete dns rrsets for zone: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM dns_managed_zones WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete dns managed zone: %w", err)
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

// CreateDNSRrset inserts a record set. created=false means already exists.
func (s *Store) CreateDNSRrset(r DNSRrset) (bool, error) {
	if r.ZoneName == "" || r.RrsetName == "" || r.RrsetType == "" {
		return false, fmt.Errorf("dns rrset zone/name/type required")
	}
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.RrdatasJSON == "" {
		r.RrdatasJSON = "[]"
	}
	if r.TTL <= 0 {
		r.TTL = 300
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO dns_rrsets
		 (id, project_id, zone_name, zone_id, rrset_name, rrset_type, ttl, rrdatas_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.ProjectID, r.ZoneName, r.ZoneID, r.RrsetName, strings.ToUpper(r.RrsetType), r.TTL, r.RrdatasJSON,
	)
	if err != nil {
		return false, fmt.Errorf("create dns rrset: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetDNSRrset returns a record set by zone + name + type.
func (s *Store) GetDNSRrset(zoneName, rrsetName, rrsetType string) (DNSRrset, bool, error) {
	var r DNSRrset
	err := s.db.QueryRow(
		`SELECT id, project_id, zone_name, zone_id, rrset_name, rrset_type, ttl, rrdatas_json
		 FROM dns_rrsets WHERE zone_name = ? AND rrset_name = ? AND rrset_type = ?`,
		zoneName, rrsetName, strings.ToUpper(rrsetType),
	).Scan(&r.ID, &r.ProjectID, &r.ZoneName, &r.ZoneID, &r.RrsetName, &r.RrsetType, &r.TTL, &r.RrdatasJSON)
	if err == sql.ErrNoRows {
		return DNSRrset{}, false, nil
	}
	if err != nil {
		return DNSRrset{}, false, fmt.Errorf("get dns rrset: %w", err)
	}
	return r, true, nil
}

// ListDNSRrsets lists record sets for a zone.
func (s *Store) ListDNSRrsets(zoneName string) ([]DNSRrset, error) {
	rows, err := s.db.Query(
		`SELECT id, project_id, zone_name, zone_id, rrset_name, rrset_type, ttl, rrdatas_json
		 FROM dns_rrsets WHERE zone_name = ? ORDER BY rrset_name, rrset_type`, zoneName,
	)
	if err != nil {
		return nil, fmt.Errorf("list dns rrsets: %w", err)
	}
	defer rows.Close()
	var out []DNSRrset
	for rows.Next() {
		var r DNSRrset
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.ZoneName, &r.ZoneID, &r.RrsetName, &r.RrsetType, &r.TTL, &r.RrdatasJSON); err != nil {
			return nil, fmt.Errorf("scan dns rrset: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteDNSRrset deletes a record set. ok=false if missing.
func (s *Store) DeleteDNSRrset(zoneName, rrsetName, rrsetType string) (bool, error) {
	res, err := s.db.Exec(
		`DELETE FROM dns_rrsets WHERE zone_name = ? AND rrset_name = ? AND rrset_type = ?`,
		zoneName, rrsetName, strings.ToUpper(rrsetType),
	)
	if err != nil {
		return false, fmt.Errorf("delete dns rrset: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// UpsertDNSRrset inserts or replaces a record set by zone + name + type.
func (s *Store) UpsertDNSRrset(r DNSRrset) error {
	if r.ZoneName == "" || r.RrsetName == "" || r.RrsetType == "" {
		return fmt.Errorf("dns rrset zone/name/type required")
	}
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.RrdatasJSON == "" {
		r.RrdatasJSON = "[]"
	}
	if r.TTL <= 0 {
		r.TTL = 300
	}
	rrType := strings.ToUpper(r.RrsetType)
	_, err := s.db.Exec(
		`INSERT INTO dns_rrsets
		 (id, project_id, zone_name, zone_id, rrset_name, rrset_type, ttl, rrdatas_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(zone_name, rrset_name, rrset_type) DO UPDATE SET
		   ttl = excluded.ttl,
		   rrdatas_json = excluded.rrdatas_json,
		   project_id = excluded.project_id,
		   zone_id = excluded.zone_id`,
		r.ID, r.ProjectID, r.ZoneName, r.ZoneID, r.RrsetName, rrType, r.TTL, r.RrdatasJSON,
	)
	if err != nil {
		return fmt.Errorf("upsert dns rrset: %w", err)
	}
	return nil
}

// CreateDataflowJob inserts a job. created=false means already exists.
func (s *Store) CreateDataflowJob(j DataflowJob) (bool, error) {
	if j.Name == "" || j.ProjectID == "" || j.Location == "" || j.JobID == "" {
		return false, fmt.Errorf("dataflow job name/project/location/id required")
	}
	if j.JobType == "" {
		j.JobType = "JOB_TYPE_BATCH"
	}
	if j.CurrentState == "" {
		j.CurrentState = "JOB_STATE_RUNNING"
	}
	if j.JobJSON == "" {
		j.JobJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if j.CreatedAt == "" {
		j.CreatedAt = now
	}
	if j.CurrentStateTime == "" {
		j.CurrentStateTime = now
	}
	if j.StartTime == "" {
		j.StartTime = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO dataflow_jobs
		 (name, project_id, location, job_id, job_name, job_type, current_state, job_json, created_at, current_state_time, start_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.Name, j.ProjectID, j.Location, j.JobID, j.JobName, j.JobType, j.CurrentState,
		j.JobJSON, j.CreatedAt, j.CurrentStateTime, j.StartTime,
	)
	if err != nil {
		return false, fmt.Errorf("create dataflow job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetDataflowJob returns a job by resource name.
func (s *Store) GetDataflowJob(name string) (DataflowJob, bool, error) {
	var j DataflowJob
	err := s.db.QueryRow(
		`SELECT name, project_id, location, job_id, job_name, job_type, current_state, job_json, created_at, current_state_time, start_time
		 FROM dataflow_jobs WHERE name = ?`, name,
	).Scan(&j.Name, &j.ProjectID, &j.Location, &j.JobID, &j.JobName, &j.JobType, &j.CurrentState,
		&j.JobJSON, &j.CreatedAt, &j.CurrentStateTime, &j.StartTime)
	if err == sql.ErrNoRows {
		return DataflowJob{}, false, nil
	}
	if err != nil {
		return DataflowJob{}, false, fmt.Errorf("get dataflow job: %w", err)
	}
	return j, true, nil
}

// AdvanceDataflowJobToDone flips RUNNING jobs to DONE theatre and returns the updated row.
func (s *Store) AdvanceDataflowJobToDone(name string) (DataflowJob, bool, error) {
	j, ok, err := s.GetDataflowJob(name)
	if err != nil || !ok {
		return DataflowJob{}, ok, err
	}
	if j.CurrentState != "JOB_STATE_RUNNING" {
		return j, true, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	j.CurrentState = "JOB_STATE_DONE"
	j.CurrentStateTime = now
	_, err = s.db.Exec(
		`UPDATE dataflow_jobs SET current_state = ?, current_state_time = ? WHERE name = ?`,
		j.CurrentState, j.CurrentStateTime, name,
	)
	if err != nil {
		return DataflowJob{}, false, fmt.Errorf("advance dataflow job: %w", err)
	}
	return j, true, nil
}

// ListDataflowJobs lists jobs for a project and location.
func (s *Store) ListDataflowJobs(projectID, location string) ([]DataflowJob, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, job_id, job_name, job_type, current_state, job_json, created_at, current_state_time, start_time
		 FROM dataflow_jobs WHERE project_id = ? AND location = ? ORDER BY created_at DESC`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list dataflow jobs: %w", err)
	}
	defer rows.Close()
	return scanDataflowJobs(rows)
}

// ListDataflowJobsProject lists all jobs for a project (any location).
func (s *Store) ListDataflowJobsProject(projectID string) ([]DataflowJob, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, job_id, job_name, job_type, current_state, job_json, created_at, current_state_time, start_time
		 FROM dataflow_jobs WHERE project_id = ? ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list dataflow jobs project: %w", err)
	}
	defer rows.Close()
	return scanDataflowJobs(rows)
}

func scanDataflowJobs(rows *sql.Rows) ([]DataflowJob, error) {
	var out []DataflowJob
	for rows.Next() {
		var j DataflowJob
		if err := rows.Scan(&j.Name, &j.ProjectID, &j.Location, &j.JobID, &j.JobName, &j.JobType, &j.CurrentState,
			&j.JobJSON, &j.CreatedAt, &j.CurrentStateTime, &j.StartTime); err != nil {
			return nil, fmt.Errorf("scan dataflow job: %w", err)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// NewDataflowJobID returns a unique job id for theatre creates.
func NewDataflowJobID() string {
	return "job-" + uuid.NewString()
}

// DNSZoneResourceName builds the lab store key for a managed zone.
func DNSZoneResourceName(projectID, zoneID string) string {
	return fmt.Sprintf("projects/%s/managedZones/%s", projectID, zoneID)
}

// DataflowJobResourceName builds the lab store key for a regional job.
func DataflowJobResourceName(projectID, location, jobID string) string {
	return fmt.Sprintf("projects/%s/locations/%s/jobs/%s", projectID, location, jobID)
}

// MarshalStringSlice encodes a string slice as JSON (empty -> []).
func MarshalStringSlice(vals []string) string {
	if vals == nil {
		vals = []string{}
	}
	raw, err := json.Marshal(vals)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

// UnmarshalStringSlice decodes a JSON string array.
func UnmarshalStringSlice(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}
