package store

import (
	"database/sql"
	"fmt"
	"time"
)

const computeEngineSchema = `
CREATE TABLE IF NOT EXISTS gce_instances (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  zone TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  machine_type TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'RUNNING',
  network_interfaces_json TEXT NOT NULL DEFAULT '[]',
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gce_networks (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  network_id TEXT NOT NULL,
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gce_subnetworks (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  region TEXT NOT NULL,
  subnetwork_id TEXT NOT NULL,
  network TEXT NOT NULL DEFAULT '',
  ip_cidr_range TEXT NOT NULL DEFAULT '',
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gce_firewalls (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  firewall_id TEXT NOT NULL,
  network TEXT NOT NULL DEFAULT '',
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`

func (s *Store) migrateComputeEngine() error {
	if _, err := s.db.Exec(computeEngineSchema); err != nil {
		return fmt.Errorf("apply compute engine schema: %w", err)
	}
	return nil
}

// GCEInstance is Compute Engine instance metadata (no nested VM).
type GCEInstance struct {
	Name                   string
	ProjectID              string
	Zone                   string
	InstanceID             string
	MachineType            string
	Status                 string
	NetworkInterfacesJSON  string
	BodyJSON               string
	CreatedAt              string
	UpdatedAt              string
}

// GCENetwork is a VPC network metadata row.
type GCENetwork struct {
	Name      string
	ProjectID string
	NetworkID string
	BodyJSON  string
	CreatedAt string
	UpdatedAt string
}

// GCESubnetwork is a regional subnet metadata row.
type GCESubnetwork struct {
	Name         string
	ProjectID    string
	Region       string
	SubnetworkID string
	Network      string
	IPCidrRange  string
	BodyJSON     string
	CreatedAt    string
	UpdatedAt    string
}

// GCEFirewall is a global firewall rule metadata row.
type GCEFirewall struct {
	Name       string
	ProjectID  string
	FirewallID string
	Network    string
	BodyJSON   string
	CreatedAt  string
	UpdatedAt  string
}

