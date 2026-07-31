package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const gkeLBCDNSchema = `
CREATE TABLE IF NOT EXISTS gke_clusters (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  cluster_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'RUNNING',
  endpoint TEXT NOT NULL DEFAULT '',
  master_version TEXT NOT NULL DEFAULT '1.28.8-gke.1000',
  labels_json TEXT NOT NULL DEFAULT '{}',
  nested_detail_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, location, cluster_id)
);

CREATE INDEX IF NOT EXISTS idx_gke_clusters_project_loc ON gke_clusters (project_id, location);

CREATE TABLE IF NOT EXISTS lb_backend_services (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  region TEXT NOT NULL DEFAULT 'global',
  service_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  protocol TEXT NOT NULL DEFAULT 'HTTP',
  backends_json TEXT NOT NULL DEFAULT '[]',
  security_policy TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, region, service_id)
);

CREATE INDEX IF NOT EXISTS idx_lb_backend_project ON lb_backend_services (project_id);

CREATE TABLE IF NOT EXISTS lb_url_maps (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  region TEXT NOT NULL DEFAULT 'global',
  map_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  default_service TEXT NOT NULL DEFAULT '',
  host_rules_json TEXT NOT NULL DEFAULT '[]',
  path_matchers_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, region, map_id)
);

CREATE INDEX IF NOT EXISTS idx_lb_url_maps_project ON lb_url_maps (project_id);

CREATE TABLE IF NOT EXISTS lb_forwarding_rules (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  region TEXT NOT NULL DEFAULT 'global',
  rule_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  ip_address TEXT NOT NULL DEFAULT '203.0.113.1',
  target TEXT NOT NULL DEFAULT '',
  port_range TEXT NOT NULL DEFAULT '80-80',
  load_balancing_scheme TEXT NOT NULL DEFAULT 'EXTERNAL',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, region, rule_id)
);

CREATE INDEX IF NOT EXISTS idx_lb_forwarding_project ON lb_forwarding_rules (project_id);

CREATE TABLE IF NOT EXISTS lb_target_https_proxies (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  region TEXT NOT NULL DEFAULT 'global',
  proxy_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  url_map TEXT NOT NULL DEFAULT '',
  security_policy TEXT NOT NULL DEFAULT '',
  ssl_certificates_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, region, proxy_id)
);

CREATE INDEX IF NOT EXISTS idx_lb_target_https_proxies_project ON lb_target_https_proxies (project_id);

CREATE TABLE IF NOT EXISTS cdn_distributions (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  distribution_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  origin_type TEXT NOT NULL DEFAULT 'gcs',
  origin_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  UNIQUE (project_id, distribution_id)
);

CREATE INDEX IF NOT EXISTS idx_cdn_distributions_project ON cdn_distributions (project_id);
`

func (s *Store) migrateGKEEdge() error {
	if _, err := s.db.Exec(gkeLBCDNSchema); err != nil {
		return fmt.Errorf("apply gke/lb/cdn schema: %w", err)
	}
	return nil
}

// GKECluster is a Container API cluster row.
type GKECluster struct {
	Name             string
	ProjectID        string
	Location         string
	ClusterID        string
	DisplayName      string
	Status           string
	Endpoint         string
	MasterVersion    string
	LabelsJSON       string
	NestedDetailJSON string
	CreatedAt        string
}

