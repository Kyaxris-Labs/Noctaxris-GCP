package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const accessContextManagerSchema = `
CREATE TABLE IF NOT EXISTS acm_access_policies (
  name TEXT PRIMARY KEY,
  policy_id TEXT NOT NULL UNIQUE,
  parent TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  scopes_json TEXT NOT NULL DEFAULT '[]',
  etag TEXT NOT NULL DEFAULT '',
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_acm_access_policies_parent
  ON acm_access_policies (parent);

CREATE TABLE IF NOT EXISTS acm_service_perimeters (
  name TEXT PRIMARY KEY,
  policy_name TEXT NOT NULL,
  perimeter_id TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  perimeter_type TEXT NOT NULL DEFAULT 'PERIMETER_TYPE_REGULAR',
  status_json TEXT NOT NULL DEFAULT '',
  spec_json TEXT NOT NULL DEFAULT '',
  use_explicit_dry_run_spec INTEGER NOT NULL DEFAULT 0,
  etag TEXT NOT NULL DEFAULT '',
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (policy_name, perimeter_id)
);

CREATE INDEX IF NOT EXISTS idx_acm_service_perimeters_policy
  ON acm_service_perimeters (policy_name);
`

// ErrVPCSCPerimeter is returned when optional VPC-SC enforce denies a cross-perimeter call.
var ErrVPCSCPerimeter = errors.New("Request is denied because of VPC Service Controls")

// MigrateAccessContextManager applies Access Context Manager / VPC-SC lite tables.
// Wire from Store.migrate as: if err := s.MigrateAccessContextManager(); err != nil { return err }
func (s *Store) MigrateAccessContextManager() error {
	if _, err := s.db.Exec(accessContextManagerSchema); err != nil {
		return fmt.Errorf("apply access context manager schema: %w", err)
	}
	return nil
}

func (s *Store) ensureACM() error {
	return s.MigrateAccessContextManager()
}

// VPCSCEnforceEnabled reports whether optional perimeter enforce is on
// (NOCTAXRIS_GCP_VPCSC_ENFORCE=1|true).
func VPCSCEnforceEnabled() bool {
	v := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_VPCSC_ENFORCE"))
	return v == "1" || strings.EqualFold(v, "true")
}

// AccessPolicy is an Access Context Manager accessPolicies row.
type AccessPolicy struct {
	Name       string
	PolicyID   string
	Parent     string
	Title      string
	ScopesJSON string
	Etag       string
	BodyJSON   string
	CreatedAt  string
	UpdatedAt  string
}

// ServicePerimeter is an Access Context Manager servicePerimeters row.
type ServicePerimeter struct {
	Name                  string
	PolicyName            string
	PerimeterID           string
	Title                 string
	Description           string
	PerimeterType         string
	StatusJSON            string
	SpecJSON              string
	UseExplicitDryRunSpec bool
	Etag                  string
	BodyJSON              string
	CreatedAt             string
	UpdatedAt             string
}

// AccessPolicyResourceName builds accessPolicies/{policyID}.
func AccessPolicyResourceName(policyID string) string {
	return "accessPolicies/" + policyID
}

// ServicePerimeterResourceName builds accessPolicies/{policyID}/servicePerimeters/{perimeterID}.
func ServicePerimeterResourceName(policyID, perimeterID string) string {
	return AccessPolicyResourceName(policyID) + "/servicePerimeters/" + perimeterID
}

