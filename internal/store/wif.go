package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// WorkloadIdentityPool is IAM WIF pool metadata theatre (not real federation).
type WorkloadIdentityPool struct {
	Name        string
	ProjectID   string
	Location    string
	PoolID      string
	DisplayName string
	Description string
	Disabled    bool
	State       string
	CreatedAt   string
	UpdatedAt   string
}

// WorkloadIdentityPoolProvider is a WIF provider metadata theatre row.
type WorkloadIdentityPoolProvider struct {
	Name         string
	PoolName     string
	ProviderID   string
	DisplayName  string
	Description  string
	Disabled     bool
	State        string
	AttributeMap      string // JSON theatre
	IssuerURI         string
	AllowedAudiences  []string
	CreatedAt         string
	UpdatedAt         string
}

const wifSchema = `
CREATE TABLE IF NOT EXISTS wif_pools (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  pool_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  disabled INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (project_id, location, pool_id)
);

CREATE TABLE IF NOT EXISTS wif_providers (
  name TEXT PRIMARY KEY,
  pool_name TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  disabled INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  attribute_map_json TEXT NOT NULL DEFAULT '{}',
  issuer_uri TEXT NOT NULL DEFAULT '',
  allowed_audiences_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (pool_name, provider_id)
);

CREATE INDEX IF NOT EXISTS idx_wif_pools_parent ON wif_pools (project_id, location);
CREATE INDEX IF NOT EXISTS idx_wif_providers_pool ON wif_providers (pool_name);
`

func (s *Store) migrateWIF() error {
	if _, err := s.db.Exec(wifSchema); err != nil {
		return fmt.Errorf("migrate wif: %w", err)
	}
	return nil
}

