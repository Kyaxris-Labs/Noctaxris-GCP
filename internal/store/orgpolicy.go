package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Lab Organization Policy constraints (boolean theatre).
const (
	ConstraintDisableServiceAccountKeyCreation = "iam.disableServiceAccountKeyCreation"
	ConstraintStoragePublicAccessPrevention    = "storage.publicAccessPrevention"
)

// OrgPolicy is a stored organization policy for one constraint on one parent.
type OrgPolicy struct {
	Name       string // {parent}/policies/{constraint}
	Parent     string // projects/... | folders/... | organizations/...
	Constraint string // e.g. iam.disableServiceAccountKeyCreation
	SpecJSON   string // PolicySpec JSON
	Etag       string
	UpdatedAt  string
}

const orgPolicySchema = `
CREATE TABLE IF NOT EXISTS org_policies (
  name TEXT PRIMARY KEY,
  parent TEXT NOT NULL,
  constraint_id TEXT NOT NULL,
  spec_json TEXT NOT NULL DEFAULT '{}',
  etag TEXT NOT NULL DEFAULT 'ACAB',
  updated_at TEXT NOT NULL,
  UNIQUE (parent, constraint_id)
);

CREATE INDEX IF NOT EXISTS idx_org_policies_parent ON org_policies (parent);
`

// migrateOrgPolicy creates Organization Policy tables.
// Wire from Store.migrate: if err := s.migrateOrgPolicy(); err != nil { return err }
func (s *Store) migrateOrgPolicy() error {
	if _, err := s.db.Exec(orgPolicySchema); err != nil {
		return fmt.Errorf("migrate org policy: %w", err)
	}
	return nil
}

func (s *Store) ensureOrgPolicySchema() error {
	return s.migrateOrgPolicy()
}

// NormalizeOrgConstraint strips an optional "constraints/" prefix.
func NormalizeOrgConstraint(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	constraint = strings.TrimPrefix(constraint, "constraints/")
	return strings.TrimSpace(constraint)
}

// KnownOrgPolicyConstraints returns the lab-supported constraint ids.
func KnownOrgPolicyConstraints() []string {
	return []string{
		ConstraintDisableServiceAccountKeyCreation,
		ConstraintStoragePublicAccessPrevention,
	}
}

// IsKnownOrgConstraint reports whether constraint is a lab-supported id.
func IsKnownOrgConstraint(constraint string) bool {
	c := NormalizeOrgConstraint(constraint)
	for _, k := range KnownOrgPolicyConstraints() {
		if k == c {
			return true
		}
	}
	return false
}

func orgPolicyName(parent, constraint string) string {
	return strings.TrimSuffix(parent, "/") + "/policies/" + NormalizeOrgConstraint(constraint)
}