// CreateAccessPolicy inserts an access policy. created=false on conflict.
func (s *Store) CreateAccessPolicy(p AccessPolicy) (bool, error) {
	if err := s.ensureACM(); err != nil {
		return false, err
	}
	p.Name = strings.TrimSpace(p.Name)
	p.PolicyID = strings.TrimSpace(p.PolicyID)
	p.Parent = strings.TrimSpace(p.Parent)
	if p.Name == "" || p.PolicyID == "" || p.Parent == "" {
		return false, fmt.Errorf("access policy name, policy_id, and parent required")
	}
	if p.ScopesJSON == "" {
		p.ScopesJSON = "[]"
	}
	if p.BodyJSON == "" {
		p.BodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if p.CreatedAt == "" {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.Etag == "" {
		p.Etag = "etag-" + p.PolicyID
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO acm_access_policies
		 (name, policy_id, parent, title, scopes_json, etag, body_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.PolicyID, p.Parent, p.Title, p.ScopesJSON, p.Etag, p.BodyJSON, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetAccessPolicy loads one access policy by resource name or policy id.
func (s *Store) GetAccessPolicy(nameOrID string) (AccessPolicy, bool, error) {
	if err := s.ensureACM(); err != nil {
		return AccessPolicy{}, false, err
	}
	nameOrID = strings.TrimSpace(nameOrID)
	if nameOrID == "" {
		return AccessPolicy{}, false, nil
	}
	name := nameOrID
	if !strings.HasPrefix(name, "accessPolicies/") {
		name = AccessPolicyResourceName(nameOrID)
	}
	var p AccessPolicy
	err := s.db.QueryRow(
		`SELECT name, policy_id, parent, title, scopes_json, etag, body_json, created_at, updated_at
		 FROM acm_access_policies WHERE name = ? OR policy_id = ?`,
		name, strings.TrimPrefix(name, "accessPolicies/"),
	).Scan(&p.Name, &p.PolicyID, &p.Parent, &p.Title, &p.ScopesJSON, &p.Etag, &p.BodyJSON, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AccessPolicy{}, false, nil
	}
	if err != nil {
		return AccessPolicy{}, false, err
	}
	return p, true, nil
}

// ListAccessPolicies lists policies, optionally filtered by parent (organizations/...).
func (s *Store) ListAccessPolicies(parent string) ([]AccessPolicy, error) {
	if err := s.ensureACM(); err != nil {
		return nil, err
	}
	parent = strings.TrimSpace(parent)
	var (
		rows *sql.Rows
		err  error
	)
	if parent == "" {
		rows, err = s.db.Query(
			`SELECT name, policy_id, parent, title, scopes_json, etag, body_json, created_at, updated_at
			 FROM acm_access_policies ORDER BY name`)
	} else {
		rows, err = s.db.Query(
			`SELECT name, policy_id, parent, title, scopes_json, etag, body_json, created_at, updated_at
			 FROM acm_access_policies WHERE parent = ? ORDER BY name`, parent)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccessPolicy
	for rows.Next() {
		var p AccessPolicy
		if err := rows.Scan(&p.Name, &p.PolicyID, &p.Parent, &p.Title, &p.ScopesJSON, &p.Etag, &p.BodyJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateAccessPolicy patches title/scopes when the corresponding flags are set.
func (s *Store) UpdateAccessPolicy(name, title, scopesJSON string, updateTitle, updateScopes bool) (AccessPolicy, bool, error) {
	if err := s.ensureACM(); err != nil {
		return AccessPolicy{}, false, err
	}
	cur, ok, err := s.GetAccessPolicy(name)
	if err != nil || !ok {
		return AccessPolicy{}, ok, err
	}
	if updateTitle {
		cur.Title = title
	}
	if updateScopes {
		if scopesJSON == "" {
			scopesJSON = "[]"
		}
		cur.ScopesJSON = scopesJSON
	}
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	cur.Etag = "etag-" + cur.PolicyID + "-" + cur.UpdatedAt
	_, err = s.db.Exec(
		`UPDATE acm_access_policies SET title = ?, scopes_json = ?, etag = ?, updated_at = ? WHERE name = ?`,
		cur.Title, cur.ScopesJSON, cur.Etag, cur.UpdatedAt, cur.Name,
	)
	if err != nil {
		return AccessPolicy{}, false, err
	}
	return cur, true, nil
}

// DeleteAccessPolicy deletes a policy and its perimeters.
func (s *Store) DeleteAccessPolicy(nameOrID string) (bool, error) {
	if err := s.ensureACM(); err != nil {
		return false, err
	}
	p, ok, err := s.GetAccessPolicy(nameOrID)
	if err != nil || !ok {
		return ok, err
	}
	if _, err := s.db.Exec(`DELETE FROM acm_service_perimeters WHERE policy_name = ?`, p.Name); err != nil {
		return false, err
	}
	res, err := s.db.Exec(`DELETE FROM acm_access_policies WHERE name = ?`, p.Name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateServicePerimeter inserts a perimeter under an access policy.
func (s *Store) CreateServicePerimeter(sp ServicePerimeter) (bool, error) {
	if err := s.ensureACM(); err != nil {
		return false, err
	}
	sp.Name = strings.TrimSpace(sp.Name)
	sp.PolicyName = strings.TrimSpace(sp.PolicyName)
	sp.PerimeterID = strings.TrimSpace(sp.PerimeterID)
	if sp.Name == "" || sp.PolicyName == "" || sp.PerimeterID == "" {
		return false, fmt.Errorf("service perimeter name, policy, and id required")
	}
	if sp.PerimeterType == "" {
		sp.PerimeterType = "PERIMETER_TYPE_REGULAR"
	}
	if sp.BodyJSON == "" {
		sp.BodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if sp.CreatedAt == "" {
		sp.CreatedAt = now
	}
	sp.UpdatedAt = now
	if sp.Etag == "" {
		sp.Etag = "etag-" + sp.PerimeterID
	}
	dry := 0
	if sp.UseExplicitDryRunSpec {
		dry = 1
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO acm_service_perimeters
		 (name, policy_name, perimeter_id, title, description, perimeter_type,
		  status_json, spec_json, use_explicit_dry_run_spec, etag, body_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sp.Name, sp.PolicyName, sp.PerimeterID, sp.Title, sp.Description, sp.PerimeterType,
		sp.StatusJSON, sp.SpecJSON, dry, sp.Etag, sp.BodyJSON, sp.CreatedAt, sp.UpdatedAt,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetServicePerimeter loads one perimeter by resource name.
func (s *Store) GetServicePerimeter(name string) (ServicePerimeter, bool, error) {
	if err := s.ensureACM(); err != nil {
		return ServicePerimeter{}, false, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ServicePerimeter{}, false, nil
	}
	var sp ServicePerimeter
	var dry int
	err := s.db.QueryRow(
		`SELECT name, policy_name, perimeter_id, title, description, perimeter_type,
		        status_json, spec_json, use_explicit_dry_run_spec, etag, body_json, created_at, updated_at
		 FROM acm_service_perimeters WHERE name = ?`, name,
	).Scan(&sp.Name, &sp.PolicyName, &sp.PerimeterID, &sp.Title, &sp.Description, &sp.PerimeterType,
		&sp.StatusJSON, &sp.SpecJSON, &dry, &sp.Etag, &sp.BodyJSON, &sp.CreatedAt, &sp.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ServicePerimeter{}, false, nil
	}
	if err != nil {
		return ServicePerimeter{}, false, err
	}
	sp.UseExplicitDryRunSpec = dry != 0
	return sp, true, nil
}

// ListServicePerimeters lists perimeters for an access policy resource name.
func (s *Store) ListServicePerimeters(policyName string) ([]ServicePerimeter, error) {
	if err := s.ensureACM(); err != nil {
		return nil, err
	}
	policyName = strings.TrimSpace(policyName)
	if !strings.HasPrefix(policyName, "accessPolicies/") {
		policyName = AccessPolicyResourceName(policyName)
	}
	rows, err := s.db.Query(
		`SELECT name, policy_name, perimeter_id, title, description, perimeter_type,
		        status_json, spec_json, use_explicit_dry_run_spec, etag, body_json, created_at, updated_at
		 FROM acm_service_perimeters WHERE policy_name = ? ORDER BY name`, policyName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServicePerimeter
	for rows.Next() {
		var sp ServicePerimeter
		var dry int
		if err := rows.Scan(&sp.Name, &sp.PolicyName, &sp.PerimeterID, &sp.Title, &sp.Description, &sp.PerimeterType,
			&sp.StatusJSON, &sp.SpecJSON, &dry, &sp.Etag, &sp.BodyJSON, &sp.CreatedAt, &sp.UpdatedAt); err != nil {
			return nil, err
		}
		sp.UseExplicitDryRunSpec = dry != 0
		out = append(out, sp)
	}
	return out, rows.Err()
}

// UpdateServicePerimeter replaces perimeter fields (full replace of provided columns).
func (s *Store) UpdateServicePerimeter(sp ServicePerimeter) (ServicePerimeter, bool, error) {
	if err := s.ensureACM(); err != nil {
		return ServicePerimeter{}, false, err
	}
	cur, ok, err := s.GetServicePerimeter(sp.Name)
	if err != nil || !ok {
		return ServicePerimeter{}, ok, err
	}
	if sp.Title != "" || sp.Title == cur.Title {
		cur.Title = sp.Title
	}
	cur.Description = sp.Description
	if sp.PerimeterType != "" {
		cur.PerimeterType = sp.PerimeterType
	}
	cur.StatusJSON = sp.StatusJSON
	cur.SpecJSON = sp.SpecJSON
	cur.UseExplicitDryRunSpec = sp.UseExplicitDryRunSpec
	if sp.BodyJSON != "" {
		cur.BodyJSON = sp.BodyJSON
	}
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	cur.Etag = "etag-" + cur.PerimeterID + "-" + cur.UpdatedAt
	dry := 0
	if cur.UseExplicitDryRunSpec {
		dry = 1
	}
	_, err = s.db.Exec(
		`UPDATE acm_service_perimeters SET title = ?, description = ?, perimeter_type = ?,
		 status_json = ?, spec_json = ?, use_explicit_dry_run_spec = ?, etag = ?, body_json = ?, updated_at = ?
		 WHERE name = ?`,
		cur.Title, cur.Description, cur.PerimeterType, cur.StatusJSON, cur.SpecJSON, dry,
		cur.Etag, cur.BodyJSON, cur.UpdatedAt, cur.Name,
	)
	if err != nil {
		return ServicePerimeter{}, false, err
	}
	return cur, true, nil
}

// DeleteServicePerimeter deletes one perimeter.
func (s *Store) DeleteServicePerimeter(name string) (bool, error) {
	if err := s.ensureACM(); err != nil {
		return false, err
	}
	res, err := s.db.Exec(`DELETE FROM acm_service_perimeters WHERE name = ?`, strings.TrimSpace(name))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

type perimeterConfig struct {
	Resources          []string `json:"resources"`
	RestrictedServices []string `json:"restrictedServices"`
}

// VPCSCDenyCrossPerimeter returns ErrVPCSCPerimeter when optional enforce is on and
// fromProject/toProject sit across an active perimeter that restricts service.
// Same-project calls always allow. Dry-run-only perimeters (spec + useExplicitDryRunSpec)
// participate only when enforce is enabled (optional dry-run enforce).
func (s *Store) VPCSCDenyCrossPerimeter(fromProject, toProject, service string) error {
	if !VPCSCEnforceEnabled() {
		return nil
	}
	fromProject = strings.TrimSpace(fromProject)
	toProject = strings.TrimSpace(toProject)
	service = strings.TrimSpace(service)
	if fromProject == "" || toProject == "" || service == "" {
		return nil
	}
	if fromProject == toProject {
		return nil
	}
	if err := s.ensureACM(); err != nil {
		return err
	}
	rows, err := s.db.Query(
		`SELECT status_json, spec_json, use_explicit_dry_run_spec FROM acm_service_perimeters`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var statusJSON, specJSON string
		var dry int
		if err := rows.Scan(&statusJSON, &specJSON, &dry); err != nil {
			return err
		}
		cfgJSON := strings.TrimSpace(statusJSON)
		if cfgJSON == "" || cfgJSON == "{}" || cfgJSON == "null" {
			// Optional dry-run enforce: treat spec as active when flag set and enforce env on.
			if dry == 0 {
				continue
			}
			cfgJSON = strings.TrimSpace(specJSON)
		}
		if cfgJSON == "" || cfgJSON == "{}" || cfgJSON == "null" {
			continue
		}
		var cfg perimeterConfig
		if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
			continue
		}
		if !restrictedServiceListed(cfg.RestrictedServices, service) {
			continue
		}
		fromIn := projectInPerimeterResources(cfg.Resources, fromProject)
		toIn := projectInPerimeterResources(cfg.Resources, toProject)
		if fromIn != toIn {
			return ErrVPCSCPerimeter
		}
	}
	return rows.Err()
}

func restrictedServiceListed(list []string, service string) bool {
	for _, s := range list {
		if strings.TrimSpace(s) == service {
			return true
		}
	}
	return false
}

func projectInPerimeterResources(resources []string, projectID string) bool {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return false
	}
	candidates := []string{
		projectID,
		"projects/" + projectID,
	}
	for _, r := range resources {
		r = strings.TrimSpace(r)
		for _, c := range candidates {
			if r == c {
				return true
			}
		}
	}
	return false
}

// ProjectIDFromServiceAccountEmail extracts the project id from
// name@PROJECT_ID.iam.gserviceaccount.com. Empty when not an SA email.
func ProjectIDFromServiceAccountEmail(email string) string {
	email = strings.TrimSpace(email)
	const suffix = ".iam.gserviceaccount.com"
	if !strings.HasSuffix(email, suffix) {
		return ""
	}
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return ""
	}
	host := email[at+1:]
	return strings.TrimSuffix(host, suffix)
}
