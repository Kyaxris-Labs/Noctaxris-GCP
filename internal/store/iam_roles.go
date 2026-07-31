package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CustomRole is a project-level IAM custom role (IAM Admin roles.* lite).
type CustomRole struct {
	Name                string
	ProjectID           string
	RoleID              string
	Title               string
	Description         string
	IncludedPermissions []string
	Stage               string
	Etag                string
	Deleted             bool
	CreatedAt           string
	UpdatedAt           string
}

const iamRolesSchema = `
CREATE TABLE IF NOT EXISTS iam_custom_roles (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  role_id TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  included_permissions_json TEXT NOT NULL DEFAULT '[]',
  stage TEXT NOT NULL DEFAULT 'GA',
  etag TEXT NOT NULL DEFAULT 'ACAB',
  deleted INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (project_id, role_id)
);

CREATE INDEX IF NOT EXISTS idx_iam_custom_roles_project ON iam_custom_roles (project_id);
`

func (s *Store) migrateIAMRoles() error {
	if _, err := s.db.Exec(iamRolesSchema); err != nil {
		return fmt.Errorf("migrate iam custom roles: %w", err)
	}
	return nil
}

// CreateCustomRole inserts a project custom role.
func (s *Store) CreateCustomRole(projectID, roleID, title, description, stage string, includedPermissions []string) (CustomRole, error) {
	projectID = strings.TrimSpace(projectID)
	roleID = strings.TrimSpace(roleID)
	if projectID == "" || roleID == "" {
		return CustomRole{}, fmt.Errorf("project and roleId required")
	}
	if !validCustomRoleID(roleID) {
		return CustomRole{}, fmt.Errorf("invalid roleId")
	}
	if stage == "" {
		stage = "GA"
	}
	if includedPermissions == nil {
		includedPermissions = []string{}
	}
	permsJSON, err := json.Marshal(includedPermissions)
	if err != nil {
		return CustomRole{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	name := fmt.Sprintf("projects/%s/roles/%s", projectID, roleID)
	etag := "ACAB"
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO iam_custom_roles
		 (name, project_id, role_id, title, description, included_permissions_json, stage, etag, deleted, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		name, projectID, roleID, title, description, string(permsJSON), stage, etag, now, now,
	)
	if err != nil {
		return CustomRole{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return CustomRole{}, err
	}
	if n == 0 {
		return CustomRole{}, ErrAlreadyExists
	}
	return CustomRole{
		Name: name, ProjectID: projectID, RoleID: roleID,
		Title: title, Description: description,
		IncludedPermissions: append([]string(nil), includedPermissions...),
		Stage:               stage, Etag: etag, Deleted: false,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// GetCustomRole loads a custom role by resource name.
// Soft-deleted roles are returned with Deleted=true (GET does not 404); use ListCustomRoles(showDeleted) to enumerate them.
func (s *Store) GetCustomRole(name string) (CustomRole, bool, error) {
	var r CustomRole
	var permsJSON string
	var deleted int
	err := s.db.QueryRow(
		`SELECT name, project_id, role_id, title, description, included_permissions_json, stage, etag, deleted, created_at, updated_at
		 FROM iam_custom_roles WHERE name = ?`, name,
	).Scan(&r.Name, &r.ProjectID, &r.RoleID, &r.Title, &r.Description, &permsJSON, &r.Stage, &r.Etag, &deleted, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return CustomRole{}, false, nil
	}
	if err != nil {
		return CustomRole{}, false, err
	}
	r.Deleted = deleted != 0
	r.IncludedPermissions, err = decodePermissionList(permsJSON)
	if err != nil {
		return CustomRole{}, false, err
	}
	return r, true, nil
}

// GetRoleIncludedPermissions implements authz.RoleStore for active (non-deleted) custom roles.
func (s *Store) GetRoleIncludedPermissions(roleName string) ([]string, bool, error) {
	r, ok, err := s.GetCustomRole(roleName)
	if err != nil || !ok || r.Deleted {
		return nil, false, err
	}
	return append([]string(nil), r.IncludedPermissions...), true, nil
}

// ListCustomRoles returns non-deleted custom roles for a project.
func (s *Store) ListCustomRoles(projectID string, showDeleted bool) ([]CustomRole, error) {
	projectID = strings.TrimSpace(projectID)
	q := `SELECT name, project_id, role_id, title, description, included_permissions_json, stage, etag, deleted, created_at, updated_at
	      FROM iam_custom_roles WHERE project_id = ?`
	if !showDeleted {
		q += ` AND deleted = 0`
	}
	q += ` ORDER BY role_id`
	rows, err := s.db.Query(q, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomRole
	for rows.Next() {
		var r CustomRole
		var permsJSON string
		var deleted int
		if err := rows.Scan(&r.Name, &r.ProjectID, &r.RoleID, &r.Title, &r.Description, &permsJSON, &r.Stage, &r.Etag, &deleted, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Deleted = deleted != 0
		r.IncludedPermissions, err = decodePermissionList(permsJSON)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateCustomRole patches title/description/stage/includedPermissions for an active custom role.
func (s *Store) UpdateCustomRole(name, title, description, stage string, includedPermissions []string, updateTitle, updateDescription, updateStage, updatePerms bool) (CustomRole, bool, error) {
	r, ok, err := s.GetCustomRole(name)
	if err != nil || !ok || r.Deleted {
		return CustomRole{}, false, err
	}
	if updateTitle {
		r.Title = title
	}
	if updateDescription {
		r.Description = description
	}
	if updateStage {
		if stage == "" {
			stage = "GA"
		}
		r.Stage = stage
	}
	if updatePerms {
		if includedPermissions == nil {
			includedPermissions = []string{}
		}
		r.IncludedPermissions = append([]string(nil), includedPermissions...)
	}
	permsJSON, err := json.Marshal(r.IncludedPermissions)
	if err != nil {
		return CustomRole{}, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r.Etag = "ACAB"
	r.UpdatedAt = now
	res, err := s.db.Exec(
		`UPDATE iam_custom_roles SET title = ?, description = ?, included_permissions_json = ?, stage = ?, etag = ?, updated_at = ?
		 WHERE name = ? AND deleted = 0`,
		r.Title, r.Description, string(permsJSON), r.Stage, r.Etag, now, name,
	)
	if err != nil {
		return CustomRole{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return CustomRole{}, false, err
	}
	if n == 0 {
		return CustomRole{}, false, nil
	}
	return r, true, nil
}

// DeleteCustomRole soft-deletes a custom role (sets deleted=1).
func (s *Store) DeleteCustomRole(name string) (CustomRole, bool, error) {
	r, ok, err := s.GetCustomRole(name)
	if err != nil || !ok || r.Deleted {
		return CustomRole{}, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE iam_custom_roles SET deleted = 1, updated_at = ? WHERE name = ? AND deleted = 0`,
		now, name,
	)
	if err != nil {
		return CustomRole{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return CustomRole{}, false, err
	}
	if n == 0 {
		return CustomRole{}, false, nil
	}
	r.Deleted = true
	r.UpdatedAt = now
	return r, true, nil
}

// UndeleteCustomRole clears soft-delete for a custom role.
func (s *Store) UndeleteCustomRole(name string) (CustomRole, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE iam_custom_roles SET deleted = 0, updated_at = ? WHERE name = ? AND deleted = 1`,
		now, name,
	)
	if err != nil {
		return CustomRole{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return CustomRole{}, false, err
	}
	if n == 0 {
		return CustomRole{}, false, nil
	}
	return s.GetCustomRole(name)
}

func decodePermissionList(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}
	var perms []string
	if err := json.Unmarshal([]byte(raw), &perms); err != nil {
		return nil, fmt.Errorf("decode includedPermissions: %w", err)
	}
	if perms == nil {
		perms = []string{}
	}
	return perms, nil
}

// validCustomRoleID matches Google IAM roleId: 3-64 chars, [a-zA-Z0-9_.].
func validCustomRoleID(id string) bool {
	if len(id) < 3 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}
