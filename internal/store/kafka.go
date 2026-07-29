package store

import (
	"database/sql"
	"fmt"
	"time"
)

const kafkaSchema = `
CREATE TABLE IF NOT EXISTS kafka_clusters (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  cluster_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  bootstrap_servers TEXT NOT NULL DEFAULT '',
  container_id TEXT NOT NULL DEFAULT '',
  labels_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, location, cluster_id)
);
`

func (s *Store) migrateManagedKafka() error {
	if _, err := s.db.Exec(kafkaSchema); err != nil {
		return fmt.Errorf("apply managed kafka schema: %w", err)
	}
	return nil
}

// KafkaCluster is a Managed Kafka cluster row.
type KafkaCluster struct {
	Name              string
	ProjectID         string
	Location          string
	ClusterID         string
	DisplayName       string
	State             string
	BootstrapServers  string
	ContainerID       string
	LabelsJSON        string
	CreatedAt         string
}

// CreateKafkaCluster inserts a cluster. created=false means already exists.
func (s *Store) CreateKafkaCluster(c KafkaCluster) (bool, error) {
	if c.Name == "" || c.ProjectID == "" || c.Location == "" || c.ClusterID == "" {
		return false, fmt.Errorf("kafka cluster requires name, project, location, and cluster id")
	}
	if c.State == "" {
		c.State = "ACTIVE"
	}
	if c.DisplayName == "" {
		c.DisplayName = c.ClusterID
	}
	if c.LabelsJSON == "" {
		c.LabelsJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if c.CreatedAt == "" {
		c.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO kafka_clusters
		 (name, project_id, location, cluster_id, display_name, state, bootstrap_servers,
		  container_id, labels_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.ProjectID, c.Location, c.ClusterID, c.DisplayName, c.State, c.BootstrapServers,
		c.ContainerID, c.LabelsJSON, c.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create kafka cluster: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetKafkaCluster loads a cluster by name.
func (s *Store) GetKafkaCluster(name string) (KafkaCluster, bool, error) {
	var c KafkaCluster
	err := s.db.QueryRow(
		`SELECT name, project_id, location, cluster_id, display_name, state, bootstrap_servers,
		        container_id, labels_json, created_at
		 FROM kafka_clusters WHERE name = ?`, name,
	).Scan(
		&c.Name, &c.ProjectID, &c.Location, &c.ClusterID, &c.DisplayName, &c.State,
		&c.BootstrapServers, &c.ContainerID, &c.LabelsJSON, &c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return KafkaCluster{}, false, nil
	}
	if err != nil {
		return KafkaCluster{}, false, fmt.Errorf("get kafka cluster: %w", err)
	}
	return c, true, nil
}

// ListKafkaClusters lists clusters under a project location.
func (s *Store) ListKafkaClusters(projectID, location string) ([]KafkaCluster, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, cluster_id, display_name, state, bootstrap_servers,
		        container_id, labels_json, created_at
		 FROM kafka_clusters WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list kafka clusters: %w", err)
	}
	defer rows.Close()
	var out []KafkaCluster
	for rows.Next() {
		var c KafkaCluster
		if err := rows.Scan(
			&c.Name, &c.ProjectID, &c.Location, &c.ClusterID, &c.DisplayName, &c.State,
			&c.BootstrapServers, &c.ContainerID, &c.LabelsJSON, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateKafkaClusterNested updates bootstrap and container id after nested start.
func (s *Store) UpdateKafkaClusterNested(name, bootstrap, containerID, state string) error {
	if state == "" {
		state = "ACTIVE"
	}
	_, err := s.db.Exec(
		`UPDATE kafka_clusters SET bootstrap_servers = ?, container_id = ?, state = ? WHERE name = ?`,
		bootstrap, containerID, state, name,
	)
	if err != nil {
		return fmt.Errorf("update kafka cluster nested: %w", err)
	}
	return nil
}

// DeleteKafkaCluster deletes a cluster by name.
func (s *Store) DeleteKafkaCluster(name string) (KafkaCluster, bool, error) {
	c, ok, err := s.GetKafkaCluster(name)
	if err != nil || !ok {
		return KafkaCluster{}, ok, err
	}
	res, err := s.db.Exec(`DELETE FROM kafka_clusters WHERE name = ?`, name)
	if err != nil {
		return KafkaCluster{}, false, fmt.Errorf("delete kafka cluster: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return KafkaCluster{}, false, err
	}
	return c, n > 0, nil
}
