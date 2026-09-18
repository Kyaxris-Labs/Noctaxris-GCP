package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const labExtrasSchema = `
CREATE TABLE IF NOT EXISTS gcs_hmac_keys (
  access_id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  service_account_email TEXT NOT NULL,
  secret TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS identity_tenants (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  allow_password_signup INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  UNIQUE (project_id, tenant_id)
);

CREATE TABLE IF NOT EXISTS cb_worker_pools (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  pool_id TEXT NOT NULL,
  annotations_json TEXT NOT NULL DEFAULT '{}',
  config_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE (project_id, location, pool_id)
);

CREATE TABLE IF NOT EXISTS container_occurrences (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  occurrence_id TEXT NOT NULL,
  resource_uri TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'ATTESTATION',
  note_name TEXT NOT NULL DEFAULT '',
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS binaryauthz_policies (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  enforcement_mode TEXT NOT NULL DEFAULT 'ENFORCED_BLOCK_AND_AUDIT_LOG',
  body_json TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL
);
`

func (s *Store) migrateLabExtras() error {
	if _, err := s.db.Exec(labExtrasSchema); err != nil {
		return fmt.Errorf("migrate lab extras: %w", err)
	}
	if _, err := s.db.Exec(`ALTER TABLE firebase_users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	return nil
}

// HMACKey is a Cloud Storage HMAC key metadata row.
type HMACKey struct {
	AccessID             string
	ProjectID            string
	ServiceAccountEmail  string
	Secret               string
	State                string
	CreatedAt            string
}

func (s *Store) CreateHMACKey(projectID, saEmail string) (*HMACKey, error) {
	if err := s.migrateLabExtras(); err != nil {
		return nil, err
	}
	projectID = strings.TrimSpace(projectID)
	saEmail = strings.TrimSpace(saEmail)
	if projectID == "" || saEmail == "" {
		return nil, fmt.Errorf("project and serviceAccountEmail required")
	}
	var b [20]byte
	_, _ = rand.Read(b[:])
	accessID := "GOOG1" + strings.ToUpper(hex.EncodeToString(b[:]))
	var sb [20]byte
	_, _ = rand.Read(sb[:])
	secret := hex.EncodeToString(sb[:])
	now := time.Now().UTC().Format(time.RFC3339Nano)
	k := &HMACKey{
		AccessID: accessID, ProjectID: projectID, ServiceAccountEmail: saEmail,
		Secret: secret, State: "ACTIVE", CreatedAt: now,
	}
	_, err := s.db.Exec(
		`INSERT INTO gcs_hmac_keys (access_id, project_id, service_account_email, secret, state, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		k.AccessID, k.ProjectID, k.ServiceAccountEmail, k.Secret, k.State, k.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (s *Store) GetHMACKey(accessID string) (*HMACKey, bool, error) {
	if err := s.migrateLabExtras(); err != nil {
		return nil, false, err
	}
	if accessID == LabGCSHMACAccessID {
		return &HMACKey{
			AccessID: LabGCSHMACAccessID, Secret: LabGCSHMACSecret, State: "ACTIVE",
			ServiceAccountEmail: LabGCSHMACAccessID + "@lab.local",
		}, true, nil
	}
	var k HMACKey
	err := s.db.QueryRow(
		`SELECT access_id, project_id, service_account_email, secret, state, created_at FROM gcs_hmac_keys WHERE access_id = ?`,
		accessID,
	).Scan(&k.AccessID, &k.ProjectID, &k.ServiceAccountEmail, &k.Secret, &k.State, &k.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &k, true, nil
}

func (s *Store) ListHMACKeys(projectID string) ([]HMACKey, error) {
	if err := s.migrateLabExtras(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT access_id, project_id, service_account_email, secret, state, created_at
		 FROM gcs_hmac_keys WHERE project_id = ? ORDER BY created_at`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HMACKey
	for rows.Next() {
		var k HMACKey
		if err := rows.Scan(&k.AccessID, &k.ProjectID, &k.ServiceAccountEmail, &k.Secret, &k.State, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteHMACKey(accessID string) (bool, error) {
	if err := s.migrateLabExtras(); err != nil {
		return false, err
	}
	res, err := s.db.Exec(`DELETE FROM gcs_hmac_keys WHERE access_id = ?`, accessID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) HMACSecret(accessID string) (string, bool, error) {
	k, ok, err := s.GetHMACKey(accessID)
	if err != nil || !ok {
		return "", ok, err
	}
	if k.State != "ACTIVE" {
		return "", false, nil
	}
	return k.Secret, true, nil
}

// IdentityTenant is an Identity Platform tenant.
type IdentityTenant struct {
	Name                string
	ProjectID           string
	TenantID            string
	DisplayName         string
	AllowPasswordSignup bool
	CreatedAt           string
}

func (s *Store) CreateIdentityTenant(t IdentityTenant) (*IdentityTenant, bool, error) {
	if err := s.migrateLabExtras(); err != nil {
		return nil, false, err
	}
	t.ProjectID = strings.TrimSpace(t.ProjectID)
	t.TenantID = strings.TrimSpace(t.TenantID)
	if t.ProjectID == "" || t.TenantID == "" {
		return nil, false, fmt.Errorf("project and tenantId required")
	}
	t.Name = "projects/" + t.ProjectID + "/tenants/" + t.TenantID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	t.CreatedAt = now
	allow := 1
	if !t.AllowPasswordSignup {
		allow = 0
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO identity_tenants (name, project_id, tenant_id, display_name, allow_password_signup, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		t.Name, t.ProjectID, t.TenantID, t.DisplayName, allow, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, false, nil
	}
	return &t, true, nil
}

func (s *Store) GetIdentityTenant(projectID, tenantID string) (*IdentityTenant, bool, error) {
	if err := s.migrateLabExtras(); err != nil {
		return nil, false, err
	}
	var t IdentityTenant
	var allow int
	err := s.db.QueryRow(
		`SELECT name, project_id, tenant_id, display_name, allow_password_signup, created_at
		 FROM identity_tenants WHERE project_id = ? AND tenant_id = ?`,
		projectID, tenantID,
	).Scan(&t.Name, &t.ProjectID, &t.TenantID, &t.DisplayName, &allow, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	t.AllowPasswordSignup = allow != 0
	return &t, true, nil
}

func (s *Store) ListIdentityTenants(projectID string) ([]IdentityTenant, error) {
	if err := s.migrateLabExtras(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT name, project_id, tenant_id, display_name, allow_password_signup, created_at
		 FROM identity_tenants WHERE project_id = ? ORDER BY tenant_id`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IdentityTenant
	for rows.Next() {
		var t IdentityTenant
		var allow int
		if err := rows.Scan(&t.Name, &t.ProjectID, &t.TenantID, &t.DisplayName, &allow, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.AllowPasswordSignup = allow != 0
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) PatchIdentityTenant(projectID, tenantID string, allow *bool, displayName *string) (*IdentityTenant, bool, error) {
	t, ok, err := s.GetIdentityTenant(projectID, tenantID)
	if err != nil || !ok {
		return t, ok, err
	}
	if allow != nil {
		t.AllowPasswordSignup = *allow
	}
	if displayName != nil {
		t.DisplayName = *displayName
	}
	av := 0
	if t.AllowPasswordSignup {
		av = 1
	}
	_, err = s.db.Exec(
		`UPDATE identity_tenants SET allow_password_signup = ?, display_name = ? WHERE name = ?`,
		av, t.DisplayName, t.Name,
	)
	if err != nil {
		return nil, false, err
	}
	return t, true, nil
}

// CbWorkerPool is a Cloud Build private pool.
type CbWorkerPool struct {
	Name            string
	ProjectID       string
	Location        string
	PoolID          string
	AnnotationsJSON string
	ConfigJSON      string
	CreatedAt       string
}

func (s *Store) CreateCbWorkerPool(p CbWorkerPool) (*CbWorkerPool, bool, error) {
	if err := s.migrateLabExtras(); err != nil {
		return nil, false, err
	}
	if p.ProjectID == "" || p.Location == "" || p.PoolID == "" {
		return nil, false, fmt.Errorf("project, location, and workerPoolId required")
	}
	p.Name = fmt.Sprintf("projects/%s/locations/%s/workerPools/%s", p.ProjectID, p.Location, p.PoolID)
	if p.AnnotationsJSON == "" {
		p.AnnotationsJSON = "{}"
	}
	if p.ConfigJSON == "" {
		p.ConfigJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	p.CreatedAt = now
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cb_worker_pools (name, project_id, location, pool_id, annotations_json, config_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.ProjectID, p.Location, p.PoolID, p.AnnotationsJSON, p.ConfigJSON, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, false, nil
	}
	return &p, true, nil
}

func (s *Store) GetCbWorkerPool(name string) (*CbWorkerPool, bool, error) {
	if err := s.migrateLabExtras(); err != nil {
		return nil, false, err
	}
	var p CbWorkerPool
	err := s.db.QueryRow(
		`SELECT name, project_id, location, pool_id, annotations_json, config_json, created_at FROM cb_worker_pools WHERE name = ?`,
		name,
	).Scan(&p.Name, &p.ProjectID, &p.Location, &p.PoolID, &p.AnnotationsJSON, &p.ConfigJSON, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &p, true, nil
}

func (s *Store) ListCbWorkerPools(projectID, location string) ([]CbWorkerPool, error) {
	if err := s.migrateLabExtras(); err != nil {
		return nil, err
	}
	q := `SELECT name, project_id, location, pool_id, annotations_json, config_json, created_at FROM cb_worker_pools WHERE project_id = ?`
	args := []any{projectID}
	if location != "" {
		q += ` AND location = ?`
		args = append(args, location)
	}
	q += ` ORDER BY pool_id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CbWorkerPool
	for rows.Next() {
		var p CbWorkerPool
		if err := rows.Scan(&p.Name, &p.ProjectID, &p.Location, &p.PoolID, &p.AnnotationsJSON, &p.ConfigJSON, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ContainerOccurrence is a Container Analysis occurrence.
type ContainerOccurrence struct {
	Name         string
	ProjectID    string
	OccurrenceID string
	ResourceURI  string
	Kind         string
	NoteName     string
	BodyJSON     string
	CreatedAt    string
}

func (s *Store) PutContainerOccurrence(o ContainerOccurrence) error {
	if err := s.migrateLabExtras(); err != nil {
		return err
	}
	if o.Name == "" || o.ProjectID == "" {
		return fmt.Errorf("occurrence name and project required")
	}
	if o.CreatedAt == "" {
		o.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if o.BodyJSON == "" {
		o.BodyJSON = "{}"
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO container_occurrences
		 (name, project_id, occurrence_id, resource_uri, kind, note_name, body_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		o.Name, o.ProjectID, o.OccurrenceID, o.ResourceURI, o.Kind, o.NoteName, o.BodyJSON, o.CreatedAt,
	)
	return err
}

func (s *Store) GetContainerOccurrence(name string) (*ContainerOccurrence, bool, error) {
	if err := s.migrateLabExtras(); err != nil {
		return nil, false, err
	}
	var o ContainerOccurrence
	err := s.db.QueryRow(
		`SELECT name, project_id, occurrence_id, resource_uri, kind, note_name, body_json, created_at
		 FROM container_occurrences WHERE name = ?`, name,
	).Scan(&o.Name, &o.ProjectID, &o.OccurrenceID, &o.ResourceURI, &o.Kind, &o.NoteName, &o.BodyJSON, &o.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &o, true, nil
}

func (s *Store) ListContainerOccurrences(projectID, resourceURI string) ([]ContainerOccurrence, error) {
	if err := s.migrateLabExtras(); err != nil {
		return nil, err
	}
	q := `SELECT name, project_id, occurrence_id, resource_uri, kind, note_name, body_json, created_at
	      FROM container_occurrences WHERE project_id = ?`
	args := []any{projectID}
	if resourceURI != "" {
		q += ` AND resource_uri = ?`
		args = append(args, resourceURI)
	}
	q += ` ORDER BY created_at`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContainerOccurrence
	for rows.Next() {
		var o ContainerOccurrence
		if err := rows.Scan(&o.Name, &o.ProjectID, &o.OccurrenceID, &o.ResourceURI, &o.Kind, &o.NoteName, &o.BodyJSON, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) PutBinaryAuthzPolicy(projectID, mode, bodyJSON string) error {
	if err := s.migrateLabExtras(); err != nil {
		return err
	}
	name := "projects/" + projectID + "/policy"
	if mode == "" {
		mode = "ENFORCED_BLOCK_AND_AUDIT_LOG"
	}
	if bodyJSON == "" {
		bodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO binaryauthz_policies (name, project_id, enforcement_mode, body_json, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET enforcement_mode = excluded.enforcement_mode, body_json = excluded.body_json, updated_at = excluded.updated_at`,
		name, projectID, mode, bodyJSON, now,
	)
	return err
}

func (s *Store) GetBinaryAuthzPolicy(projectID string) (mode, bodyJSON string, ok bool, err error) {
	if err = s.migrateLabExtras(); err != nil {
		return "", "", false, err
	}
	err = s.db.QueryRow(
		`SELECT enforcement_mode, body_json FROM binaryauthz_policies WHERE project_id = ?`,
		projectID,
	).Scan(&mode, &bodyJSON)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return mode, bodyJSON, true, nil
}

// BinaryAuthzAllows reports whether imageURI may deploy under the project policy.
func (s *Store) BinaryAuthzAllows(projectID, imageURI string) (bool, error) {
	mode, _, ok, err := s.GetBinaryAuthzPolicy(projectID)
	if err != nil {
		return false, err
	}
	if !ok {
		return true, nil
	}
	if !strings.Contains(strings.ToUpper(mode), "ENFORCED") {
		return true, nil
	}
	imageURI = strings.TrimSpace(imageURI)
	if imageURI == "" {
		return false, nil
	}
	list, err := s.ListContainerOccurrences(projectID, imageURI)
	if err != nil {
		return false, err
	}
	return len(list) > 0, nil
}
