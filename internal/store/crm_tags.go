package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// TagKey is a Cloud Resource Manager TagKey row.
type TagKey struct {
	Name           string
	Parent         string
	ShortName      string
	NamespacedName string
	Description    string
	Etag           string
	CreatedAt      string
	UpdatedAt      string
}

// TagBinding is a TagBinding row (lite; stores namespaced tag value).
type TagBinding struct {
	Name                   string
	Parent                 string
	TagValue               string
	TagValueNamespacedName string
	CreatedAt              string
}

const crmTagsSchema = `
CREATE TABLE IF NOT EXISTS crm_tag_keys (
  name TEXT PRIMARY KEY,
  parent TEXT NOT NULL,
  short_name TEXT NOT NULL,
  namespaced_name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  etag TEXT NOT NULL DEFAULT 'ACAB',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (parent, short_name)
);

CREATE TABLE IF NOT EXISTS crm_tag_bindings (
  name TEXT PRIMARY KEY,
  parent TEXT NOT NULL,
  tag_value TEXT NOT NULL,
  tag_value_namespaced_name TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_crm_tag_keys_parent ON crm_tag_keys (parent);
CREATE INDEX IF NOT EXISTS idx_crm_tag_bindings_parent ON crm_tag_bindings (parent);
`

func (s *Store) migrateCRMTags() error {
	if _, err := s.db.Exec(crmTagsSchema); err != nil {
		return fmt.Errorf("migrate crm tags: %w", err)
	}
	return nil
}

// CreateTagKey inserts a TagKey under parent (organizations/... or projects/...).
func (s *Store) CreateTagKey(parent, shortName, description string) (TagKey, error) {
	parent = strings.TrimSpace(parent)
	shortName = strings.TrimSpace(shortName)
	if parent == "" || shortName == "" {
		return TagKey{}, fmt.Errorf("parent and shortName required")
	}
	if !strings.HasPrefix(parent, "organizations/") && !strings.HasPrefix(parent, "projects/") {
		return TagKey{}, fmt.Errorf("parent must be organizations/{org} or projects/{project}")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := NewLabID()
	name := "tagKeys/" + id
	nsParent := strings.TrimPrefix(strings.TrimPrefix(parent, "organizations/"), "projects/")
	namespaced := nsParent + "/" + shortName
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO crm_tag_keys
		 (name, parent, short_name, namespaced_name, description, etag, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'ACAB', ?, ?)`,
		name, parent, shortName, namespaced, description, now, now,
	)
	if err != nil {
		return TagKey{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return TagKey{}, err
	}
	if n == 0 {
		return TagKey{}, ErrAlreadyExists
	}
	return TagKey{
		Name: name, Parent: parent, ShortName: shortName, NamespacedName: namespaced,
		Description: description, Etag: "ACAB", CreatedAt: now, UpdatedAt: now,
	}, nil
}

// GetTagKey loads a TagKey by name (tagKeys/{id}).
func (s *Store) GetTagKey(nameOrID string) (TagKey, bool, error) {
	nameOrID = strings.TrimSpace(nameOrID)
	if !strings.HasPrefix(nameOrID, "tagKeys/") {
		nameOrID = "tagKeys/" + nameOrID
	}
	var k TagKey
	err := s.db.QueryRow(
		`SELECT name, parent, short_name, namespaced_name, description, etag, created_at, updated_at
		 FROM crm_tag_keys WHERE name = ?`, nameOrID,
	).Scan(&k.Name, &k.Parent, &k.ShortName, &k.NamespacedName, &k.Description, &k.Etag, &k.CreatedAt, &k.UpdatedAt)
	if err == sql.ErrNoRows {
		return TagKey{}, false, nil
	}
	if err != nil {
		return TagKey{}, false, err
	}
	return k, true, nil
}

// ListTagKeys lists TagKeys for parent.
func (s *Store) ListTagKeys(parent string) ([]TagKey, error) {
	rows, err := s.db.Query(
		`SELECT name, parent, short_name, namespaced_name, description, etag, created_at, updated_at
		 FROM crm_tag_keys WHERE parent = ? ORDER BY short_name`, parent,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagKey
	for rows.Next() {
		var k TagKey
		if err := rows.Scan(&k.Name, &k.Parent, &k.ShortName, &k.NamespacedName, &k.Description, &k.Etag, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteTagKey removes a TagKey.
func (s *Store) DeleteTagKey(nameOrID string) (bool, error) {
	nameOrID = strings.TrimSpace(nameOrID)
	if !strings.HasPrefix(nameOrID, "tagKeys/") {
		nameOrID = "tagKeys/" + nameOrID
	}
	res, err := s.db.Exec(`DELETE FROM crm_tag_keys WHERE name = ?`, nameOrID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// CreateTagBinding attaches a namespaced tag value to parent.
// tagValueNamespacedName is like "org/env/prod"; lab allocates tagValues/{id}.
func (s *Store) CreateTagBinding(parent, tagValueNamespacedName string) (TagBinding, error) {
	parent = strings.TrimSpace(parent)
	tagValueNamespacedName = strings.TrimSpace(tagValueNamespacedName)
	if parent == "" || tagValueNamespacedName == "" {
		return TagBinding{}, fmt.Errorf("parent and tagValueNamespacedName required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := NewLabID()
	name := "tagBindings/" + id
	tagValue := "tagValues/" + id
	_, err := s.db.Exec(
		`INSERT INTO crm_tag_bindings
		 (name, parent, tag_value, tag_value_namespaced_name, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		name, parent, tagValue, tagValueNamespacedName, now,
	)
	if err != nil {
		return TagBinding{}, err
	}
	return TagBinding{
		Name: name, Parent: parent, TagValue: tagValue,
		TagValueNamespacedName: tagValueNamespacedName, CreatedAt: now,
	}, nil
}

// GetTagBinding loads a binding by name.
func (s *Store) GetTagBinding(nameOrID string) (TagBinding, bool, error) {
	nameOrID = strings.TrimSpace(nameOrID)
	if !strings.HasPrefix(nameOrID, "tagBindings/") {
		nameOrID = "tagBindings/" + nameOrID
	}
	var b TagBinding
	err := s.db.QueryRow(
		`SELECT name, parent, tag_value, tag_value_namespaced_name, created_at
		 FROM crm_tag_bindings WHERE name = ?`, nameOrID,
	).Scan(&b.Name, &b.Parent, &b.TagValue, &b.TagValueNamespacedName, &b.CreatedAt)
	if err == sql.ErrNoRows {
		return TagBinding{}, false, nil
	}
	if err != nil {
		return TagBinding{}, false, err
	}
	return b, true, nil
}

// ListTagBindings lists bindings for parent.
func (s *Store) ListTagBindings(parent string) ([]TagBinding, error) {
	rows, err := s.db.Query(
		`SELECT name, parent, tag_value, tag_value_namespaced_name, created_at
		 FROM crm_tag_bindings WHERE parent = ? ORDER BY created_at`, parent,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagBinding
	for rows.Next() {
		var b TagBinding
		if err := rows.Scan(&b.Name, &b.Parent, &b.TagValue, &b.TagValueNamespacedName, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeleteTagBinding removes a binding.
func (s *Store) DeleteTagBinding(nameOrID string) (bool, error) {
	nameOrID = strings.TrimSpace(nameOrID)
	if !strings.HasPrefix(nameOrID, "tagBindings/") {
		nameOrID = "tagBindings/" + nameOrID
	}
	res, err := s.db.Exec(`DELETE FROM crm_tag_bindings WHERE name = ?`, nameOrID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
