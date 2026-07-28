package store

import (
	"crypto/md5"
	"database/sql"
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Bucket is Cloud Storage bucket metadata.
type Bucket struct {
	Name         string
	ProjectID    string
	Location     string
	StorageClass string
	CreatedAt    string
}

// ObjectMeta is Cloud Storage object metadata (one generation).
type ObjectMeta struct {
	Bucket      string
	Name        string
	Generation  int64
	Size        int64
	ContentType string
	BlobPath    string
	MD5Hash     string
	CRC32C      string
	CreatedAt   string
	UpdatedAt   string
}

// CreateBucket inserts a bucket. Returns false when the name already exists.
func (s *Store) CreateBucket(name, projectID, location, storageClass string) (*Bucket, bool, error) {
	name = strings.TrimSpace(name)
	projectID = strings.TrimSpace(projectID)
	if name == "" || projectID == "" {
		return nil, false, fmt.Errorf("bucket name and project required")
	}
	if location == "" {
		location = "US"
	}
	if storageClass == "" {
		storageClass = "STANDARD"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO buckets (name, project_id, location, storage_class, created_at) VALUES (?, ?, ?, ?, ?)`,
		name, projectID, location, storageClass, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	dir := filepath.Join(s.dataRoot, "gcs", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, fmt.Errorf("create bucket dir: %w", err)
	}
	return &Bucket{
		Name: name, ProjectID: projectID, Location: location,
		StorageClass: storageClass, CreatedAt: now,
	}, true, nil
}

// GetBucket loads bucket metadata.
func (s *Store) GetBucket(name string) (*Bucket, bool, error) {
	var b Bucket
	err := s.db.QueryRow(
		`SELECT name, project_id, location, storage_class, created_at FROM buckets WHERE name = ?`,
		name,
	).Scan(&b.Name, &b.ProjectID, &b.Location, &b.StorageClass, &b.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &b, true, nil
}

// ListBuckets returns buckets for a project.
func (s *Store) ListBuckets(projectID string) ([]Bucket, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, storage_class, created_at FROM buckets WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Name, &b.ProjectID, &b.Location, &b.StorageClass, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeleteBucket removes a bucket when it has no objects.
func (s *Store) DeleteBucket(name string) (found bool, err error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM objects WHERE bucket = ?`, name).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, fmt.Errorf("bucket not empty")
	}
	res, err := s.db.Exec(`DELETE FROM buckets WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	_ = os.RemoveAll(filepath.Join(s.dataRoot, "gcs", name))
	return true, nil
}

// PutObjectBytes writes object bytes and metadata. generation 0 assigns next generation.
func (s *Store) PutObjectBytes(bucket, name, contentType string, data []byte) (*ObjectMeta, error) {
	if _, ok, err := s.GetBucket(bucket); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("bucket not found")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	var nextGen int64 = 1
	err := s.db.QueryRow(
		`SELECT COALESCE(MAX(generation), 0) + 1 FROM objects WHERE bucket = ? AND name = ?`,
		bucket, name,
	).Scan(&nextGen)
	if err != nil {
		return nil, err
	}
	rel := filepath.Join(bucket, fmt.Sprintf("%d", nextGen), sanitizeObjectPath(name))
	abs := filepath.Join(s.dataRoot, "gcs", rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, fmt.Errorf("create object dir: %w", err)
	}
	if err := os.WriteFile(abs, data, 0o600); err != nil {
		return nil, fmt.Errorf("write object: %w", err)
	}
	md5sum := md5.Sum(data)
	crc := crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli))
	md5b64 := base64.StdEncoding.EncodeToString(md5sum[:])
	crcBuf := []byte{byte(crc >> 24), byte(crc >> 16), byte(crc >> 8), byte(crc)}
	crcB64 := base64.StdEncoding.EncodeToString(crcBuf)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(
		`INSERT INTO objects (bucket, name, generation, size, content_type, blob_path, md5_hash, crc32c, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		bucket, name, nextGen, int64(len(data)), contentType, rel, md5b64, crcB64, now, now,
	)
	if err != nil {
		_ = os.Remove(abs)
		return nil, err
	}
	return &ObjectMeta{
		Bucket: bucket, Name: name, Generation: nextGen, Size: int64(len(data)),
		ContentType: contentType, BlobPath: rel, MD5Hash: md5b64, CRC32C: crcB64,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// GetObject returns metadata. generation 0 selects the latest generation.
func (s *Store) GetObject(bucket, name string, generation int64) (*ObjectMeta, bool, error) {
	var o ObjectMeta
	var err error
	if generation > 0 {
		err = s.db.QueryRow(
			`SELECT bucket, name, generation, size, content_type, blob_path, md5_hash, crc32c, created_at, updated_at
			 FROM objects WHERE bucket = ? AND name = ? AND generation = ?`,
			bucket, name, generation,
		).Scan(&o.Bucket, &o.Name, &o.Generation, &o.Size, &o.ContentType, &o.BlobPath, &o.MD5Hash, &o.CRC32C, &o.CreatedAt, &o.UpdatedAt)
	} else {
		err = s.db.QueryRow(
			`SELECT bucket, name, generation, size, content_type, blob_path, md5_hash, crc32c, created_at, updated_at
			 FROM objects WHERE bucket = ? AND name = ? ORDER BY generation DESC LIMIT 1`,
			bucket, name,
		).Scan(&o.Bucket, &o.Name, &o.Generation, &o.Size, &o.ContentType, &o.BlobPath, &o.MD5Hash, &o.CRC32C, &o.CreatedAt, &o.UpdatedAt)
	}
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &o, true, nil
}

// ListObjects returns the latest generation per object name, optional prefix filter.
func (s *Store) ListObjects(bucket, prefix string) ([]ObjectMeta, error) {
	rows, err := s.db.Query(
		`SELECT o.bucket, o.name, o.generation, o.size, o.content_type, o.blob_path, o.md5_hash, o.crc32c, o.created_at, o.updated_at
		 FROM objects o
		 INNER JOIN (
		   SELECT name, MAX(generation) AS generation FROM objects WHERE bucket = ? GROUP BY name
		 ) latest ON o.bucket = ? AND o.name = latest.name AND o.generation = latest.generation
		 ORDER BY o.name`,
		bucket, bucket,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ObjectMeta
	for rows.Next() {
		var o ObjectMeta
		if err := rows.Scan(&o.Bucket, &o.Name, &o.Generation, &o.Size, &o.ContentType, &o.BlobPath, &o.MD5Hash, &o.CRC32C, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		if prefix != "" && !strings.HasPrefix(o.Name, prefix) {
			continue
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// DeleteObject removes one generation (0 = latest).
func (s *Store) DeleteObject(bucket, name string, generation int64) (bool, error) {
	o, ok, err := s.GetObject(bucket, name, generation)
	if err != nil || !ok {
		return ok, err
	}
	res, err := s.db.Exec(`DELETE FROM objects WHERE bucket = ? AND name = ? AND generation = ?`, o.Bucket, o.Name, o.Generation)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	_ = os.Remove(filepath.Join(s.dataRoot, "gcs", o.BlobPath))
	return true, nil
}

// ReadObjectBytes loads object payload from disk.
func (s *Store) ReadObjectBytes(o *ObjectMeta) ([]byte, error) {
	if o == nil {
		return nil, fmt.Errorf("nil object")
	}
	return os.ReadFile(filepath.Join(s.dataRoot, "gcs", o.BlobPath))
}

func sanitizeObjectPath(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	parts := strings.Split(name, "/")
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			continue
		}
		clean = append(clean, p)
	}
	if len(clean) == 0 {
		return "_object"
	}
	return filepath.Join(clean...)
}
