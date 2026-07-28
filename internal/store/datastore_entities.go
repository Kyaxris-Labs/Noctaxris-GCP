package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// --- Datastore ---

// DatastoreEntity is a Datastore entity row (distinct from Firestore).
type DatastoreEntity struct {
	ProjectID      string
	Namespace      string
	Kind           string
	KeyPath        string
	KeyID          int64
	KeyName        string
	PropertiesJSON string
	UpdatedAt      string
}

// PutDatastoreEntity upserts an entity.
func (s *Store) PutDatastoreEntity(e DatastoreEntity) error {
	e.ProjectID = strings.TrimSpace(e.ProjectID)
	e.Kind = strings.TrimSpace(e.Kind)
	e.KeyPath = strings.TrimSpace(e.KeyPath)
	if e.ProjectID == "" || e.Kind == "" || e.KeyPath == "" {
		return fmt.Errorf("project, kind, and key_path required")
	}
	if e.PropertiesJSON == "" {
		e.PropertiesJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO datastore_entities (project_id, namespace, kind, key_path, key_id, key_name, properties_json, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(project_id, namespace, key_path) DO UPDATE SET
		   kind = excluded.kind,
		   key_id = excluded.key_id,
		   key_name = excluded.key_name,
		   properties_json = excluded.properties_json,
		   updated_at = excluded.updated_at`,
		e.ProjectID, e.Namespace, e.Kind, e.KeyPath, e.KeyID, e.KeyName, e.PropertiesJSON, now,
	)
	return err
}

// GetDatastoreEntity loads by project/namespace/key_path.
func (s *Store) GetDatastoreEntity(projectID, namespace, keyPath string) (*DatastoreEntity, bool, error) {
	var e DatastoreEntity
	err := s.db.QueryRow(
		`SELECT project_id, namespace, kind, key_path, key_id, key_name, properties_json, updated_at
		 FROM datastore_entities WHERE project_id = ? AND namespace = ? AND key_path = ?`,
		projectID, namespace, keyPath,
	).Scan(&e.ProjectID, &e.Namespace, &e.Kind, &e.KeyPath, &e.KeyID, &e.KeyName, &e.PropertiesJSON, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &e, true, nil
}

// DeleteDatastoreEntity removes an entity.
func (s *Store) DeleteDatastoreEntity(projectID, namespace, keyPath string) (bool, error) {
	res, err := s.db.Exec(
		`DELETE FROM datastore_entities WHERE project_id = ? AND namespace = ? AND key_path = ?`,
		projectID, namespace, keyPath,
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

// QueryDatastoreEntitiesFilter is equality-only RunQuery support.
type QueryDatastoreEntitiesFilter struct {
	ProjectID string
	Namespace string
	Kind      string
	// PropEquals maps property name -> JSON-encoded scalar for equality.
	PropEquals map[string]string
	Limit      int
}

// QueryDatastoreEntities returns entities matching kind + equality filters.
func (s *Store) QueryDatastoreEntities(f QueryDatastoreEntitiesFilter) ([]DatastoreEntity, error) {
	q := `SELECT project_id, namespace, kind, key_path, key_id, key_name, properties_json, updated_at
	      FROM datastore_entities WHERE project_id = ? AND namespace = ? AND kind = ?`
	args := []any{f.ProjectID, f.Namespace, f.Kind}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DatastoreEntity
	for rows.Next() {
		var e DatastoreEntity
		if err := rows.Scan(&e.ProjectID, &e.Namespace, &e.Kind, &e.KeyPath, &e.KeyID, &e.KeyName, &e.PropertiesJSON, &e.UpdatedAt); err != nil {
			return nil, err
		}
		if len(f.PropEquals) > 0 {
			var props map[string]any
			if err := json.Unmarshal([]byte(e.PropertiesJSON), &props); err != nil {
				continue
			}
			match := true
			for k, wantRaw := range f.PropEquals {
				got, ok := props[k]
				if !ok {
					match = false
					break
				}
				gotRaw, _ := json.Marshal(got)
				if string(gotRaw) != wantRaw && fmt.Sprint(got) != strings.Trim(wantRaw, `"`) {
					// also compare unquoted string
					var want any
					_ = json.Unmarshal([]byte(wantRaw), &want)
					if fmt.Sprint(got) != fmt.Sprint(want) {
						match = false
						break
					}
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, e)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, rows.Err()
}

// NextDatastoreID allocates a numeric id (lab counter via max+1).
func (s *Store) NextDatastoreID(projectID, namespace, kind string) (int64, error) {
	var max sql.NullInt64
	err := s.db.QueryRow(
		`SELECT MAX(key_id) FROM datastore_entities WHERE project_id = ? AND namespace = ? AND kind = ?`,
		projectID, namespace, kind,
	).Scan(&max)
	if err != nil {
		return 0, err
	}
	if !max.Valid || max.Int64 < 1 {
		return 1, nil
	}
	return max.Int64 + 1, nil
}

// PutDatastoreTransaction registers a lab transaction token (no isolation).
func (s *Store) PutDatastoreTransaction(token, projectID, databaseID string) error {
	if token == "" || projectID == "" {
		return fmt.Errorf("datastore transaction requires token and project")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO datastore_transactions (token, project_id, database_id, created_at) VALUES (?, ?, ?, ?)`,
		token, projectID, databaseID, now,
	)
	if err != nil {
		return fmt.Errorf("put datastore transaction: %w", err)
	}
	return nil
}

// ConsumeDatastoreTransaction deletes and validates a token for project. ok is false when missing.
func (s *Store) ConsumeDatastoreTransaction(token, projectID string) (bool, error) {
	if token == "" {
		return false, nil
	}
	res, err := s.db.Exec(
		`DELETE FROM datastore_transactions WHERE token = ? AND project_id = ?`,
		token, projectID,
	)
	if err != nil {
		return false, fmt.Errorf("consume datastore transaction: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteDatastoreTransaction removes a token. ok is false when missing.
func (s *Store) DeleteDatastoreTransaction(token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	res, err := s.db.Exec(`DELETE FROM datastore_transactions WHERE token = ?`, token)
	if err != nil {
		return false, fmt.Errorf("delete datastore transaction: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
