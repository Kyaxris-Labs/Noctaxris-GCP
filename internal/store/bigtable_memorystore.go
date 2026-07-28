package store

import (
	"database/sql"
	"fmt"
	"time"
)

const bigtableMemorystoreSchema = `
CREATE TABLE IF NOT EXISTS bigtable_instances (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'READY',
  type TEXT NOT NULL DEFAULT 'PRODUCTION',
  labels_json TEXT NOT NULL DEFAULT '{}',
  clusters_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, instance_id)
);

CREATE TABLE IF NOT EXISTS bigtable_tables (
  name TEXT PRIMARY KEY,
  instance_name TEXT NOT NULL,
  project_id TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  table_id TEXT NOT NULL,
  column_families_json TEXT NOT NULL DEFAULT '{}',
  granularity TEXT NOT NULL DEFAULT 'MILLIS',
  created_at TEXT NOT NULL,
  UNIQUE (instance_name, table_id)
);

CREATE INDEX IF NOT EXISTS idx_bigtable_tables_instance ON bigtable_tables (instance_name);

CREATE TABLE IF NOT EXISTS memorystore_redis_instances (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  tier TEXT NOT NULL DEFAULT 'BASIC',
  memory_size_gb INTEGER NOT NULL DEFAULT 1,
  redis_version TEXT NOT NULL DEFAULT 'REDIS_7_0',
  host TEXT NOT NULL DEFAULT '',
  port INTEGER NOT NULL DEFAULT 6379,
  state TEXT NOT NULL DEFAULT 'READY',
  labels_json TEXT NOT NULL DEFAULT '{}',
  authorized_network TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, location, instance_id)
);
`

func (s *Store) migrateBigtableMemorystore() error {
	if _, err := s.db.Exec(bigtableMemorystoreSchema); err != nil {
		return fmt.Errorf("apply bigtable/memorystore schema: %w", err)
	}
	return nil
}

// BigtableInstance is a Bigtable Admin instance row (control-plane theatre).
type BigtableInstance struct {
	Name         string
	ProjectID    string
	InstanceID   string
	DisplayName  string
	State        string
	Type         string
	LabelsJSON   string
	ClustersJSON string
	CreatedAt    string
}

// BigtableTable is a Bigtable Admin table row (metadata only).
type BigtableTable struct {
	Name               string
	InstanceName       string
	ProjectID          string
	InstanceID         string
	TableID            string
	ColumnFamiliesJSON string
	Granularity        string
	CreatedAt          string
}

// MemorystoreRedisInstance is a Memorystore for Redis instance row (no Redis process).
type MemorystoreRedisInstance struct {
	Name               string
	ProjectID          string
	Location           string
	InstanceID         string
	DisplayName        string
	Tier               string
	MemorySizeGb       int
	RedisVersion       string
	Host               string
	Port               int
	State              string
	LabelsJSON         string
	AuthorizedNetwork  string
	CreatedAt          string
}

