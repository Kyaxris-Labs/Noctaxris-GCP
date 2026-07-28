package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// FirestoreDoc is a stored Firestore document row.
type FirestoreDoc struct {
	Path         string
	ProjectID    string
	CollectionID string
	DocumentID   string
	FieldsJSON   string
	CreateTime   string
	UpdateTime   string
}

// PutFirestoreDoc inserts or replaces a document.
func (s *Store) PutFirestoreDoc(doc FirestoreDoc) error {
	if doc.Path == "" || doc.ProjectID == "" || doc.CollectionID == "" || doc.DocumentID == "" {
		return fmt.Errorf("firestore doc requires path, project, collection, and document id")
	}
	if doc.FieldsJSON == "" {
		doc.FieldsJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if doc.CreateTime == "" {
		doc.CreateTime = now
	}
	if doc.UpdateTime == "" {
		doc.UpdateTime = now
	}
	_, err := s.db.Exec(
		`INSERT INTO firestore_docs (path, project_id, collection_id, document_id, fields_json, create_time, update_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
		   fields_json = excluded.fields_json,
		   update_time = excluded.update_time`,
		doc.Path, doc.ProjectID, doc.CollectionID, doc.DocumentID, doc.FieldsJSON, doc.CreateTime, doc.UpdateTime,
	)
	if err != nil {
		return fmt.Errorf("put firestore doc: %w", err)
	}
	return nil
}

// GetFirestoreDoc loads a document by full resource path.
func (s *Store) GetFirestoreDoc(path string) (FirestoreDoc, bool, error) {
	var d FirestoreDoc
	err := s.db.QueryRow(
		`SELECT path, project_id, collection_id, document_id, fields_json, create_time, update_time
		 FROM firestore_docs WHERE path = ?`, path,
	).Scan(&d.Path, &d.ProjectID, &d.CollectionID, &d.DocumentID, &d.FieldsJSON, &d.CreateTime, &d.UpdateTime)
	if err == sql.ErrNoRows {
		return FirestoreDoc{}, false, nil
	}
	if err != nil {
		return FirestoreDoc{}, false, fmt.Errorf("get firestore doc: %w", err)
	}
	return d, true, nil
}

// DeleteFirestoreDoc removes a document by path. ok is false when missing.
func (s *Store) DeleteFirestoreDoc(path string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM firestore_docs WHERE path = ?`, path)
	if err != nil {
		return false, fmt.Errorf("delete firestore doc: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListFirestoreDocs lists documents in a collection under a parent documents prefix.
// parentDocuments is projects/{p}/databases/(default)/documents or a nested document path.
// collectionID is the immediate child collection id.
func (s *Store) ListFirestoreDocs(projectID, parentDocuments, collectionID string, pageSize int) ([]FirestoreDoc, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	prefix := strings.TrimSuffix(parentDocuments, "/")
	// Direct children: {prefix}/{collectionID}/{docId} with no further slash after docId.
	like := prefix + "/" + collectionID + "/%"
	rows, err := s.db.Query(
		`SELECT path, project_id, collection_id, document_id, fields_json, create_time, update_time
		 FROM firestore_docs
		 WHERE project_id = ? AND collection_id = ? AND path LIKE ?
		 ORDER BY path
		 LIMIT ?`,
		projectID, collectionID, like, pageSize*4, // over-fetch then filter depth
	)
	if err != nil {
		return nil, fmt.Errorf("list firestore docs: %w", err)
	}
	defer rows.Close()

	wantSegments := strings.Count(prefix, "/") + 2 // collection + document under prefix
	out := make([]FirestoreDoc, 0, pageSize)
	for rows.Next() {
		var d FirestoreDoc
		if err := rows.Scan(&d.Path, &d.ProjectID, &d.CollectionID, &d.DocumentID, &d.FieldsJSON, &d.CreateTime, &d.UpdateTime); err != nil {
			return nil, err
		}
		if strings.Count(d.Path, "/") != wantSegments {
			continue
		}
		if !strings.HasPrefix(d.Path, prefix+"/"+collectionID+"/") {
			continue
		}
		out = append(out, d)
		if len(out) >= pageSize {
			break
		}
	}
	return out, rows.Err()
}

// ListFirestoreDocsByCollection lists all docs in a project collection (any parent depth).
func (s *Store) ListFirestoreDocsByCollection(projectID, collectionID string, limit int) ([]FirestoreDoc, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.Query(
		`SELECT path, project_id, collection_id, document_id, fields_json, create_time, update_time
		 FROM firestore_docs
		 WHERE project_id = ? AND collection_id = ?
		 ORDER BY path
		 LIMIT ?`,
		projectID, collectionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list firestore by collection: %w", err)
	}
	defer rows.Close()
	var out []FirestoreDoc
	for rows.Next() {
		var d FirestoreDoc
		if err := rows.Scan(&d.Path, &d.ProjectID, &d.CollectionID, &d.DocumentID, &d.FieldsJSON, &d.CreateTime, &d.UpdateTime); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// BatchGetFirestoreDocs loads documents by path; missing paths are omitted.
func (s *Store) BatchGetFirestoreDocs(paths []string) ([]FirestoreDoc, error) {
	out := make([]FirestoreDoc, 0, len(paths))
	for _, p := range paths {
		d, ok, err := s.GetFirestoreDoc(p)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, d)
		}
	}
	return out, nil
}

// PutFirestoreTransaction registers a lab transaction token (no isolation).
func (s *Store) PutFirestoreTransaction(token, database, projectID string) error {
	if token == "" || database == "" || projectID == "" {
		return fmt.Errorf("firestore transaction requires token, database, and project")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO firestore_transactions (token, database, project_id, created_at) VALUES (?, ?, ?, ?)`,
		token, database, projectID, now,
	)
	if err != nil {
		return fmt.Errorf("put firestore transaction: %w", err)
	}
	return nil
}

// ConsumeFirestoreTransaction deletes and validates a token for database. ok is false when missing/mismatch.
func (s *Store) ConsumeFirestoreTransaction(token, database string) (bool, error) {
	if token == "" {
		return false, nil
	}
	res, err := s.db.Exec(
		`DELETE FROM firestore_transactions WHERE token = ? AND database = ?`,
		token, database,
	)
	if err != nil {
		return false, fmt.Errorf("consume firestore transaction: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteFirestoreTransaction removes a token regardless of database. ok is false when missing.
func (s *Store) DeleteFirestoreTransaction(token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	res, err := s.db.Exec(`DELETE FROM firestore_transactions WHERE token = ?`, token)
	if err != nil {
		return false, fmt.Errorf("delete firestore transaction: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