// CreateWIFPool inserts a workload identity pool.
func (s *Store) CreateWIFPool(projectID, location, poolID, displayName, description string, disabled bool) (WorkloadIdentityPool, error) {
	projectID = strings.TrimSpace(projectID)
	location = strings.TrimSpace(location)
	poolID = strings.TrimSpace(poolID)
	if projectID == "" || location == "" || poolID == "" {
		return WorkloadIdentityPool{}, fmt.Errorf("project, location, and pool id required")
	}
	if !validWIFID(poolID) {
		return WorkloadIdentityPool{}, fmt.Errorf("invalid workloadIdentityPoolId")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	name := fmt.Sprintf("projects/%s/locations/%s/workloadIdentityPools/%s", projectID, location, poolID)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO wif_pools
		 (name, project_id, location, pool_id, display_name, description, disabled, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'ACTIVE', ?, ?)`,
		name, projectID, location, poolID, displayName, description, boolToInt(disabled), now, now,
	)
	if err != nil {
		return WorkloadIdentityPool{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return WorkloadIdentityPool{}, err
	}
	if n == 0 {
		return WorkloadIdentityPool{}, ErrAlreadyExists
	}
	return WorkloadIdentityPool{
		Name: name, ProjectID: projectID, Location: location, PoolID: poolID,
		DisplayName: displayName, Description: description, Disabled: disabled,
		State: "ACTIVE", CreatedAt: now, UpdatedAt: now,
	}, nil
}

// GetWIFPool loads a pool by resource name.
func (s *Store) GetWIFPool(name string) (WorkloadIdentityPool, bool, error) {
	var p WorkloadIdentityPool
	var disabled int
	err := s.db.QueryRow(
		`SELECT name, project_id, location, pool_id, display_name, description, disabled, state, created_at, updated_at
		 FROM wif_pools WHERE name = ?`, name,
	).Scan(&p.Name, &p.ProjectID, &p.Location, &p.PoolID, &p.DisplayName, &p.Description,
		&disabled, &p.State, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return WorkloadIdentityPool{}, false, nil
	}
	if err != nil {
		return WorkloadIdentityPool{}, false, err
	}
	p.Disabled = disabled != 0
	return p, true, nil
}

// ListWIFPools lists non-deleted pools under project/location.
func (s *Store) ListWIFPools(projectID, location string, showDeleted bool) ([]WorkloadIdentityPool, error) {
	q := `SELECT name, project_id, location, pool_id, display_name, description, disabled, state, created_at, updated_at
	      FROM wif_pools WHERE project_id = ? AND location = ?`
	if !showDeleted {
		q += ` AND state = 'ACTIVE'`
	}
	q += ` ORDER BY pool_id`
	rows, err := s.db.Query(q, projectID, location)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkloadIdentityPool
	for rows.Next() {
		var p WorkloadIdentityPool
		var disabled int
		if err := rows.Scan(&p.Name, &p.ProjectID, &p.Location, &p.PoolID, &p.DisplayName, &p.Description,
			&disabled, &p.State, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Disabled = disabled != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteWIFPool soft-deletes a pool.
func (s *Store) DeleteWIFPool(name string) (WorkloadIdentityPool, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE wif_pools SET state = 'DELETED', updated_at = ? WHERE name = ? AND state = 'ACTIVE'`,
		now, name,
	)
	if err != nil {
		return WorkloadIdentityPool{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return WorkloadIdentityPool{}, false, err
	}
	if n == 0 {
		return WorkloadIdentityPool{}, false, nil
	}
	return s.GetWIFPool(name)
}

// CreateWIFProvider inserts a provider under a pool.
func (s *Store) CreateWIFProvider(poolName, providerID, displayName, description, issuerURI, attributeMapJSON, allowedAudiencesJSON string, disabled bool) (WorkloadIdentityPoolProvider, error) {
	poolName = strings.TrimSpace(poolName)
	providerID = strings.TrimSpace(providerID)
	if poolName == "" || providerID == "" {
		return WorkloadIdentityPoolProvider{}, fmt.Errorf("pool name and provider id required")
	}
	if !validWIFID(providerID) {
		return WorkloadIdentityPoolProvider{}, fmt.Errorf("invalid workloadIdentityPoolProviderId")
	}
	pool, ok, err := s.GetWIFPool(poolName)
	if err != nil {
		return WorkloadIdentityPoolProvider{}, err
	}
	if !ok || pool.State != "ACTIVE" {
		return WorkloadIdentityPoolProvider{}, fmt.Errorf("pool not found")
	}
	if attributeMapJSON == "" {
		attributeMapJSON = "{}"
	}
	audJSON, err := normalizeAllowedAudiencesJSON(allowedAudiencesJSON)
	if err != nil {
		return WorkloadIdentityPoolProvider{}, fmt.Errorf("invalid allowedAudiences")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	name := poolName + "/providers/" + providerID
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO wif_providers
		 (name, pool_name, provider_id, display_name, description, disabled, state, attribute_map_json, issuer_uri, allowed_audiences_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'ACTIVE', ?, ?, ?, ?, ?)`,
		name, poolName, providerID, displayName, description, boolToInt(disabled), attributeMapJSON, issuerURI, audJSON, now, now,
	)
	if err != nil {
		return WorkloadIdentityPoolProvider{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return WorkloadIdentityPoolProvider{}, err
	}
	if n == 0 {
		return WorkloadIdentityPoolProvider{}, ErrAlreadyExists
	}
	audiences, err := parseAllowedAudiencesJSON(audJSON)
	if err != nil {
		return WorkloadIdentityPoolProvider{}, err
	}
	return WorkloadIdentityPoolProvider{
		Name: name, PoolName: poolName, ProviderID: providerID,
		DisplayName: displayName, Description: description, Disabled: disabled,
		State: "ACTIVE", AttributeMap: attributeMapJSON, IssuerURI: issuerURI,
		AllowedAudiences: audiences,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// GetWIFProvider loads a provider by resource name.
func (s *Store) GetWIFProvider(name string) (WorkloadIdentityPoolProvider, bool, error) {
	var p WorkloadIdentityPoolProvider
	var disabled int
	var audJSON string
	err := s.db.QueryRow(
		`SELECT name, pool_name, provider_id, display_name, description, disabled, state,
		 COALESCE(attribute_map_json, '{}'), COALESCE(issuer_uri, ''), COALESCE(allowed_audiences_json, '[]'), created_at, updated_at
		 FROM wif_providers WHERE name = ?`, name,
	).Scan(&p.Name, &p.PoolName, &p.ProviderID, &p.DisplayName, &p.Description,
		&disabled, &p.State, &p.AttributeMap, &p.IssuerURI, &audJSON, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return WorkloadIdentityPoolProvider{}, false, nil
	}
	if err != nil {
		return WorkloadIdentityPoolProvider{}, false, err
	}
	p.Disabled = disabled != 0
	p.AllowedAudiences, err = parseAllowedAudiencesJSON(audJSON)
	if err != nil {
		return WorkloadIdentityPoolProvider{}, false, err
	}
	return p, true, nil
}

// ListWIFProviders lists providers under a pool.
func (s *Store) ListWIFProviders(poolName string, showDeleted bool) ([]WorkloadIdentityPoolProvider, error) {
	q := `SELECT name, pool_name, provider_id, display_name, description, disabled, state,
	      COALESCE(attribute_map_json, '{}'), COALESCE(issuer_uri, ''), COALESCE(allowed_audiences_json, '[]'), created_at, updated_at
	      FROM wif_providers WHERE pool_name = ?`
	if !showDeleted {
		q += ` AND state = 'ACTIVE'`
	}
	q += ` ORDER BY provider_id`
	rows, err := s.db.Query(q, poolName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkloadIdentityPoolProvider
	for rows.Next() {
		var p WorkloadIdentityPoolProvider
		var disabled int
		var audJSON string
		if err := rows.Scan(&p.Name, &p.PoolName, &p.ProviderID, &p.DisplayName, &p.Description,
			&disabled, &p.State, &p.AttributeMap, &p.IssuerURI, &audJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Disabled = disabled != 0
		p.AllowedAudiences, err = parseAllowedAudiencesJSON(audJSON)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteWIFProvider soft-deletes a provider.
func (s *Store) DeleteWIFProvider(name string) (WorkloadIdentityPoolProvider, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE wif_providers SET state = 'DELETED', updated_at = ? WHERE name = ? AND state = 'ACTIVE'`,
		now, name,
	)
	if err != nil {
		return WorkloadIdentityPoolProvider{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return WorkloadIdentityPoolProvider{}, false, err
	}
	if n == 0 {
		return WorkloadIdentityPoolProvider{}, false, nil
	}
	return s.GetWIFProvider(name)
}

// UpdateWIFProvider patches provider fields selected by the update* flags.
func (s *Store) UpdateWIFProvider(
	name, displayName, description, issuerURI, attributeMapJSON, allowedAudiencesJSON string, disabled bool,
	updateDisplay, updateDescription, updateIssuer, updateAttr, updateAudiences, updateDisabled bool,
) (WorkloadIdentityPoolProvider, bool, error) {
	p, ok, err := s.GetWIFProvider(name)
	if err != nil || !ok || p.State != "ACTIVE" {
		return WorkloadIdentityPoolProvider{}, false, err
	}
	if updateDisplay {
		p.DisplayName = displayName
	}
	if updateDescription {
		p.Description = description
	}
	if updateIssuer {
		p.IssuerURI = issuerURI
	}
	if updateAttr {
		if attributeMapJSON == "" {
			attributeMapJSON = "{}"
		}
		p.AttributeMap = attributeMapJSON
	}
	if updateAudiences {
		audJSON, err := normalizeAllowedAudiencesJSON(allowedAudiencesJSON)
		if err != nil {
			return WorkloadIdentityPoolProvider{}, false, fmt.Errorf("invalid allowedAudiences")
		}
		p.AllowedAudiences, err = parseAllowedAudiencesJSON(audJSON)
		if err != nil {
			return WorkloadIdentityPoolProvider{}, false, err
		}
	}
	if updateDisabled {
		p.Disabled = disabled
	}
	audJSON, err := normalizeAllowedAudiencesFromList(p.AllowedAudiences)
	if err != nil {
		return WorkloadIdentityPoolProvider{}, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE wif_providers SET display_name = ?, description = ?, disabled = ?, attribute_map_json = ?,
		 issuer_uri = ?, allowed_audiences_json = ?, updated_at = ?
		 WHERE name = ? AND state = 'ACTIVE'`,
		p.DisplayName, p.Description, boolToInt(p.Disabled), p.AttributeMap, p.IssuerURI, audJSON, now, name,
	)
	if err != nil {
		return WorkloadIdentityPoolProvider{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return WorkloadIdentityPoolProvider{}, false, err
	}
	if n == 0 {
		return WorkloadIdentityPoolProvider{}, false, nil
	}
	return s.GetWIFProvider(name)
}

func normalizeAllowedAudiencesJSON(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "[]", nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return "", err
	}
	return normalizeAllowedAudiencesFromList(list)
}

func normalizeAllowedAudiencesFromList(list []string) (string, error) {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(list))
	for _, a := range list {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func parseAllowedAudiencesJSON(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}, nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(list))
	for _, a := range list {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	return out, nil
}

func validWIFID(id string) bool {
	if len(id) < 4 || len(id) > 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
		if !ok {
			return false
		}
	}
	return id[0] >= 'a' && id[0] <= 'z'
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// NewLabID returns a short unique id for lab resources.
func NewLabID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
}
