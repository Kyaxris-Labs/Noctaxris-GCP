package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const securitySchema = `
CREATE TABLE IF NOT EXISTS cloud_armor_security_policies (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  policy_id TEXT NOT NULL,
  numeric_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  policy_type TEXT NOT NULL DEFAULT 'CLOUD_ARMOR',
  rules_json TEXT NOT NULL DEFAULT '[]',
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (project_id, policy_id)
);

CREATE INDEX IF NOT EXISTS idx_cloud_armor_policies_project
  ON cloud_armor_security_policies (project_id);

CREATE TABLE IF NOT EXISTS cert_manager_certificates (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  certificate_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  labels_json TEXT NOT NULL DEFAULT '{}',
  scope TEXT NOT NULL DEFAULT 'DEFAULT',
  cert_type TEXT NOT NULL DEFAULT 'SELF_MANAGED',
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (project_id, location, certificate_id)
);

CREATE INDEX IF NOT EXISTS idx_cert_manager_certs_project_loc
  ON cert_manager_certificates (project_id, location);

CREATE TABLE IF NOT EXISTS cert_manager_certificate_maps (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  map_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  labels_json TEXT NOT NULL DEFAULT '{}',
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (project_id, location, map_id)
);

CREATE INDEX IF NOT EXISTS idx_cert_manager_maps_project_loc
  ON cert_manager_certificate_maps (project_id, location);
`

func (s *Store) migrateSecurity() error {
	if _, err := s.db.Exec(securitySchema); err != nil {
		return fmt.Errorf("apply security schema: %w", err)
	}
	return nil
}

// CloudArmorSecurityPolicy is a Compute securityPolicies row (Cloud Armor).
type CloudArmorSecurityPolicy struct {
	Name        string
	ProjectID   string
	PolicyID    string
	NumericID   string
	Description string
	PolicyType  string
	RulesJSON   string
	BodyJSON    string
	CreatedAt   string
	UpdatedAt   string
}

// CertManagerCertificate is a Certificate Manager certificates row.
type CertManagerCertificate struct {
	Name          string
	ProjectID     string
	Location      string
	CertificateID string
	Description   string
	LabelsJSON    string
	Scope         string
	CertType      string
	BodyJSON      string
	CreatedAt     string
	UpdatedAt     string
}

// CertManagerCertificateMap is a Certificate Manager certificateMaps row.
type CertManagerCertificateMap struct {
	Name        string
	ProjectID   string
	Location    string
	MapID       string
	Description string
	LabelsJSON  string
	BodyJSON    string
	CreatedAt   string
	UpdatedAt   string
}

// CloudArmorPolicyResourceName builds projects/{p}/global/securityPolicies/{id}.
func CloudArmorPolicyResourceName(project, policyID string) string {
	return "projects/" + project + "/global/securityPolicies/" + policyID
}

// CertManagerCertificateResourceName builds projects/{p}/locations/{loc}/certificates/{id}.
func CertManagerCertificateResourceName(project, location, id string) string {
	return "projects/" + project + "/locations/" + location + "/certificates/" + id
}

// CertManagerCertificateMapResourceName builds projects/{p}/locations/{loc}/certificateMaps/{id}.
func CertManagerCertificateMapResourceName(project, location, id string) string {
	return "projects/" + project + "/locations/" + location + "/certificateMaps/" + id
}

// DefaultCloudArmorRulesJSON returns the required default allow rule (priority 2147483647).
func DefaultCloudArmorRulesJSON() string {
	rules := []map[string]any{
		{
			"kind":        "compute#securityPolicyRule",
			"description": "default rule",
			"priority":    2147483647,
			"match": map[string]any{
				"versionedExpr": "SRC_IPS_V1",
				"config":        map[string]any{"srcIpRanges": []string{"*"}},
			},
			"action":  "allow",
			"preview": false,
		},
	}
	raw, _ := json.Marshal(rules)
	return string(raw)
}

