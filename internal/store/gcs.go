package store

import (
	"crypto/md5"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bucket is Cloud Storage bucket metadata.
type Bucket struct {
	Name           string
	ProjectID      string
	Location       string
	StorageClass   string
	Labels         map[string]string
	Metageneration int64
	CreatedAt      string
	UpdatedAt      string
}

// ObjectMeta is Cloud Storage object metadata (one generation).
type ObjectMeta struct {
	Bucket             string
	Name               string
	Generation         int64
	Size               int64
	ContentType        string
	BlobPath           string
	MD5Hash            string
	CRC32C             string
	Metadata           map[string]string
	CacheControl       string
	ContentDisposition string
	ContentEncoding    string
	ContentLanguage    string
	Metageneration     int64
	CreatedAt          string
	UpdatedAt          string
}

// BucketIAMResource returns the lab IAM resource name for a bucket.
func BucketIAMResource(bucket string) string {
	return "buckets/" + bucket
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
		`INSERT OR IGNORE INTO buckets (name, project_id, location, storage_class, labels_json, metageneration, created_at, updated_at)
		 VALUES (?, ?, ?, ?, '{}', 1, ?, ?)`,
		name, projectID, location, storageClass, now, now,
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
		StorageClass: storageClass, Labels: map[string]string{}, Metageneration: 1,
		CreatedAt: now, UpdatedAt: now,
	}, true, nil
}