// CreateBigtableInstance inserts an instance. created=false means already exists.
func (s *Store) CreateBigtableInstance(inst BigtableInstance) (bool, error) {
	if inst.Name == "" || inst.ProjectID == "" || inst.InstanceID == "" {
		return false, fmt.Errorf("bigtable instance requires name, project, and instance id")
	}
	if inst.State == "" {
		inst.State = "READY"
	}
	if inst.Type == "" {
		inst.Type = "PRODUCTION"
	}
	if inst.DisplayName == "" {
		inst.DisplayName = inst.InstanceID
	}
	if inst.LabelsJSON == "" {
		inst.LabelsJSON = "{}"
	}
	if inst.ClustersJSON == "" {
		inst.ClustersJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if inst.CreatedAt == "" {
		inst.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO bigtable_instances
		 (name, project_id, instance_id, display_name, state, type, labels_json, clusters_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.Name, inst.ProjectID, inst.InstanceID, inst.DisplayName, inst.State, inst.Type,
		inst.LabelsJSON, inst.ClustersJSON, inst.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create bigtable instance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetBigtableInstance loads an instance by name.
func (s *Store) GetBigtableInstance(name string) (BigtableInstance, bool, error) {
	var inst BigtableInstance
	err := s.db.QueryRow(
		`SELECT name, project_id, instance_id, display_name, state, type, labels_json, clusters_json, created_at
		 FROM bigtable_instances WHERE name = ?`, name,
	).Scan(
		&inst.Name, &inst.ProjectID, &inst.InstanceID, &inst.DisplayName, &inst.State, &inst.Type,
		&inst.LabelsJSON, &inst.ClustersJSON, &inst.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return BigtableInstance{}, false, nil
	}
	if err != nil {
		return BigtableInstance{}, false, fmt.Errorf("get bigtable instance: %w", err)
	}
	return inst, true, nil
}

// ListBigtableInstances lists instances under a project.
func (s *Store) ListBigtableInstances(projectID string) ([]BigtableInstance, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, instance_id, display_name, state, type, labels_json, clusters_json, created_at
		 FROM bigtable_instances WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list bigtable instances: %w", err)
	}
	defer rows.Close()
	var out []BigtableInstance
	for rows.Next() {
		var inst BigtableInstance
		if err := rows.Scan(
			&inst.Name, &inst.ProjectID, &inst.InstanceID, &inst.DisplayName, &inst.State, &inst.Type,
			&inst.LabelsJSON, &inst.ClustersJSON, &inst.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// DeleteBigtableInstance deletes an instance and nested tables.
func (s *Store) DeleteBigtableInstance(name string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM bigtable_tables WHERE instance_name = ?`, name); err != nil {
		return false, fmt.Errorf("delete bigtable tables: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM bigtable_instances WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete bigtable instance: %w", err)
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

// CreateBigtableTable inserts a table. created=false means already exists.
func (s *Store) CreateBigtableTable(t BigtableTable) (bool, error) {
	if t.Name == "" || t.InstanceName == "" || t.TableID == "" {
		return false, fmt.Errorf("bigtable table requires name, instance name, and table id")
	}
	if t.ColumnFamiliesJSON == "" {
		t.ColumnFamiliesJSON = "{}"
	}
	if t.Granularity == "" {
		t.Granularity = "MILLIS"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if t.CreatedAt == "" {
		t.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO bigtable_tables
		 (name, instance_name, project_id, instance_id, table_id, column_families_json, granularity, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.InstanceName, t.ProjectID, t.InstanceID, t.TableID, t.ColumnFamiliesJSON, t.Granularity, t.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create bigtable table: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetBigtableTable loads a table by name.
func (s *Store) GetBigtableTable(name string) (BigtableTable, bool, error) {
	var t BigtableTable
	err := s.db.QueryRow(
		`SELECT name, instance_name, project_id, instance_id, table_id, column_families_json, granularity, created_at
		 FROM bigtable_tables WHERE name = ?`, name,
	).Scan(
		&t.Name, &t.InstanceName, &t.ProjectID, &t.InstanceID, &t.TableID, &t.ColumnFamiliesJSON, &t.Granularity, &t.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return BigtableTable{}, false, nil
	}
	if err != nil {
		return BigtableTable{}, false, fmt.Errorf("get bigtable table: %w", err)
	}
	return t, true, nil
}

// ListBigtableTables lists tables under an instance.
func (s *Store) ListBigtableTables(instanceName string) ([]BigtableTable, error) {
	rows, err := s.db.Query(
		`SELECT name, instance_name, project_id, instance_id, table_id, column_families_json, granularity, created_at
		 FROM bigtable_tables WHERE instance_name = ? ORDER BY name`,
		instanceName,
	)
	if err != nil {
		return nil, fmt.Errorf("list bigtable tables: %w", err)
	}
	defer rows.Close()
	var out []BigtableTable
	for rows.Next() {
		var t BigtableTable
		if err := rows.Scan(
			&t.Name, &t.InstanceName, &t.ProjectID, &t.InstanceID, &t.TableID, &t.ColumnFamiliesJSON, &t.Granularity, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteBigtableTable deletes a table by name.
func (s *Store) DeleteBigtableTable(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM bigtable_tables WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete bigtable table: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateMemorystoreRedisInstance inserts a Redis instance. created=false means already exists.
func (s *Store) CreateMemorystoreRedisInstance(inst MemorystoreRedisInstance) (bool, error) {
	if inst.Name == "" || inst.ProjectID == "" || inst.Location == "" || inst.InstanceID == "" {
		return false, fmt.Errorf("memorystore redis instance requires name, project, location, and instance id")
	}
	if inst.State == "" {
		inst.State = "READY"
	}
	if inst.Tier == "" {
		inst.Tier = "BASIC"
	}
	if inst.MemorySizeGb <= 0 {
		inst.MemorySizeGb = 1
	}
	if inst.RedisVersion == "" {
		inst.RedisVersion = "REDIS_7_0"
	}
	if inst.Port == 0 {
		inst.Port = 6379
	}
	if inst.Host == "" {
		inst.Host = fmt.Sprintf("%s.%s.redis.noctaxris-gcp.lab", inst.InstanceID, inst.Location)
	}
	if inst.DisplayName == "" {
		inst.DisplayName = inst.InstanceID
	}
	if inst.LabelsJSON == "" {
		inst.LabelsJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if inst.CreatedAt == "" {
		inst.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO memorystore_redis_instances
		 (name, project_id, location, instance_id, display_name, tier, memory_size_gb, redis_version,
		  host, port, state, labels_json, authorized_network, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.Name, inst.ProjectID, inst.Location, inst.InstanceID, inst.DisplayName, inst.Tier, inst.MemorySizeGb,
		inst.RedisVersion, inst.Host, inst.Port, inst.State, inst.LabelsJSON, inst.AuthorizedNetwork, inst.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create memorystore redis instance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetMemorystoreRedisInstance loads an instance by name.
func (s *Store) GetMemorystoreRedisInstance(name string) (MemorystoreRedisInstance, bool, error) {
	var inst MemorystoreRedisInstance
	err := s.db.QueryRow(
		`SELECT name, project_id, location, instance_id, display_name, tier, memory_size_gb, redis_version,
		        host, port, state, labels_json, authorized_network, created_at
		 FROM memorystore_redis_instances WHERE name = ?`, name,
	).Scan(
		&inst.Name, &inst.ProjectID, &inst.Location, &inst.InstanceID, &inst.DisplayName, &inst.Tier, &inst.MemorySizeGb,
		&inst.RedisVersion, &inst.Host, &inst.Port, &inst.State, &inst.LabelsJSON, &inst.AuthorizedNetwork, &inst.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return MemorystoreRedisInstance{}, false, nil
	}
	if err != nil {
		return MemorystoreRedisInstance{}, false, fmt.Errorf("get memorystore redis instance: %w", err)
	}
	return inst, true, nil
}

// ListMemorystoreRedisInstances lists instances under a project location.
func (s *Store) ListMemorystoreRedisInstances(projectID, location string) ([]MemorystoreRedisInstance, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, instance_id, display_name, tier, memory_size_gb, redis_version,
		        host, port, state, labels_json, authorized_network, created_at
		 FROM memorystore_redis_instances WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list memorystore redis instances: %w", err)
	}
	defer rows.Close()
	var out []MemorystoreRedisInstance
	for rows.Next() {
		var inst MemorystoreRedisInstance
		if err := rows.Scan(
			&inst.Name, &inst.ProjectID, &inst.Location, &inst.InstanceID, &inst.DisplayName, &inst.Tier, &inst.MemorySizeGb,
			&inst.RedisVersion, &inst.Host, &inst.Port, &inst.State, &inst.LabelsJSON, &inst.AuthorizedNetwork, &inst.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// DeleteMemorystoreRedisInstance deletes an instance by name.
func (s *Store) DeleteMemorystoreRedisInstance(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM memorystore_redis_instances WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete memorystore redis instance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
