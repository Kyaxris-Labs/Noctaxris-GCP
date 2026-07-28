package store

import (
	"database/sql"
	"fmt"
	"time"
)

const filestoreSchema = `
CREATE TABLE IF NOT EXISTS filestore_instances (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  tier TEXT NOT NULL DEFAULT 'BASIC_HDD',
  state TEXT NOT NULL DEFAULT 'READY',
  labels_json TEXT NOT NULL DEFAULT '{}',
  file_shares_json TEXT NOT NULL DEFAULT '[]',
  networks_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, location, instance_id)
);
`

func (s *Store) migrateFilestore() error {
	if _, err := s.db.Exec(filestoreSchema); err != nil {
		return fmt.Errorf("apply filestore schema: %w", err)
	}
	return nil
}

// FilestoreInstance is a Cloud Filestore instance row (no NFS server).
type FilestoreInstance struct {
	Name           string
	ProjectID      string
	Location       string
	InstanceID     string
	Description    string
	Tier           string
	State          string
	LabelsJSON     string
	FileSharesJSON string
	NetworksJSON   string
	CreatedAt      string
}

// CreateFilestoreInstance inserts a Filestore instance. created=false means already exists.
func (s *Store) CreateFilestoreInstance(inst FilestoreInstance) (bool, error) {
	if inst.Name == "" || inst.ProjectID == "" || inst.Location == "" || inst.InstanceID == "" {
		return false, fmt.Errorf("filestore instance requires name, project, location, and instance id")
	}
	if inst.State == "" {
		inst.State = "READY"
	}
	if inst.Tier == "" {
		inst.Tier = "BASIC_HDD"
	}
	if inst.LabelsJSON == "" {
		inst.LabelsJSON = "{}"
	}
	if inst.FileSharesJSON == "" {
		inst.FileSharesJSON = "[]"
	}
	if inst.NetworksJSON == "" {
		inst.NetworksJSON = "[]"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if inst.CreatedAt == "" {
		inst.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO filestore_instances
		 (name, project_id, location, instance_id, description, tier, state, labels_json,
		  file_shares_json, networks_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.Name, inst.ProjectID, inst.Location, inst.InstanceID, inst.Description, inst.Tier, inst.State,
		inst.LabelsJSON, inst.FileSharesJSON, inst.NetworksJSON, inst.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create filestore instance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetFilestoreInstance loads an instance by name.
func (s *Store) GetFilestoreInstance(name string) (FilestoreInstance, bool, error) {
	var inst FilestoreInstance
	err := s.db.QueryRow(
		`SELECT name, project_id, location, instance_id, description, tier, state, labels_json,
		        file_shares_json, networks_json, created_at
		 FROM filestore_instances WHERE name = ?`, name,
	).Scan(
		&inst.Name, &inst.ProjectID, &inst.Location, &inst.InstanceID, &inst.Description, &inst.Tier, &inst.State,
		&inst.LabelsJSON, &inst.FileSharesJSON, &inst.NetworksJSON, &inst.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return FilestoreInstance{}, false, nil
	}
	if err != nil {
		return FilestoreInstance{}, false, fmt.Errorf("get filestore instance: %w", err)
	}
	return inst, true, nil
}

// ListFilestoreInstances lists instances under a project location.
func (s *Store) ListFilestoreInstances(projectID, location string) ([]FilestoreInstance, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, instance_id, description, tier, state, labels_json,
		        file_shares_json, networks_json, created_at
		 FROM filestore_instances WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list filestore instances: %w", err)
	}
	defer rows.Close()
	var out []FilestoreInstance
	for rows.Next() {
		var inst FilestoreInstance
		if err := rows.Scan(
			&inst.Name, &inst.ProjectID, &inst.Location, &inst.InstanceID, &inst.Description, &inst.Tier, &inst.State,
			&inst.LabelsJSON, &inst.FileSharesJSON, &inst.NetworksJSON, &inst.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// DeleteFilestoreInstance deletes an instance by name.
func (s *Store) DeleteFilestoreInstance(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM filestore_instances WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete filestore instance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