// CreateGKECluster inserts a cluster. created=false when it already exists.
func (s *Store) CreateGKECluster(c GKECluster) (bool, error) {
	if c.Name == "" || c.ProjectID == "" || c.Location == "" || c.ClusterID == "" {
		return false, fmt.Errorf("gke cluster name/project/location/cluster id required")
	}
	if c.Status == "" {
		c.Status = "RUNNING"
	}
	if c.LabelsJSON == "" {
		c.LabelsJSON = "{}"
	}
	if c.NestedDetailJSON == "" {
		c.NestedDetailJSON = "{}"
	}
	if c.MasterVersion == "" {
		c.MasterVersion = "1.28.8-gke.1000"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if c.CreatedAt == "" {
		c.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT INTO gke_clusters (name, project_id, location, cluster_id, display_name, status, endpoint,
		 master_version, labels_json, nested_detail_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.ProjectID, c.Location, c.ClusterID, c.DisplayName, c.Status, c.Endpoint,
		c.MasterVersion, c.LabelsJSON, c.NestedDetailJSON, c.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return false, nil
		}
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetGKECluster returns a cluster by full name.
func (s *Store) GetGKECluster(name string) (GKECluster, bool, error) {
	var c GKECluster
	err := s.db.QueryRow(
		`SELECT name, project_id, location, cluster_id, display_name, status, endpoint,
		        master_version, labels_json, nested_detail_json, created_at
		 FROM gke_clusters WHERE name = ?`, name,
	).Scan(&c.Name, &c.ProjectID, &c.Location, &c.ClusterID, &c.DisplayName, &c.Status, &c.Endpoint,
		&c.MasterVersion, &c.LabelsJSON, &c.NestedDetailJSON, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return GKECluster{}, false, nil
	}
	if err != nil {
		return GKECluster{}, false, err
	}
	return c, true, nil
}

// ListGKEClusters lists clusters in a project/location.
func (s *Store) ListGKEClusters(projectID, location string) ([]GKECluster, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, cluster_id, display_name, status, endpoint,
		        master_version, labels_json, nested_detail_json, created_at
		 FROM gke_clusters WHERE project_id = ? AND location = ? ORDER BY cluster_id`,
		projectID, location,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GKECluster
	for rows.Next() {
		var c GKECluster
		if err := rows.Scan(&c.Name, &c.ProjectID, &c.Location, &c.ClusterID, &c.DisplayName, &c.Status, &c.Endpoint,
			&c.MasterVersion, &c.LabelsJSON, &c.NestedDetailJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteGKECluster removes a cluster by full name.
func (s *Store) DeleteGKECluster(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM gke_clusters WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UpdateGKEClusterNestedDetail patches nested engine JSON on a cluster.
func (s *Store) UpdateGKEClusterNestedDetail(name, nestedJSON string) error {
	_, err := s.db.Exec(`UPDATE gke_clusters SET nested_detail_json = ? WHERE name = ?`, nestedJSON, name)
	return err
}

// LBBackendService is a Compute backendServices row.
type LBBackendService struct {
	Name            string
	ProjectID       string
	Region          string
	ServiceID       string
	Description     string
	Protocol        string
	BackendsJSON    string
	SecurityPolicy  string
	CreatedAt       string
}

// CreateLBBackendService inserts a backend service.
func (s *Store) CreateLBBackendService(bs LBBackendService) (bool, error) {
	if bs.Name == "" || bs.ProjectID == "" || bs.ServiceID == "" {
		return false, fmt.Errorf("lb backend service name/project/service id required")
	}
	if bs.Region == "" {
		bs.Region = "global"
	}
	if bs.Protocol == "" {
		bs.Protocol = "HTTP"
	}
	if bs.BackendsJSON == "" {
		bs.BackendsJSON = "[]"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if bs.CreatedAt == "" {
		bs.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT INTO lb_backend_services (name, project_id, region, service_id, description, protocol, backends_json, security_policy, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		bs.Name, bs.ProjectID, bs.Region, bs.ServiceID, bs.Description, bs.Protocol, bs.BackendsJSON, bs.SecurityPolicy, bs.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return false, nil
		}
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetLBBackendService returns a backend service by full self link name.
func (s *Store) GetLBBackendService(name string) (LBBackendService, bool, error) {
	var bs LBBackendService
	err := s.db.QueryRow(
		`SELECT name, project_id, region, service_id, description, protocol, backends_json, security_policy, created_at
		 FROM lb_backend_services WHERE name = ?`, name,
	).Scan(&bs.Name, &bs.ProjectID, &bs.Region, &bs.ServiceID, &bs.Description, &bs.Protocol, &bs.BackendsJSON, &bs.SecurityPolicy, &bs.CreatedAt)
	if err == sql.ErrNoRows {
		return LBBackendService{}, false, nil
	}
	if err != nil {
		return LBBackendService{}, false, err
	}
	return bs, true, nil
}

// GetLBBackendServiceByID looks up by project, region, id.
func (s *Store) GetLBBackendServiceByID(projectID, region, serviceID string) (LBBackendService, bool, error) {
	if region == "" {
		region = "global"
	}
	name := lbBackendServiceName(projectID, region, serviceID)
	return s.GetLBBackendService(name)
}

// ListLBBackendServices lists backend services for a project (global scope).
func (s *Store) ListLBBackendServices(projectID, region string) ([]LBBackendService, error) {
	if region == "" {
		region = "global"
	}
	rows, err := s.db.Query(
		`SELECT name, project_id, region, service_id, description, protocol, backends_json, security_policy, created_at
		 FROM lb_backend_services WHERE project_id = ? AND region = ? ORDER BY service_id`,
		projectID, region,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLBBackendServices(rows)
}

func scanLBBackendServices(rows *sql.Rows) ([]LBBackendService, error) {
	var out []LBBackendService
	for rows.Next() {
		var bs LBBackendService
		if err := rows.Scan(&bs.Name, &bs.ProjectID, &bs.Region, &bs.ServiceID, &bs.Description, &bs.Protocol, &bs.BackendsJSON, &bs.SecurityPolicy, &bs.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, bs)
	}
	return out, rows.Err()
}

// UpdateLBBackendServiceSecurityPolicy sets Cloud Armor policy self link on a backend service.
func (s *Store) UpdateLBBackendServiceSecurityPolicy(name, securityPolicy string) (bool, error) {
	res, err := s.db.Exec(`UPDATE lb_backend_services SET security_policy = ? WHERE name = ?`, securityPolicy, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteLBBackendService deletes by full name.
func (s *Store) DeleteLBBackendService(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM lb_backend_services WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// LBURLMap is a urlMaps row.
type LBURLMap struct {
	Name              string
	ProjectID         string
	Region            string
	MapID             string
	Description       string
	DefaultService    string
	HostRulesJSON     string
	PathMatchersJSON  string
	CreatedAt         string
}

// CreateLBURLMap inserts a url map.
func (s *Store) CreateLBURLMap(m LBURLMap) (bool, error) {
	if m.Name == "" || m.ProjectID == "" || m.MapID == "" {
		return false, fmt.Errorf("lb url map name/project/map id required")
	}
	if m.Region == "" {
		m.Region = "global"
	}
	if m.HostRulesJSON == "" {
		m.HostRulesJSON = "[]"
	}
	if m.PathMatchersJSON == "" {
		m.PathMatchersJSON = "[]"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if m.CreatedAt == "" {
		m.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT INTO lb_url_maps (name, project_id, region, map_id, description, default_service, host_rules_json, path_matchers_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.ProjectID, m.Region, m.MapID, m.Description, m.DefaultService, m.HostRulesJSON, m.PathMatchersJSON, m.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return false, nil
		}
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetLBURLMap returns a url map by name.
func (s *Store) GetLBURLMap(name string) (LBURLMap, bool, error) {
	var m LBURLMap
	err := s.db.QueryRow(
		`SELECT name, project_id, region, map_id, description, default_service, host_rules_json, path_matchers_json, created_at
		 FROM lb_url_maps WHERE name = ?`, name,
	).Scan(&m.Name, &m.ProjectID, &m.Region, &m.MapID, &m.Description, &m.DefaultService, &m.HostRulesJSON, &m.PathMatchersJSON, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return LBURLMap{}, false, nil
	}
	if err != nil {
		return LBURLMap{}, false, err
	}
	return m, true, nil
}

// GetLBURLMapByID looks up by project, region, id.
func (s *Store) GetLBURLMapByID(projectID, region, mapID string) (LBURLMap, bool, error) {
	if region == "" {
		region = "global"
	}
	return s.GetLBURLMap(lbURLMapName(projectID, region, mapID))
}

// ListLBURLMaps lists url maps.
func (s *Store) ListLBURLMaps(projectID, region string) ([]LBURLMap, error) {
	if region == "" {
		region = "global"
	}
	rows, err := s.db.Query(
		`SELECT name, project_id, region, map_id, description, default_service, host_rules_json, path_matchers_json, created_at
		 FROM lb_url_maps WHERE project_id = ? AND region = ? ORDER BY map_id`,
		projectID, region,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LBURLMap
	for rows.Next() {
		var m LBURLMap
		if err := rows.Scan(&m.Name, &m.ProjectID, &m.Region, &m.MapID, &m.Description, &m.DefaultService, &m.HostRulesJSON, &m.PathMatchersJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteLBURLMap deletes by name.
func (s *Store) DeleteLBURLMap(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM lb_url_maps WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// LBForwardingRule is a forwardingRules row.
type LBForwardingRule struct {
	Name                string
	ProjectID           string
	Region              string
	RuleID              string
	Description         string
	IPAddress           string
	Target              string
	PortRange           string
	LoadBalancingScheme string
	CreatedAt           string
}

// CreateLBForwardingRule inserts a forwarding rule.
func (s *Store) CreateLBForwardingRule(fr LBForwardingRule) (bool, error) {
	if fr.Name == "" || fr.ProjectID == "" || fr.RuleID == "" {
		return false, fmt.Errorf("lb forwarding rule name/project/rule id required")
	}
	if fr.Region == "" {
		fr.Region = "global"
	}
	if fr.IPAddress == "" {
		fr.IPAddress = "203.0.113.1"
	}
	if fr.PortRange == "" {
		fr.PortRange = "80-80"
	}
	if fr.LoadBalancingScheme == "" {
		fr.LoadBalancingScheme = "EXTERNAL"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if fr.CreatedAt == "" {
		fr.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT INTO lb_forwarding_rules (name, project_id, region, rule_id, description, ip_address, target, port_range, load_balancing_scheme, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fr.Name, fr.ProjectID, fr.Region, fr.RuleID, fr.Description, fr.IPAddress, fr.Target, fr.PortRange, fr.LoadBalancingScheme, fr.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return false, nil
		}
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetLBForwardingRule returns a forwarding rule by name.
func (s *Store) GetLBForwardingRule(name string) (LBForwardingRule, bool, error) {
	var fr LBForwardingRule
	err := s.db.QueryRow(
		`SELECT name, project_id, region, rule_id, description, ip_address, target, port_range, load_balancing_scheme, created_at
		 FROM lb_forwarding_rules WHERE name = ?`, name,
	).Scan(&fr.Name, &fr.ProjectID, &fr.Region, &fr.RuleID, &fr.Description, &fr.IPAddress, &fr.Target, &fr.PortRange, &fr.LoadBalancingScheme, &fr.CreatedAt)
	if err == sql.ErrNoRows {
		return LBForwardingRule{}, false, nil
	}
	if err != nil {
		return LBForwardingRule{}, false, err
	}
	return fr, true, nil
}

// GetLBForwardingRuleByID looks up by project, region, rule id.
func (s *Store) GetLBForwardingRuleByID(projectID, region, ruleID string) (LBForwardingRule, bool, error) {
	if region == "" {
		region = "global"
	}
	return s.GetLBForwardingRule(lbForwardingRuleName(projectID, region, ruleID))
}

// ListLBForwardingRules lists forwarding rules.
func (s *Store) ListLBForwardingRules(projectID, region string) ([]LBForwardingRule, error) {
	if region == "" {
		region = "global"
	}
	rows, err := s.db.Query(
		`SELECT name, project_id, region, rule_id, description, ip_address, target, port_range, load_balancing_scheme, created_at
		 FROM lb_forwarding_rules WHERE project_id = ? AND region = ? ORDER BY rule_id`,
		projectID, region,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LBForwardingRule
	for rows.Next() {
		var fr LBForwardingRule
		if err := rows.Scan(&fr.Name, &fr.ProjectID, &fr.Region, &fr.RuleID, &fr.Description, &fr.IPAddress, &fr.Target, &fr.PortRange, &fr.LoadBalancingScheme, &fr.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, fr)
	}
	return out, rows.Err()
}

// DeleteLBForwardingRule deletes by name.
func (s *Store) DeleteLBForwardingRule(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM lb_forwarding_rules WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// LBTargetHTTPSProxy is a targetHttpsProxies row.
type LBTargetHTTPSProxy struct {
	Name                 string
	ProjectID            string
	Region               string
	ProxyID              string
	Description          string
	URLMap               string
	SecurityPolicy       string
	SSLCertificatesJSON  string
	CreatedAt            string
}

// CreateLBTargetHTTPSProxy inserts a target HTTPS proxy.
func (s *Store) CreateLBTargetHTTPSProxy(p LBTargetHTTPSProxy) (bool, error) {
	if p.Name == "" || p.ProjectID == "" || p.ProxyID == "" {
		return false, fmt.Errorf("lb target https proxy name/project/proxy id required")
	}
	if p.Region == "" {
		p.Region = "global"
	}
	if p.SSLCertificatesJSON == "" {
		p.SSLCertificatesJSON = "[]"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if p.CreatedAt == "" {
		p.CreatedAt = now
	}
	res, err := s.db.Exec(
		`INSERT INTO lb_target_https_proxies (name, project_id, region, proxy_id, description, url_map, security_policy, ssl_certificates_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.ProjectID, p.Region, p.ProxyID, p.Description, p.URLMap, p.SecurityPolicy, p.SSLCertificatesJSON, p.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return false, nil
		}
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetLBTargetHTTPSProxy returns a proxy by full self link name.
func (s *Store) GetLBTargetHTTPSProxy(name string) (LBTargetHTTPSProxy, bool, error) {
	var p LBTargetHTTPSProxy
	err := s.db.QueryRow(
		`SELECT name, project_id, region, proxy_id, description, url_map, security_policy, ssl_certificates_json, created_at
		 FROM lb_target_https_proxies WHERE name = ?`, name,
	).Scan(&p.Name, &p.ProjectID, &p.Region, &p.ProxyID, &p.Description, &p.URLMap, &p.SecurityPolicy, &p.SSLCertificatesJSON, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return LBTargetHTTPSProxy{}, false, nil
	}
	if err != nil {
		return LBTargetHTTPSProxy{}, false, err
	}
	return p, true, nil
}

// ListLBTargetHTTPSProxies lists proxies for a project (global scope).
func (s *Store) ListLBTargetHTTPSProxies(projectID, region string) ([]LBTargetHTTPSProxy, error) {
	if region == "" {
		region = "global"
	}
	rows, err := s.db.Query(
		`SELECT name, project_id, region, proxy_id, description, url_map, security_policy, ssl_certificates_json, created_at
		 FROM lb_target_https_proxies WHERE project_id = ? AND region = ? ORDER BY proxy_id`,
		projectID, region,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LBTargetHTTPSProxy
	for rows.Next() {
		var p LBTargetHTTPSProxy
		if err := rows.Scan(&p.Name, &p.ProjectID, &p.Region, &p.ProxyID, &p.Description, &p.URLMap, &p.SecurityPolicy, &p.SSLCertificatesJSON, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateLBTargetHTTPSProxy patches url map, security policy, and description.
func (s *Store) UpdateLBTargetHTTPSProxy(name, description, urlMap, securityPolicy string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE lb_target_https_proxies SET description = ?, url_map = ?, security_policy = ? WHERE name = ?`,
		description, urlMap, securityPolicy, name,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteLBTargetHTTPSProxy deletes by full name.
func (s *Store) DeleteLBTargetHTTPSProxy(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM lb_target_https_proxies WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CDNDistribution is a Cloud CDN distribution row.
type CDNDistribution struct {
	Name           string
	ProjectID      string
	DistributionID string
	Description    string
	OriginType     string
	OriginJSON     string
	Enabled        bool
	CreatedAt      string
}

// CreateCDNDistribution inserts a distribution.
func (s *Store) CreateCDNDistribution(d CDNDistribution) (bool, error) {
	if d.Name == "" || d.ProjectID == "" || d.DistributionID == "" {
		return false, fmt.Errorf("cdn distribution name/project/distribution id required")
	}
	if d.OriginType == "" {
		d.OriginType = "gcs"
	}
	if d.OriginJSON == "" {
		d.OriginJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if d.CreatedAt == "" {
		d.CreatedAt = now
	}
	enabled := 0
	if d.Enabled {
		enabled = 1
	}
	res, err := s.db.Exec(
		`INSERT INTO cdn_distributions (name, project_id, distribution_id, description, origin_type, origin_json, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Name, d.ProjectID, d.DistributionID, d.Description, d.OriginType, d.OriginJSON, enabled, d.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return false, nil
		}
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetCDNDistribution returns a distribution by full name.
func (s *Store) GetCDNDistribution(name string) (CDNDistribution, bool, error) {
	var d CDNDistribution
	var enabled int
	err := s.db.QueryRow(
		`SELECT name, project_id, distribution_id, description, origin_type, origin_json, enabled, created_at
		 FROM cdn_distributions WHERE name = ?`, name,
	).Scan(&d.Name, &d.ProjectID, &d.DistributionID, &d.Description, &d.OriginType, &d.OriginJSON, &enabled, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return CDNDistribution{}, false, nil
	}
	if err != nil {
		return CDNDistribution{}, false, err
	}
	d.Enabled = enabled != 0
	return d, true, nil
}

// GetCDNDistributionByID looks up by project and distribution id.
func (s *Store) GetCDNDistributionByID(projectID, distributionID string) (CDNDistribution, bool, error) {
	return s.GetCDNDistribution(cdnDistributionName(projectID, distributionID))
}

// GetCDNDistributionByEdgeID finds a distribution by edge id (distribution_id).
func (s *Store) GetCDNDistributionByEdgeID(distributionID string) (CDNDistribution, bool, error) {
	var d CDNDistribution
	var enabled int
	err := s.db.QueryRow(
		`SELECT name, project_id, distribution_id, description, origin_type, origin_json, enabled, created_at
		 FROM cdn_distributions WHERE distribution_id = ? AND enabled = 1`, distributionID,
	).Scan(&d.Name, &d.ProjectID, &d.DistributionID, &d.Description, &d.OriginType, &d.OriginJSON, &enabled, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return CDNDistribution{}, false, nil
	}
	if err != nil {
		return CDNDistribution{}, false, err
	}
	d.Enabled = enabled != 0
	return d, true, nil
}

// ListCDNDistributions lists distributions for a project.
func (s *Store) ListCDNDistributions(projectID string) ([]CDNDistribution, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, distribution_id, description, origin_type, origin_json, enabled, created_at
		 FROM cdn_distributions WHERE project_id = ? ORDER BY distribution_id`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CDNDistribution
	for rows.Next() {
		var d CDNDistribution
		var enabled int
		if err := rows.Scan(&d.Name, &d.ProjectID, &d.DistributionID, &d.Description, &d.OriginType, &d.OriginJSON, &enabled, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Enabled = enabled != 0
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteCDNDistribution deletes by name.
func (s *Store) DeleteCDNDistribution(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM cdn_distributions WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func lbBackendServiceName(projectID, region, serviceID string) string {
	if region == "" || region == "global" {
		return fmt.Sprintf("projects/%s/global/backendServices/%s", projectID, serviceID)
	}
	return fmt.Sprintf("projects/%s/regions/%s/backendServices/%s", projectID, region, serviceID)
}

func lbURLMapName(projectID, region, mapID string) string {
	if region == "" || region == "global" {
		return fmt.Sprintf("projects/%s/global/urlMaps/%s", projectID, mapID)
	}
	return fmt.Sprintf("projects/%s/regions/%s/urlMaps/%s", projectID, region, mapID)
}

func lbForwardingRuleName(projectID, region, ruleID string) string {
	if region == "" || region == "global" {
		return fmt.Sprintf("projects/%s/global/forwardingRules/%s", projectID, ruleID)
	}
	return fmt.Sprintf("projects/%s/regions/%s/forwardingRules/%s", projectID, region, ruleID)
}

func lbTargetHTTPSProxyName(projectID, region, proxyID string) string {
	if region == "" || region == "global" {
		return fmt.Sprintf("projects/%s/global/targetHttpsProxies/%s", projectID, proxyID)
	}
	return fmt.Sprintf("projects/%s/regions/%s/targetHttpsProxies/%s", projectID, region, proxyID)
}

func cdnDistributionName(projectID, distributionID string) string {
	return fmt.Sprintf("projects/%s/global/distributions/%s", projectID, distributionID)
}

// NewLBResourceID returns a short unique id for lab LB resources.
func NewLBResourceID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
}

// ParseGCSOriginFromBackends reads the first backend with balancingMode GCS.
func ParseGCSOriginFromBackends(backendsJSON string) (bucket, objectPrefix string, ok bool) {
	var backends []map[string]any
	if err := json.Unmarshal([]byte(backendsJSON), &backends); err != nil {
		return "", "", false
	}
	for _, b := range backends {
		if g, _ := b["gcsBucket"].(string); g != "" {
			prefix, _ := b["objectPrefix"].(string)
			return g, prefix, true
		}
	}
	return "", "", false
}