// CreateCloudArmorSecurityPolicy inserts a security policy. created=false means already exists.
func (s *Store) CreateCloudArmorSecurityPolicy(p CloudArmorSecurityPolicy) (bool, error) {
	if p.Name == "" || p.ProjectID == "" || p.PolicyID == "" {
		return false, fmt.Errorf("cloud armor policy name/project/policy id required")
	}
	if p.PolicyType == "" {
		p.PolicyType = "CLOUD_ARMOR"
	}
	if p.RulesJSON == "" {
		p.RulesJSON = DefaultCloudArmorRulesJSON()
	}
	if p.BodyJSON == "" {
		p.BodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if p.CreatedAt == "" {
		p.CreatedAt = now
	}
	if p.UpdatedAt == "" {
		p.UpdatedAt = now
	}
	if p.NumericID == "" {
		p.NumericID = strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cloud_armor_security_policies
		 (name, project_id, policy_id, numeric_id, description, policy_type, rules_json, body_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.ProjectID, p.PolicyID, p.NumericID, p.Description, p.PolicyType,
		p.RulesJSON, p.BodyJSON, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create cloud armor policy: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetCloudArmorSecurityPolicy loads a policy by resource name.
func (s *Store) GetCloudArmorSecurityPolicy(name string) (CloudArmorSecurityPolicy, bool, error) {
	var p CloudArmorSecurityPolicy
	err := s.db.QueryRow(
		`SELECT name, project_id, policy_id, numeric_id, description, policy_type, rules_json, body_json, created_at, updated_at
		 FROM cloud_armor_security_policies WHERE name = ?`, name,
	).Scan(&p.Name, &p.ProjectID, &p.PolicyID, &p.NumericID, &p.Description, &p.PolicyType,
		&p.RulesJSON, &p.BodyJSON, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return CloudArmorSecurityPolicy{}, false, nil
	}
	if err != nil {
		return CloudArmorSecurityPolicy{}, false, fmt.Errorf("get cloud armor policy: %w", err)
	}
	return p, true, nil
}

// GetCloudArmorSecurityPolicyByProjectID loads by project + policy id.
func (s *Store) GetCloudArmorSecurityPolicyByProjectID(projectID, policyID string) (CloudArmorSecurityPolicy, bool, error) {
	return s.GetCloudArmorSecurityPolicy(CloudArmorPolicyResourceName(projectID, policyID))
}

// ListCloudArmorSecurityPolicies lists policies for a project.
func (s *Store) ListCloudArmorSecurityPolicies(projectID string) ([]CloudArmorSecurityPolicy, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, policy_id, numeric_id, description, policy_type, rules_json, body_json, created_at, updated_at
		 FROM cloud_armor_security_policies WHERE project_id = ? ORDER BY policy_id`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list cloud armor policies: %w", err)
	}
	defer rows.Close()
	var out []CloudArmorSecurityPolicy
	for rows.Next() {
		var p CloudArmorSecurityPolicy
		if err := rows.Scan(&p.Name, &p.ProjectID, &p.PolicyID, &p.NumericID, &p.Description, &p.PolicyType,
			&p.RulesJSON, &p.BodyJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan cloud armor policy: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateCloudArmorSecurityPolicyRules replaces rules_json (and optional description).
func (s *Store) UpdateCloudArmorSecurityPolicyRules(name, rulesJSON, description string) (CloudArmorSecurityPolicy, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE cloud_armor_security_policies SET rules_json = ?, description = ?, updated_at = ? WHERE name = ?`,
		rulesJSON, description, now, name,
	)
	if err != nil {
		return CloudArmorSecurityPolicy{}, false, fmt.Errorf("update cloud armor rules: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return CloudArmorSecurityPolicy{}, false, nil
	}
	return s.GetCloudArmorSecurityPolicy(name)
}

// UpdateCloudArmorSecurityPolicyBody replaces body_json (labels / extras).
func (s *Store) UpdateCloudArmorSecurityPolicyBody(name, bodyJSON string) (CloudArmorSecurityPolicy, bool, error) {
	if bodyJSON == "" {
		bodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE cloud_armor_security_policies SET body_json = ?, updated_at = ? WHERE name = ?`,
		bodyJSON, now, name,
	)
	if err != nil {
		return CloudArmorSecurityPolicy{}, false, fmt.Errorf("update cloud armor body: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return CloudArmorSecurityPolicy{}, false, fmt.Errorf("update cloud armor body rows: %w", err)
	}
	if n == 0 {
		return CloudArmorSecurityPolicy{}, false, nil
	}
	return s.GetCloudArmorSecurityPolicy(name)
}

// DeleteCloudArmorSecurityPolicy removes a policy.
func (s *Store) DeleteCloudArmorSecurityPolicy(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM cloud_armor_security_policies WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete cloud armor policy: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CreateCertManagerCertificate inserts a certificate. created=false means already exists.
func (s *Store) CreateCertManagerCertificate(c CertManagerCertificate) (bool, error) {
	if c.Name == "" || c.ProjectID == "" || c.Location == "" || c.CertificateID == "" {
		return false, fmt.Errorf("certificate name/project/location/id required")
	}
	if c.LabelsJSON == "" {
		c.LabelsJSON = "{}"
	}
	if c.BodyJSON == "" {
		c.BodyJSON = "{}"
	}
	if c.Scope == "" {
		c.Scope = "DEFAULT"
	}
	if c.CertType == "" {
		c.CertType = "SELF_MANAGED"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if c.CreatedAt == "" {
		c.CreatedAt = now
	}
	if c.UpdatedAt == "" {
		c.UpdatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cert_manager_certificates
		 (name, project_id, location, certificate_id, description, labels_json, scope, cert_type, body_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.ProjectID, c.Location, c.CertificateID, c.Description, c.LabelsJSON,
		c.Scope, c.CertType, c.BodyJSON, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create certificate: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetCertManagerCertificate loads a certificate by resource name.
func (s *Store) GetCertManagerCertificate(name string) (CertManagerCertificate, bool, error) {
	var c CertManagerCertificate
	err := s.db.QueryRow(
		`SELECT name, project_id, location, certificate_id, description, labels_json, scope, cert_type, body_json, created_at, updated_at
		 FROM cert_manager_certificates WHERE name = ?`, name,
	).Scan(&c.Name, &c.ProjectID, &c.Location, &c.CertificateID, &c.Description, &c.LabelsJSON,
		&c.Scope, &c.CertType, &c.BodyJSON, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return CertManagerCertificate{}, false, nil
	}
	if err != nil {
		return CertManagerCertificate{}, false, fmt.Errorf("get certificate: %w", err)
	}
	return c, true, nil
}

// ListCertManagerCertificates lists certificates for a project/location.
func (s *Store) ListCertManagerCertificates(projectID, location string) ([]CertManagerCertificate, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, certificate_id, description, labels_json, scope, cert_type, body_json, created_at, updated_at
		 FROM cert_manager_certificates WHERE project_id = ? AND location = ? ORDER BY certificate_id`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list certificates: %w", err)
	}
	defer rows.Close()
	var out []CertManagerCertificate
	for rows.Next() {
		var c CertManagerCertificate
		if err := rows.Scan(&c.Name, &c.ProjectID, &c.Location, &c.CertificateID, &c.Description, &c.LabelsJSON,
			&c.Scope, &c.CertType, &c.BodyJSON, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan certificate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteCertManagerCertificate removes a certificate.
func (s *Store) DeleteCertManagerCertificate(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM cert_manager_certificates WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete certificate: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CreateCertManagerCertificateMap inserts a certificate map. created=false means already exists.
func (s *Store) CreateCertManagerCertificateMap(m CertManagerCertificateMap) (bool, error) {
	if m.Name == "" || m.ProjectID == "" || m.Location == "" || m.MapID == "" {
		return false, fmt.Errorf("certificate map name/project/location/id required")
	}
	if m.LabelsJSON == "" {
		m.LabelsJSON = "{}"
	}
	if m.BodyJSON == "" {
		m.BodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if m.CreatedAt == "" {
		m.CreatedAt = now
	}
	if m.UpdatedAt == "" {
		m.UpdatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cert_manager_certificate_maps
		 (name, project_id, location, map_id, description, labels_json, body_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.ProjectID, m.Location, m.MapID, m.Description, m.LabelsJSON, m.BodyJSON, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create certificate map: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetCertManagerCertificateMap loads a certificate map by resource name.
func (s *Store) GetCertManagerCertificateMap(name string) (CertManagerCertificateMap, bool, error) {
	var m CertManagerCertificateMap
	err := s.db.QueryRow(
		`SELECT name, project_id, location, map_id, description, labels_json, body_json, created_at, updated_at
		 FROM cert_manager_certificate_maps WHERE name = ?`, name,
	).Scan(&m.Name, &m.ProjectID, &m.Location, &m.MapID, &m.Description, &m.LabelsJSON,
		&m.BodyJSON, &m.CreatedAt, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return CertManagerCertificateMap{}, false, nil
	}
	if err != nil {
		return CertManagerCertificateMap{}, false, fmt.Errorf("get certificate map: %w", err)
	}
	return m, true, nil
}

// ListCertManagerCertificateMaps lists certificate maps for a project/location.
func (s *Store) ListCertManagerCertificateMaps(projectID, location string) ([]CertManagerCertificateMap, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, map_id, description, labels_json, body_json, created_at, updated_at
		 FROM cert_manager_certificate_maps WHERE project_id = ? AND location = ? ORDER BY map_id`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list certificate maps: %w", err)
	}
	defer rows.Close()
	var out []CertManagerCertificateMap
	for rows.Next() {
		var m CertManagerCertificateMap
		if err := rows.Scan(&m.Name, &m.ProjectID, &m.Location, &m.MapID, &m.Description, &m.LabelsJSON,
			&m.BodyJSON, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan certificate map: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteCertManagerCertificateMap removes a certificate map.
func (s *Store) DeleteCertManagerCertificateMap(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM cert_manager_certificate_maps WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete certificate map: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