// CreateGCEInstance inserts an instance. created=false means already exists.
func (s *Store) CreateGCEInstance(inst GCEInstance) (created bool, err error) {
	if inst.Name == "" || inst.ProjectID == "" || inst.Zone == "" || inst.InstanceID == "" {
		return false, fmt.Errorf("gce instance requires name, project, zone, and instance id")
	}
	if inst.Status == "" {
		inst.Status = "RUNNING"
	}
	if inst.NetworkInterfacesJSON == "" {
		inst.NetworkInterfacesJSON = "[]"
	}
	if inst.BodyJSON == "" {
		inst.BodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if inst.CreatedAt == "" {
		inst.CreatedAt = now
	}
	if inst.UpdatedAt == "" {
		inst.UpdatedAt = inst.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO gce_instances
		 (name, project_id, zone, instance_id, machine_type, status, network_interfaces_json, body_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.Name, inst.ProjectID, inst.Zone, inst.InstanceID, inst.MachineType, inst.Status,
		inst.NetworkInterfacesJSON, inst.BodyJSON, inst.CreatedAt, inst.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create gce instance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetGCEInstance loads an instance by resource name.
func (s *Store) GetGCEInstance(name string) (GCEInstance, bool, error) {
	var inst GCEInstance
	err := s.db.QueryRow(
		`SELECT name, project_id, zone, instance_id, machine_type, status, network_interfaces_json, body_json, created_at, updated_at
		 FROM gce_instances WHERE name = ?`, name,
	).Scan(
		&inst.Name, &inst.ProjectID, &inst.Zone, &inst.InstanceID, &inst.MachineType, &inst.Status,
		&inst.NetworkInterfacesJSON, &inst.BodyJSON, &inst.CreatedAt, &inst.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return GCEInstance{}, false, nil
	}
	if err != nil {
		return GCEInstance{}, false, fmt.Errorf("get gce instance: %w", err)
	}
	return inst, true, nil
}

// ListGCEInstances lists instances in a project/zone.
func (s *Store) ListGCEInstances(projectID, zone string) ([]GCEInstance, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, zone, instance_id, machine_type, status, network_interfaces_json, body_json, created_at, updated_at
		 FROM gce_instances WHERE project_id = ? AND zone = ? ORDER BY name`,
		projectID, zone,
	)
	if err != nil {
		return nil, fmt.Errorf("list gce instances: %w", err)
	}
	defer rows.Close()
	var out []GCEInstance
	for rows.Next() {
		var inst GCEInstance
		if err := rows.Scan(
			&inst.Name, &inst.ProjectID, &inst.Zone, &inst.InstanceID, &inst.MachineType, &inst.Status,
			&inst.NetworkInterfacesJSON, &inst.BodyJSON, &inst.CreatedAt, &inst.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// SetGCEInstanceStatus updates status theatre (stop/start/reset); never starts a VM.
func (s *Store) SetGCEInstanceStatus(name, status string) (GCEInstance, bool, error) {
	if status == "" {
		return GCEInstance{}, false, fmt.Errorf("status required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`UPDATE gce_instances SET status = ?, updated_at = ? WHERE name = ?`, status, now, name)
	if err != nil {
		return GCEInstance{}, false, fmt.Errorf("set gce instance status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return GCEInstance{}, false, err
	}
	if n == 0 {
		return GCEInstance{}, false, nil
	}
	return s.GetGCEInstance(name)
}

// UpdateGCEInstanceBody replaces body_json / machine_type / networkInterfaces when provided.
func (s *Store) UpdateGCEInstanceBody(name, machineType, networkInterfacesJSON, bodyJSON string) (GCEInstance, bool, error) {
	cur, ok, err := s.GetGCEInstance(name)
	if err != nil || !ok {
		return GCEInstance{}, ok, err
	}
	if machineType != "" {
		cur.MachineType = machineType
	}
	if networkInterfacesJSON != "" {
		cur.NetworkInterfacesJSON = networkInterfacesJSON
	}
	if bodyJSON != "" {
		cur.BodyJSON = bodyJSON
	}
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(
		`UPDATE gce_instances SET machine_type = ?, network_interfaces_json = ?, body_json = ?, updated_at = ? WHERE name = ?`,
		cur.MachineType, cur.NetworkInterfacesJSON, cur.BodyJSON, cur.UpdatedAt, name,
	)
	if err != nil {
		return GCEInstance{}, false, fmt.Errorf("update gce instance: %w", err)
	}
	return cur, true, nil
}

// DeleteGCEInstance removes an instance.
func (s *Store) DeleteGCEInstance(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM gce_instances WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete gce instance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateGCENetwork inserts a network. created=false means already exists.
func (s *Store) CreateGCENetwork(net GCENetwork) (created bool, err error) {
	if net.Name == "" || net.ProjectID == "" || net.NetworkID == "" {
		return false, fmt.Errorf("gce network requires name, project, and network id")
	}
	if net.BodyJSON == "" {
		net.BodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if net.CreatedAt == "" {
		net.CreatedAt = now
	}
	if net.UpdatedAt == "" {
		net.UpdatedAt = net.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO gce_networks (name, project_id, network_id, body_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		net.Name, net.ProjectID, net.NetworkID, net.BodyJSON, net.CreatedAt, net.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create gce network: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetGCENetwork loads a network by resource name.
func (s *Store) GetGCENetwork(name string) (GCENetwork, bool, error) {
	var net GCENetwork
	err := s.db.QueryRow(
		`SELECT name, project_id, network_id, body_json, created_at, updated_at FROM gce_networks WHERE name = ?`, name,
	).Scan(&net.Name, &net.ProjectID, &net.NetworkID, &net.BodyJSON, &net.CreatedAt, &net.UpdatedAt)
	if err == sql.ErrNoRows {
		return GCENetwork{}, false, nil
	}
	if err != nil {
		return GCENetwork{}, false, fmt.Errorf("get gce network: %w", err)
	}
	return net, true, nil
}

// ListGCENetworks lists networks for a project.
func (s *Store) ListGCENetworks(projectID string) ([]GCENetwork, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, network_id, body_json, created_at, updated_at
		 FROM gce_networks WHERE project_id = ? ORDER BY name`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list gce networks: %w", err)
	}
	defer rows.Close()
	var out []GCENetwork
	for rows.Next() {
		var net GCENetwork
		if err := rows.Scan(&net.Name, &net.ProjectID, &net.NetworkID, &net.BodyJSON, &net.CreatedAt, &net.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, net)
	}
	return out, rows.Err()
}

// UpdateGCENetworkBody replaces body_json.
func (s *Store) UpdateGCENetworkBody(name, bodyJSON string) (GCENetwork, bool, error) {
	if bodyJSON == "" {
		bodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`UPDATE gce_networks SET body_json = ?, updated_at = ? WHERE name = ?`, bodyJSON, now, name)
	if err != nil {
		return GCENetwork{}, false, fmt.Errorf("update gce network: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return GCENetwork{}, false, err
	}
	if n == 0 {
		return GCENetwork{}, false, nil
	}
	return s.GetGCENetwork(name)
}

// DeleteGCENetwork removes a network.
func (s *Store) DeleteGCENetwork(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM gce_networks WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete gce network: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateGCESubnetwork inserts a subnetwork. created=false means already exists.
func (s *Store) CreateGCESubnetwork(sub GCESubnetwork) (created bool, err error) {
	if sub.Name == "" || sub.ProjectID == "" || sub.Region == "" || sub.SubnetworkID == "" {
		return false, fmt.Errorf("gce subnetwork requires name, project, region, and subnetwork id")
	}
	if sub.BodyJSON == "" {
		sub.BodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if sub.CreatedAt == "" {
		sub.CreatedAt = now
	}
	if sub.UpdatedAt == "" {
		sub.UpdatedAt = sub.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO gce_subnetworks
		 (name, project_id, region, subnetwork_id, network, ip_cidr_range, body_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sub.Name, sub.ProjectID, sub.Region, sub.SubnetworkID, sub.Network, sub.IPCidrRange,
		sub.BodyJSON, sub.CreatedAt, sub.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create gce subnetwork: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetGCESubnetwork loads a subnetwork by resource name.
func (s *Store) GetGCESubnetwork(name string) (GCESubnetwork, bool, error) {
	var sub GCESubnetwork
	err := s.db.QueryRow(
		`SELECT name, project_id, region, subnetwork_id, network, ip_cidr_range, body_json, created_at, updated_at
		 FROM gce_subnetworks WHERE name = ?`, name,
	).Scan(
		&sub.Name, &sub.ProjectID, &sub.Region, &sub.SubnetworkID, &sub.Network, &sub.IPCidrRange,
		&sub.BodyJSON, &sub.CreatedAt, &sub.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return GCESubnetwork{}, false, nil
	}
	if err != nil {
		return GCESubnetwork{}, false, fmt.Errorf("get gce subnetwork: %w", err)
	}
	return sub, true, nil
}

// ListGCESubnetworks lists subnetworks in a project/region.
func (s *Store) ListGCESubnetworks(projectID, region string) ([]GCESubnetwork, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, region, subnetwork_id, network, ip_cidr_range, body_json, created_at, updated_at
		 FROM gce_subnetworks WHERE project_id = ? AND region = ? ORDER BY name`,
		projectID, region,
	)
	if err != nil {
		return nil, fmt.Errorf("list gce subnetworks: %w", err)
	}
	defer rows.Close()
	var out []GCESubnetwork
	for rows.Next() {
		var sub GCESubnetwork
		if err := rows.Scan(
			&sub.Name, &sub.ProjectID, &sub.Region, &sub.SubnetworkID, &sub.Network, &sub.IPCidrRange,
			&sub.BodyJSON, &sub.CreatedAt, &sub.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// UpdateGCESubnetworkBody replaces mutable subnet fields.
func (s *Store) UpdateGCESubnetworkBody(name, network, ipCidrRange, bodyJSON string) (GCESubnetwork, bool, error) {
	cur, ok, err := s.GetGCESubnetwork(name)
	if err != nil || !ok {
		return GCESubnetwork{}, ok, err
	}
	if network != "" {
		cur.Network = network
	}
	if ipCidrRange != "" {
		cur.IPCidrRange = ipCidrRange
	}
	if bodyJSON != "" {
		cur.BodyJSON = bodyJSON
	}
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(
		`UPDATE gce_subnetworks SET network = ?, ip_cidr_range = ?, body_json = ?, updated_at = ? WHERE name = ?`,
		cur.Network, cur.IPCidrRange, cur.BodyJSON, cur.UpdatedAt, name,
	)
	if err != nil {
		return GCESubnetwork{}, false, fmt.Errorf("update gce subnetwork: %w", err)
	}
	return cur, true, nil
}

// DeleteGCESubnetwork removes a subnetwork.
func (s *Store) DeleteGCESubnetwork(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM gce_subnetworks WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete gce subnetwork: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateGCEFirewall inserts a firewall. created=false means already exists.
func (s *Store) CreateGCEFirewall(fw GCEFirewall) (created bool, err error) {
	if fw.Name == "" || fw.ProjectID == "" || fw.FirewallID == "" {
		return false, fmt.Errorf("gce firewall requires name, project, and firewall id")
	}
	if fw.BodyJSON == "" {
		fw.BodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if fw.CreatedAt == "" {
		fw.CreatedAt = now
	}
	if fw.UpdatedAt == "" {
		fw.UpdatedAt = fw.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO gce_firewalls (name, project_id, firewall_id, network, body_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		fw.Name, fw.ProjectID, fw.FirewallID, fw.Network, fw.BodyJSON, fw.CreatedAt, fw.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create gce firewall: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetGCEFirewall loads a firewall by resource name.
func (s *Store) GetGCEFirewall(name string) (GCEFirewall, bool, error) {
	var fw GCEFirewall
	err := s.db.QueryRow(
		`SELECT name, project_id, firewall_id, network, body_json, created_at, updated_at FROM gce_firewalls WHERE name = ?`, name,
	).Scan(&fw.Name, &fw.ProjectID, &fw.FirewallID, &fw.Network, &fw.BodyJSON, &fw.CreatedAt, &fw.UpdatedAt)
	if err == sql.ErrNoRows {
		return GCEFirewall{}, false, nil
	}
	if err != nil {
		return GCEFirewall{}, false, fmt.Errorf("get gce firewall: %w", err)
	}
	return fw, true, nil
}

// ListGCEFirewalls lists firewalls for a project.
func (s *Store) ListGCEFirewalls(projectID string) ([]GCEFirewall, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, firewall_id, network, body_json, created_at, updated_at
		 FROM gce_firewalls WHERE project_id = ? ORDER BY name`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list gce firewalls: %w", err)
	}
	defer rows.Close()
	var out []GCEFirewall
	for rows.Next() {
		var fw GCEFirewall
		if err := rows.Scan(&fw.Name, &fw.ProjectID, &fw.FirewallID, &fw.Network, &fw.BodyJSON, &fw.CreatedAt, &fw.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, fw)
	}
	return out, rows.Err()
}

// UpdateGCEFirewallBody replaces network and body_json.
func (s *Store) UpdateGCEFirewallBody(name, network, bodyJSON string) (GCEFirewall, bool, error) {
	cur, ok, err := s.GetGCEFirewall(name)
	if err != nil || !ok {
		return GCEFirewall{}, ok, err
	}
	if network != "" {
		cur.Network = network
	}
	if bodyJSON != "" {
		cur.BodyJSON = bodyJSON
	}
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(
		`UPDATE gce_firewalls SET network = ?, body_json = ?, updated_at = ? WHERE name = ?`,
		cur.Network, cur.BodyJSON, cur.UpdatedAt, name,
	)
	if err != nil {
		return GCEFirewall{}, false, fmt.Errorf("update gce firewall: %w", err)
	}
	return cur, true, nil
}

// DeleteGCEFirewall removes a firewall.
func (s *Store) DeleteGCEFirewall(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM gce_firewalls WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete gce firewall: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// NewGCEResourceID returns a lab numeric id string.
func NewGCEResourceID() string {
	return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}
