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

CREATE TABLE IF NOT EXISTS kafka_topics (
  name TEXT PRIMARY KEY,
  cluster_name TEXT NOT NULL,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  cluster_id TEXT NOT NULL,
  topic_id TEXT NOT NULL,
  partition_count INTEGER NOT NULL DEFAULT 1,
  replication_factor INTEGER NOT NULL DEFAULT 1,
  configs_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE (cluster_name, topic_id)
);

CREATE TABLE IF NOT EXISTS kafka_acls (
  name TEXT PRIMARY KEY,
  cluster_name TEXT NOT NULL,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  cluster_id TEXT NOT NULL,
  acl_id TEXT NOT NULL,
  resource_type TEXT NOT NULL DEFAULT '',
  resource_name TEXT NOT NULL DEFAULT '',
  pattern_type TEXT NOT NULL DEFAULT 'LITERAL',
  acl_entries_json TEXT NOT NULL DEFAULT '[]',
  etag TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (cluster_name, acl_id)
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

// DeleteKafkaCluster deletes a cluster by name and cascades topics/ACLs.
func (s *Store) DeleteKafkaCluster(name string) (KafkaCluster, bool, error) {
	c, ok, err := s.GetKafkaCluster(name)
	if err != nil || !ok {
		return KafkaCluster{}, ok, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return KafkaCluster{}, false, fmt.Errorf("begin delete kafka cluster: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM kafka_topics WHERE cluster_name = ?`, name); err != nil {
		return KafkaCluster{}, false, fmt.Errorf("cascade kafka topics: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM kafka_acls WHERE cluster_name = ?`, name); err != nil {
		return KafkaCluster{}, false, fmt.Errorf("cascade kafka acls: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM kafka_clusters WHERE name = ?`, name)
	if err != nil {
		return KafkaCluster{}, false, fmt.Errorf("delete kafka cluster: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return KafkaCluster{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return KafkaCluster{}, false, fmt.Errorf("commit delete kafka cluster: %w", err)
	}
	return c, n > 0, nil
}

// KafkaTopic is a Managed Kafka topic row.
type KafkaTopic struct {
	Name              string
	ClusterName       string
	ProjectID         string
	Location          string
	ClusterID         string
	TopicID           string
	PartitionCount    int
	ReplicationFactor int
	ConfigsJSON       string
	CreatedAt         string
}

// CreateKafkaTopic inserts a topic. created=false means already exists.
func (s *Store) CreateKafkaTopic(t KafkaTopic) (bool, error) {
	if t.Name == "" || t.ClusterName == "" || t.ProjectID == "" || t.Location == "" || t.ClusterID == "" || t.TopicID == "" {
		return false, fmt.Errorf("kafka topic requires name, cluster, project, location, cluster id, and topic id")
	}
	if t.PartitionCount <= 0 {
		t.PartitionCount = 1
	}
	if t.ReplicationFactor <= 0 {
		t.ReplicationFactor = 1
	}
	if t.ConfigsJSON == "" {
		t.ConfigsJSON = "{}"
	}
	if t.CreatedAt == "" {
		t.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO kafka_topics
		 (name, cluster_name, project_id, location, cluster_id, topic_id,
		  partition_count, replication_factor, configs_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.ClusterName, t.ProjectID, t.Location, t.ClusterID, t.TopicID,
		t.PartitionCount, t.ReplicationFactor, t.ConfigsJSON, t.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create kafka topic: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetKafkaTopic loads a topic by resource name.
func (s *Store) GetKafkaTopic(name string) (KafkaTopic, bool, error) {
	var t KafkaTopic
	err := s.db.QueryRow(
		`SELECT name, cluster_name, project_id, location, cluster_id, topic_id,
		        partition_count, replication_factor, configs_json, created_at
		 FROM kafka_topics WHERE name = ?`, name,
	).Scan(
		&t.Name, &t.ClusterName, &t.ProjectID, &t.Location, &t.ClusterID, &t.TopicID,
		&t.PartitionCount, &t.ReplicationFactor, &t.ConfigsJSON, &t.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return KafkaTopic{}, false, nil
	}
	if err != nil {
		return KafkaTopic{}, false, fmt.Errorf("get kafka topic: %w", err)
	}
	return t, true, nil
}

// ListKafkaTopics lists topics under a cluster.
func (s *Store) ListKafkaTopics(clusterName string) ([]KafkaTopic, error) {
	rows, err := s.db.Query(
		`SELECT name, cluster_name, project_id, location, cluster_id, topic_id,
		        partition_count, replication_factor, configs_json, created_at
		 FROM kafka_topics WHERE cluster_name = ? ORDER BY topic_id`,
		clusterName,
	)
	if err != nil {
		return nil, fmt.Errorf("list kafka topics: %w", err)
	}
	defer rows.Close()
	var out []KafkaTopic
	for rows.Next() {
		var t KafkaTopic
		if err := rows.Scan(
			&t.Name, &t.ClusterName, &t.ProjectID, &t.Location, &t.ClusterID, &t.TopicID,
			&t.PartitionCount, &t.ReplicationFactor, &t.ConfigsJSON, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteKafkaTopic deletes a topic by name.
func (s *Store) DeleteKafkaTopic(name string) (KafkaTopic, bool, error) {
	t, ok, err := s.GetKafkaTopic(name)
	if err != nil || !ok {
		return KafkaTopic{}, ok, err
	}
	res, err := s.db.Exec(`DELETE FROM kafka_topics WHERE name = ?`, name)
	if err != nil {
		return KafkaTopic{}, false, fmt.Errorf("delete kafka topic: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return KafkaTopic{}, false, err
	}
	return t, n > 0, nil
}

// KafkaACL is a Managed Kafka ACL row (metadata theatre; not applied to broker).
type KafkaACL struct {
	Name           string
	ClusterName    string
	ProjectID      string
	Location       string
	ClusterID      string
	ACLID          string
	ResourceType   string
	ResourceName   string
	PatternType    string
	ACLEntriesJSON string
	Etag           string
	CreatedAt      string
}

// CreateKafkaACL inserts an ACL. created=false means already exists.
func (s *Store) CreateKafkaACL(a KafkaACL) (bool, error) {
	if a.Name == "" || a.ClusterName == "" || a.ProjectID == "" || a.Location == "" || a.ClusterID == "" || a.ACLID == "" {
		return false, fmt.Errorf("kafka acl requires name, cluster, project, location, cluster id, and acl id")
	}
	if a.PatternType == "" {
		a.PatternType = "LITERAL"
	}
	if a.ACLEntriesJSON == "" {
		a.ACLEntriesJSON = "[]"
	}
	if a.Etag == "" {
		a.Etag = "ACAB"
	}
	if a.CreatedAt == "" {
		a.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO kafka_acls
		 (name, cluster_name, project_id, location, cluster_id, acl_id,
		  resource_type, resource_name, pattern_type, acl_entries_json, etag, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Name, a.ClusterName, a.ProjectID, a.Location, a.ClusterID, a.ACLID,
		a.ResourceType, a.ResourceName, a.PatternType, a.ACLEntriesJSON, a.Etag, a.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create kafka acl: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetKafkaACL loads an ACL by resource name.
func (s *Store) GetKafkaACL(name string) (KafkaACL, bool, error) {
	var a KafkaACL
	err := s.db.QueryRow(
		`SELECT name, cluster_name, project_id, location, cluster_id, acl_id,
		        resource_type, resource_name, pattern_type, acl_entries_json, etag, created_at
		 FROM kafka_acls WHERE name = ?`, name,
	).Scan(
		&a.Name, &a.ClusterName, &a.ProjectID, &a.Location, &a.ClusterID, &a.ACLID,
		&a.ResourceType, &a.ResourceName, &a.PatternType, &a.ACLEntriesJSON, &a.Etag, &a.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return KafkaACL{}, false, nil
	}
	if err != nil {
		return KafkaACL{}, false, fmt.Errorf("get kafka acl: %w", err)
	}
	return a, true, nil
}

// ListKafkaACLs lists ACLs under a cluster.
func (s *Store) ListKafkaACLs(clusterName string) ([]KafkaACL, error) {
	rows, err := s.db.Query(
		`SELECT name, cluster_name, project_id, location, cluster_id, acl_id,
		        resource_type, resource_name, pattern_type, acl_entries_json, etag, created_at
		 FROM kafka_acls WHERE cluster_name = ? ORDER BY acl_id`,
		clusterName,
	)
	if err != nil {
		return nil, fmt.Errorf("list kafka acls: %w", err)
	}
	defer rows.Close()
	var out []KafkaACL
	for rows.Next() {
		var a KafkaACL
		if err := rows.Scan(
			&a.Name, &a.ClusterName, &a.ProjectID, &a.Location, &a.ClusterID, &a.ACLID,
			&a.ResourceType, &a.ResourceName, &a.PatternType, &a.ACLEntriesJSON, &a.Etag, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteKafkaACL deletes an ACL by name.
func (s *Store) DeleteKafkaACL(name string) (KafkaACL, bool, error) {
	a, ok, err := s.GetKafkaACL(name)
	if err != nil || !ok {
		return KafkaACL{}, ok, err
	}
	res, err := s.db.Exec(`DELETE FROM kafka_acls WHERE name = ?`, name)
	if err != nil {
		return KafkaACL{}, false, fmt.Errorf("delete kafka acl: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return KafkaACL{}, false, err
	}
	return a, n > 0, nil
}