// GetBucket loads bucket metadata.
func (s *Store) GetBucket(name string) (*Bucket, bool, error) {
	var b Bucket
	var labelsJSON string
	err := s.db.QueryRow(
		`SELECT name, project_id, location, storage_class, COALESCE(labels_json, '{}'), COALESCE(metageneration, 1), created_at, COALESCE(updated_at, '')
		 FROM buckets WHERE name = ?`,
		name,
	).Scan(&b.Name, &b.ProjectID, &b.Location, &b.StorageClass, &labelsJSON, &b.Metageneration, &b.CreatedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	b.Labels = decodeStringMap(labelsJSON)
	if b.UpdatedAt == "" {
		b.UpdatedAt = b.CreatedAt
	}
	return &b, true, nil
}

// ListBuckets returns buckets for a project.
func (s *Store) ListBuckets(projectID string) ([]Bucket, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, storage_class, COALESCE(labels_json, '{}'), COALESCE(metageneration, 1), created_at, COALESCE(updated_at, '')
		 FROM buckets WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bucket
	for rows.Next() {
		var b Bucket
		var labelsJSON string
		if err := rows.Scan(&b.Name, &b.ProjectID, &b.Location, &b.StorageClass, &labelsJSON, &b.Metageneration, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		b.Labels = decodeStringMap(labelsJSON)
		if b.UpdatedAt == "" {
			b.UpdatedAt = b.CreatedAt
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// PatchBucket updates mutable bucket fields. Nil maps leave labels unchanged; empty map clears.
func (s *Store) PatchBucket(name string, location, storageClass *string, labels *map[string]string) (*Bucket, error) {
	b, ok, err := s.GetBucket(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}
	if location != nil && *location != "" {
		b.Location = *location
	}
	if storageClass != nil && *storageClass != "" {
		b.StorageClass = *storageClass
	}
	if labels != nil {
		b.Labels = *labels
		if b.Labels == nil {
			b.Labels = map[string]string{}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	b.Metageneration++
	b.UpdatedAt = now
	_, err = s.db.Exec(
		`UPDATE buckets SET location = ?, storage_class = ?, labels_json = ?, metageneration = ?, updated_at = ? WHERE name = ?`,
		b.Location, b.StorageClass, encodeStringMap(b.Labels), b.Metageneration, b.UpdatedAt, name,
	)
	if err != nil {
		return nil, err
	}
	return b, nil
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
	return s.PutObjectBytesMeta(bucket, name, contentType, data, nil)
}

// PutObjectBytesMeta writes object bytes with optional custom metadata.
func (s *Store) PutObjectBytesMeta(bucket, name, contentType string, data []byte, meta *ObjectMeta) (*ObjectMeta, error) {
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
	metadata := map[string]string{}
	cacheControl, contentDisposition, contentEncoding, contentLanguage := "", "", "", ""
	if meta != nil {
		if meta.Metadata != nil {
			metadata = meta.Metadata
		}
		cacheControl = meta.CacheControl
		contentDisposition = meta.ContentDisposition
		contentEncoding = meta.ContentEncoding
		contentLanguage = meta.ContentLanguage
		if meta.ContentType != "" {
			contentType = meta.ContentType
		}
	}
	_, err = s.db.Exec(
		`INSERT INTO objects (bucket, name, generation, size, content_type, blob_path, md5_hash, crc32c,
		  metadata_json, cache_control, content_disposition, content_encoding, content_language, metageneration, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		bucket, name, nextGen, int64(len(data)), contentType, rel, md5b64, crcB64,
		encodeStringMap(metadata), cacheControl, contentDisposition, contentEncoding, contentLanguage, now, now,
	)
	if err != nil {
		_ = os.Remove(abs)
		return nil, err
	}
	out := &ObjectMeta{
		Bucket: bucket, Name: name, Generation: nextGen, Size: int64(len(data)),
		ContentType: contentType, BlobPath: rel, MD5Hash: md5b64, CRC32C: crcB64,
		Metadata: metadata, CacheControl: cacheControl, ContentDisposition: contentDisposition,
		ContentEncoding: contentEncoding, ContentLanguage: contentLanguage, Metageneration: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	// Best-effort Eventarc delivery for GCS object finalize triggers.
	go s.DeliverEventarcForGCSFinalize(bucket, name, nextGen, int64(len(data)), contentType)
	return out, nil
}

func scanObject(rows interface {
	Scan(dest ...any) error
}) (*ObjectMeta, error) {
	var o ObjectMeta
	var metadataJSON string
	err := rows.Scan(
		&o.Bucket, &o.Name, &o.Generation, &o.Size, &o.ContentType, &o.BlobPath, &o.MD5Hash, &o.CRC32C,
		&metadataJSON, &o.CacheControl, &o.ContentDisposition, &o.ContentEncoding, &o.ContentLanguage,
		&o.Metageneration, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	o.Metadata = decodeStringMap(metadataJSON)
	return &o, nil
}

const objectSelectCols = `bucket, name, generation, size, content_type, blob_path, md5_hash, crc32c,
		 COALESCE(metadata_json, '{}'), COALESCE(cache_control, ''), COALESCE(content_disposition, ''),
		 COALESCE(content_encoding, ''), COALESCE(content_language, ''), COALESCE(metageneration, 1),
		 created_at, updated_at`

// GetObject returns metadata. generation 0 selects the latest generation.
func (s *Store) GetObject(bucket, name string, generation int64) (*ObjectMeta, bool, error) {
	var row *sql.Row
	if generation > 0 {
		row = s.db.QueryRow(
			`SELECT `+objectSelectCols+` FROM objects WHERE bucket = ? AND name = ? AND generation = ?`,
			bucket, name, generation,
		)
	} else {
		row = s.db.QueryRow(
			`SELECT `+objectSelectCols+` FROM objects WHERE bucket = ? AND name = ? ORDER BY generation DESC LIMIT 1`,
			bucket, name,
		)
	}
	o, err := scanObject(row)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return o, true, nil
}

// ListObjects returns the latest generation per object name, optional prefix filter.
func (s *Store) ListObjects(bucket, prefix string) ([]ObjectMeta, error) {
	result, err := s.ListObjectsDelimited(bucket, prefix, "")
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

// ObjectListResult is a listObjects response with optional common prefixes.
type ObjectListResult struct {
	Items    []ObjectMeta
	Prefixes []string
}

// ListObjectsDelimited lists latest-generation objects with optional prefix and delimiter.
// When delimiter is set (typically "/"), names that continue past the next delimiter after
// prefix are collapsed into Prefixes (directory theatre).
func (s *Store) ListObjectsDelimited(bucket, prefix, delimiter string) (*ObjectListResult, error) {
	rows, err := s.db.Query(
		`SELECT o.bucket, o.name, o.generation, o.size, o.content_type, o.blob_path, o.md5_hash, o.crc32c,
		 COALESCE(o.metadata_json, '{}'), COALESCE(o.cache_control, ''), COALESCE(o.content_disposition, ''),
		 COALESCE(o.content_encoding, ''), COALESCE(o.content_language, ''), COALESCE(o.metageneration, 1),
		 o.created_at, o.updated_at
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
	out := &ObjectListResult{}
	prefixSet := map[string]struct{}{}
	for rows.Next() {
		o, err := scanObject(rows)
		if err != nil {
			return nil, err
		}
		if prefix != "" && !strings.HasPrefix(o.Name, prefix) {
			continue
		}
		if delimiter == "" {
			out.Items = append(out.Items, *o)
			continue
		}
		rest := strings.TrimPrefix(o.Name, prefix)
		if i := strings.Index(rest, delimiter); i >= 0 {
			common := prefix + rest[:i+len(delimiter)]
			if _, ok := prefixSet[common]; !ok {
				prefixSet[common] = struct{}{}
				out.Prefixes = append(out.Prefixes, common)
			}
			continue
		}
		out.Items = append(out.Items, *o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ErrPreconditionFailed is returned when ifGenerationMatch does not match.
var ErrPreconditionFailed = fmt.Errorf("precondition failed")

// CheckGenerationMatch verifies ifGenerationMatch against the live object.
// wantGen < 0 means no check. wantGen == 0 requires the object not to exist.
func (s *Store) CheckGenerationMatch(bucket, name string, wantGen int64) error {
	if wantGen < 0 {
		return nil
	}
	o, ok, err := s.GetObject(bucket, name, 0)
	if err != nil {
		return err
	}
	if wantGen == 0 {
		if ok {
			return ErrPreconditionFailed
		}
		return nil
	}
	if !ok || o.Generation != wantGen {
		return ErrPreconditionFailed
	}
	return nil
}

// RewriteObject copies source to destination in one shot (lab: always done=true).
func (s *Store) RewriteObject(srcBucket, srcName string, srcGeneration int64, dstBucket, dstName string) (*ObjectMeta, error) {
	return s.CopyObject(srcBucket, srcName, srcGeneration, dstBucket, dstName)
}

// GCSUploadSession is a resumable upload session.
type GCSUploadSession struct {
	UploadID    string
	Bucket      string
	Name        string
	ContentType string
	CreatedAt   string
}

// CreateUploadSession starts a resumable upload session.
func (s *Store) CreateUploadSession(bucket, name, contentType string) (*GCSUploadSession, error) {
	if _, ok, err := s.GetBucket(bucket); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("bucket not found")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("object name required")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO gcs_upload_sessions (upload_id, bucket, name, content_type, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, bucket, name, contentType, now,
	)
	if err != nil {
		return nil, err
	}
	return &GCSUploadSession{UploadID: id, Bucket: bucket, Name: name, ContentType: contentType, CreatedAt: now}, nil
}

// GetUploadSession loads a resumable upload session.
func (s *Store) GetUploadSession(uploadID string) (*GCSUploadSession, bool, error) {
	var sess GCSUploadSession
	err := s.db.QueryRow(
		`SELECT upload_id, bucket, name, content_type, created_at FROM gcs_upload_sessions WHERE upload_id = ?`,
		uploadID,
	).Scan(&sess.UploadID, &sess.Bucket, &sess.Name, &sess.ContentType, &sess.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &sess, true, nil
}

// CompleteUploadSession writes object bytes and removes the session.
func (s *Store) CompleteUploadSession(uploadID string, data []byte) (*ObjectMeta, error) {
	sess, ok, err := s.GetUploadSession(uploadID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("upload session not found")
	}
	obj, err := s.PutObjectBytes(sess.Bucket, sess.Name, sess.ContentType, data)
	if err != nil {
		return nil, err
	}
	_, _ = s.db.Exec(`DELETE FROM gcs_upload_sessions WHERE upload_id = ?`, uploadID)
	return obj, nil
}

// DeleteUploadSession cancels a resumable upload.
func (s *Store) DeleteUploadSession(uploadID string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM gcs_upload_sessions WHERE upload_id = ?`, uploadID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// PatchObjectMetadata updates mutable metadata on the latest (or given) generation.
func (s *Store) PatchObjectMetadata(bucket, name string, generation int64, patch *ObjectMeta) (*ObjectMeta, error) {
	o, ok, err := s.GetObject(bucket, name, generation)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("object not found")
	}
	if patch == nil {
		return o, nil
	}
	if patch.ContentType != "" {
		o.ContentType = patch.ContentType
	}
	if patch.Metadata != nil {
		o.Metadata = patch.Metadata
	}
	if patch.CacheControl != "" || patch.ContentDisposition != "" || patch.ContentEncoding != "" || patch.ContentLanguage != "" {
		if patch.CacheControl != "" {
			o.CacheControl = patch.CacheControl
		}
		if patch.ContentDisposition != "" {
			o.ContentDisposition = patch.ContentDisposition
		}
		if patch.ContentEncoding != "" {
			o.ContentEncoding = patch.ContentEncoding
		}
		if patch.ContentLanguage != "" {
			o.ContentLanguage = patch.ContentLanguage
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	o.Metageneration++
	o.UpdatedAt = now
	_, err = s.db.Exec(
		`UPDATE objects SET content_type = ?, metadata_json = ?, cache_control = ?, content_disposition = ?,
		 content_encoding = ?, content_language = ?, metageneration = ?, updated_at = ?
		 WHERE bucket = ? AND name = ? AND generation = ?`,
		o.ContentType, encodeStringMap(o.Metadata), o.CacheControl, o.ContentDisposition,
		o.ContentEncoding, o.ContentLanguage, o.Metageneration, o.UpdatedAt,
		o.Bucket, o.Name, o.Generation,
	)
	if err != nil {
		return nil, err
	}
	return o, nil
}

// ComposeObject concatenates up to 32 source objects into destination (same bucket).
func (s *Store) ComposeObject(bucket, dest string, sources []string, contentType string) (*ObjectMeta, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("at least one source required")
	}
	if len(sources) > 32 {
		return nil, fmt.Errorf("compose supports at most 32 sources")
	}
	var parts [][]byte
	for _, src := range sources {
		o, ok, err := s.GetObject(bucket, src, 0)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("source object not found: %s", src)
		}
		data, err := s.ReadObjectBytes(o)
		if err != nil {
			return nil, err
		}
		parts = append(parts, data)
		if contentType == "" {
			contentType = o.ContentType
		}
	}
	var total int
	for _, p := range parts {
		total += len(p)
	}
	out := make([]byte, 0, total)
	for _, p := range parts {
		out = append(out, p...)
	}
	return s.PutObjectBytes(bucket, dest, contentType, out)
}

// CopyObject copies source object bytes to a destination bucket/name.
func (s *Store) CopyObject(srcBucket, srcName string, srcGeneration int64, dstBucket, dstName string) (*ObjectMeta, error) {
	src, ok, err := s.GetObject(srcBucket, srcName, srcGeneration)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("source object not found")
	}
	data, err := s.ReadObjectBytes(src)
	if err != nil {
		return nil, err
	}
	return s.PutObjectBytesMeta(dstBucket, dstName, src.ContentType, data, &ObjectMeta{
		ContentType:        src.ContentType,
		Metadata:           src.Metadata,
		CacheControl:       src.CacheControl,
		ContentDisposition: src.ContentDisposition,
		ContentEncoding:    src.ContentEncoding,
		ContentLanguage:    src.ContentLanguage,
	})
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

func encodeStringMap(m map[string]string) string {
	if m == nil {
		return "{}"
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func decodeStringMap(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" || raw == "{}" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = map[string]string{}
	}
	return out
}