// SetOrgPolicy upserts a policy on parent for constraint.
// specJSON must be PolicySpec-shaped JSON (rules with boolean enforce).
func (s *Store) SetOrgPolicy(parent, constraint, specJSON string) (OrgPolicy, error) {
	if err := s.ensureOrgPolicySchema(); err != nil {
		return OrgPolicy{}, err
	}
	parent = strings.TrimSpace(parent)
	constraint = NormalizeOrgConstraint(constraint)
	if parent == "" || constraint == "" {
		return OrgPolicy{}, fmt.Errorf("parent and constraint required")
	}
	if !validOrgPolicyParent(parent) {
		return OrgPolicy{}, fmt.Errorf("parent must be projects/{id}, folders/{id}, or organizations/{id}")
	}
	if !IsKnownOrgConstraint(constraint) {
		return OrgPolicy{}, fmt.Errorf("unknown constraint %q", constraint)
	}
	if strings.TrimSpace(specJSON) == "" {
		specJSON = `{"rules":[{"enforce":false}]}`
	}
	if !json.Valid([]byte(specJSON)) {
		return OrgPolicy{}, fmt.Errorf("spec must be valid JSON")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	name := orgPolicyName(parent, constraint)
	_, err := s.db.Exec(
		`INSERT INTO org_policies (name, parent, constraint_id, spec_json, etag, updated_at)
		 VALUES (?, ?, ?, ?, 'ACAB', ?)
		 ON CONFLICT(parent, constraint_id) DO UPDATE SET
		   name = excluded.name,
		   spec_json = excluded.spec_json,
		   etag = 'ACAB',
		   updated_at = excluded.updated_at`,
		name, parent, constraint, specJSON, now,
	)
	if err != nil {
		return OrgPolicy{}, err
	}
	return OrgPolicy{
		Name: name, Parent: parent, Constraint: constraint,
		SpecJSON: specJSON, Etag: "ACAB", UpdatedAt: now,
	}, nil
}

// GetOrgPolicy loads a policy by parent + constraint.
func (s *Store) GetOrgPolicy(parent, constraint string) (OrgPolicy, bool, error) {
	if err := s.ensureOrgPolicySchema(); err != nil {
		return OrgPolicy{}, false, err
	}
	parent = strings.TrimSpace(parent)
	constraint = NormalizeOrgConstraint(constraint)
	var p OrgPolicy
	err := s.db.QueryRow(
		`SELECT name, parent, constraint_id, spec_json, etag, updated_at
		 FROM org_policies WHERE parent = ? AND constraint_id = ?`,
		parent, constraint,
	).Scan(&p.Name, &p.Parent, &p.Constraint, &p.SpecJSON, &p.Etag, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return OrgPolicy{}, false, nil
	}
	if err != nil {
		return OrgPolicy{}, false, err
	}
	return p, true, nil
}

// GetOrgPolicyByName loads a policy by resource name (.../policies/{constraint}).
func (s *Store) GetOrgPolicyByName(name string) (OrgPolicy, bool, error) {
	parent, constraint, ok := splitOrgPolicyName(name)
	if !ok {
		return OrgPolicy{}, false, nil
	}
	return s.GetOrgPolicy(parent, constraint)
}

// ListOrgPolicies lists policies set on parent (explicit rows only).
func (s *Store) ListOrgPolicies(parent string) ([]OrgPolicy, error) {
	if err := s.ensureOrgPolicySchema(); err != nil {
		return nil, err
	}
	parent = strings.TrimSpace(parent)
	rows, err := s.db.Query(
		`SELECT name, parent, constraint_id, spec_json, etag, updated_at
		 FROM org_policies WHERE parent = ? ORDER BY constraint_id`,
		parent,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgPolicy
	for rows.Next() {
		var p OrgPolicy
		if err := rows.Scan(&p.Name, &p.Parent, &p.Constraint, &p.SpecJSON, &p.Etag, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteOrgPolicy removes an explicit policy on parent for constraint.
func (s *Store) DeleteOrgPolicy(parent, constraint string) (bool, error) {
	if err := s.ensureOrgPolicySchema(); err != nil {
		return false, err
	}
	parent = strings.TrimSpace(parent)
	constraint = NormalizeOrgConstraint(constraint)
	res, err := s.db.Exec(
		`DELETE FROM org_policies WHERE parent = ? AND constraint_id = ?`,
		parent, constraint,
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

// IsOrgPolicyConstraintEnforced walks CRM ancestry from resource and returns
// whether the nearest explicit boolean policy enforces the constraint.
// Unset policies mean not enforced (lab Google-managed default for known constraints).
func (s *Store) IsOrgPolicyConstraintEnforced(resource, constraint string) (bool, error) {
	if err := s.ensureOrgPolicySchema(); err != nil {
		return false, err
	}
	constraint = NormalizeOrgConstraint(constraint)
	resource = strings.TrimSpace(resource)
	if resource == "" || constraint == "" {
		return false, nil
	}
	seen := map[string]bool{}
	cur := resource
	for i := 0; i < 32; i++ {
		if cur == "" || seen[cur] {
			break
		}
		seen[cur] = true
		p, ok, err := s.GetOrgPolicy(cur, constraint)
		if err != nil {
			return false, err
		}
		if ok {
			return booleanSpecEnforced(p.SpecJSON), nil
		}
		parent, hasParent, err := s.CRMParent(cur)
		if err != nil {
			return false, err
		}
		if !hasParent {
			break
		}
		cur = parent
	}
	return false, nil
}

func booleanSpecEnforced(specJSON string) bool {
	var spec struct {
		Reset bool `json:"reset"`
		Rules []struct {
			Enforce *bool `json:"enforce"`
		} `json:"rules"`
	}
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return false
	}
	if spec.Reset {
		return false
	}
	for _, r := range spec.Rules {
		if r.Enforce != nil {
			return *r.Enforce
		}
	}
	return false
}

func validOrgPolicyParent(parent string) bool {
	switch {
	case strings.HasPrefix(parent, "projects/"):
		rest := strings.TrimPrefix(parent, "projects/")
		return rest != "" && !strings.Contains(rest, "/")
	case strings.HasPrefix(parent, "folders/"):
		rest := strings.TrimPrefix(parent, "folders/")
		return rest != "" && !strings.Contains(rest, "/")
	case strings.HasPrefix(parent, "organizations/"):
		rest := strings.TrimPrefix(parent, "organizations/")
		return rest != "" && !strings.Contains(rest, "/")
	default:
		return false
	}
}

func splitOrgPolicyName(name string) (parent, constraint string, ok bool) {
	name = strings.TrimSpace(name)
	const marker = "/policies/"
	i := strings.LastIndex(name, marker)
	if i < 0 {
		return "", "", false
	}
	parent = name[:i]
	constraint = NormalizeOrgConstraint(name[i+len(marker):])
	if !validOrgPolicyParent(parent) || constraint == "" {
		return "", "", false
	}
	return parent, constraint, true
}
