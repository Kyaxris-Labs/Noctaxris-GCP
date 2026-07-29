package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const cloudSQLSchema = `
CREATE TABLE IF NOT EXISTS cloudsql_instances (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  region TEXT NOT NULL,
  database_version TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'RUNNABLE',
  connection_name TEXT NOT NULL DEFAULT '',
  ip_address TEXT NOT NULL DEFAULT '',
  host TEXT NOT NULL DEFAULT '',
  port INTEGER NOT NULL DEFAULT 0,
  container_id TEXT NOT NULL DEFAULT '',
  tier TEXT NOT NULL DEFAULT 'db-f1-micro',
  settings_json TEXT NOT NULL DEFAULT '{}',
  labels_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, instance_id)
);

CREATE INDEX IF NOT EXISTS idx_cloudsql_instances_project ON cloudsql_instances (project_id);
`

func (s *Store) migrateCloudSQL() error {
	if _, err := s.db.Exec(cloudSQLSchema); err != nil {
		return fmt.Errorf("apply cloudsql schema: %w", err)
	}
	return nil
}

// CloudSQLInstance is a Cloud SQL Admin instance row (optional nested engine).
type CloudSQLInstance struct {
	Name            string
	ProjectID       string
	InstanceID      string
	Region          string
	DatabaseVersion string
	State           string
	ConnectionName  string
	IPAddress       string
	Host            string
	Port            int
	ContainerID     string
	Tier            string
	SettingsJSON    string
	LabelsJSON      string
	CreatedAt       string
}

// CloudSQLInstanceResourceName returns projects/{p}/instances/{id}.
func CloudSQLInstanceResourceName(project, instanceID string) string {
	return fmt.Sprintf("projects/%s/instances/%s", project, instanceID)
}

func defaultCloudSQLPort(databaseVersion string) int {
	v := strings.ToUpper(databaseVersion)
	if strings.HasPrefix(v, "MYSQL") {
		return 3306
	}
	return 5432
}

func theatreCloudSQLHost(instanceID, region string) string {
	return fmt.Sprintf("%s.%s.sql.noctaxris-gcp.lab", instanceID, region)
}

func theatreCloudSQLIP(instanceID string) string {
	// Stable lab private IPv4 in documentation range (not routed).
	h := uint32(0)
	for i := 0; i < len(instanceID); i++ {
		h = h*31 + uint32(instanceID[i])
	}
	return fmt.Sprintf("10.127.%d.%d", (h>>8)&0xff, h&0xff)
}

func connectionName(project, region, instanceID string) string {
	return fmt.Sprintf("%s:%s:%s", project, region, instanceID)
}

// CreateCloudSQLInstance inserts an instance. created=false means already exists.
func (s *Store) CreateCloudSQLInstance(inst CloudSQLInstance) (bool, error) {
	if inst.Name == "" || inst.ProjectID == "" || inst.InstanceID == "" || inst.Region == "" || inst.DatabaseVersion == "" {
		return false, fmt.Errorf("cloudsql instance requires name, project, instance id, region, and database version")
	}
	if inst.State == "" {
		inst.State = "RUNNABLE"
	}
	if inst.Tier == "" {
		inst.Tier = "db-f1-micro"
	}
	if inst.Port == 0 {
		inst.Port = defaultCloudSQLPort(inst.DatabaseVersion)
	}
	if inst.Host == "" {
		inst.Host = theatreCloudSQLHost(inst.InstanceID, inst.Region)
	}
	if inst.IPAddress == "" {
		inst.IPAddress = theatreCloudSQLIP(inst.InstanceID)
	}
	if inst.ConnectionName == "" {
		inst.ConnectionName = connectionName(inst.ProjectID, inst.Region, inst.InstanceID)
	}
	if inst.SettingsJSON == "" {
		inst.SettingsJSON = "{}"
	}
	if inst.LabelsJSON == "" {
		inst.LabelsJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if inst.CreatedAt == "" {
		inst.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cloudsql_instances
		 (name, project_id, instance_id, region, database_version, state, connection_name, ip_address,
		  host, port, container_id, tier, settings_json, labels_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.Name, inst.ProjectID, inst.InstanceID, inst.Region, inst.DatabaseVersion, inst.State,
		inst.ConnectionName, inst.IPAddress, inst.Host, inst.Port, inst.ContainerID, inst.Tier,
		inst.SettingsJSON, inst.LabelsJSON, inst.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create cloudsql instance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// UpdateCloudSQLInstanceNested updates host/port/container after nested start.
func (s *Store) UpdateCloudSQLInstanceNested(name, host string, port int, containerID string) error {
	if name == "" {
		return fmt.Errorf("cloudsql instance name required")
	}
	_, err := s.db.Exec(
		`UPDATE cloudsql_instances SET host = ?, port = ?, container_id = ? WHERE name = ?`,
		host, port, containerID, name,
	)
	if err != nil {
		return fmt.Errorf("update cloudsql nested: %w", err)
	}
	return nil
}

// GetCloudSQLInstance loads an instance by resource name.
func (s *Store) GetCloudSQLInstance(name string) (CloudSQLInstance, bool, error) {
	var inst CloudSQLInstance
	err := s.db.QueryRow(
		`SELECT name, project_id, instance_id, region, database_version, state, connection_name, ip_address,
		        host, port, container_id, tier, settings_json, labels_json, created_at
		 FROM cloudsql_instances WHERE name = ?`, name,
	).Scan(
		&inst.Name, &inst.ProjectID, &inst.InstanceID, &inst.Region, &inst.DatabaseVersion, &inst.State,
		&inst.ConnectionName, &inst.IPAddress, &inst.Host, &inst.Port, &inst.ContainerID, &inst.Tier,
		&inst.SettingsJSON, &inst.LabelsJSON, &inst.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return CloudSQLInstance{}, false, nil
	}
	if err != nil {
		return CloudSQLInstance{}, false, fmt.Errorf("get cloudsql instance: %w", err)
	}
	return inst, true, nil
}

// ListCloudSQLInstances lists instances in a project.
func (s *Store) ListCloudSQLInstances(projectID string) ([]CloudSQLInstance, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, instance_id, region, database_version, state, connection_name, ip_address,
		        host, port, container_id, tier, settings_json, labels_json, created_at
		 FROM cloudsql_instances WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list cloudsql instances: %w", err)
	}
	defer rows.Close()
	return scanCloudSQLInstances(rows)
}

// DeleteCloudSQLInstance deletes by name. Returns container_id when present.
func (s *Store) DeleteCloudSQLInstance(name string) (deleted bool, containerID string, err error) {
	inst, ok, err := s.GetCloudSQLInstance(name)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "", nil
	}
	res, err := s.db.Exec(`DELETE FROM cloudsql_instances WHERE name = ?`, name)
	if err != nil {
		return false, "", fmt.Errorf("delete cloudsql instance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, "", err
	}
	return n > 0, inst.ContainerID, nil
}

func scanCloudSQLInstances(rows *sql.Rows) ([]CloudSQLInstance, error) {
	var out []CloudSQLInstance
	for rows.Next() {
		var inst CloudSQLInstance
		if err := rows.Scan(
			&inst.Name, &inst.ProjectID, &inst.InstanceID, &inst.Region, &inst.DatabaseVersion, &inst.State,
			&inst.ConnectionName, &inst.IPAddress, &inst.Host, &inst.Port, &inst.ContainerID, &inst.Tier,
			&inst.SettingsJSON, &inst.LabelsJSON, &inst.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan cloudsql instance: %w", err)
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}
