package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ArRegistryManifest is a Docker Registry V2 manifest row keyed by image name + tag or digest.
type ArRegistryManifest struct {
	Name      string
	Reference string
	Digest    string
	MediaType string
	Data      []byte
	CreatedAt string
	UpdatedAt string
}

// ArRegistryBlobLink is a digest uploaded under a registry image name.
type ArRegistryBlobLink struct {
	Digest    string
	ImageName string
	SizeBytes int64
	CreatedAt string
}

// PutArRegistryBlob stores blob bytes by digest (content-addressed).
func (s *Store) PutArRegistryBlob(digest string, data []byte) error {
	if digest == "" {
		return fmt.Errorf("ar registry blob digest required")
	}
	if data == nil {
		data = []byte{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO ar_registry_blobs (digest, data, size_bytes, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(digest) DO UPDATE SET data = excluded.data, size_bytes = excluded.size_bytes`,
		digest, data, int64(len(data)), now,
	)
	if err != nil {
		return fmt.Errorf("put ar registry blob: %w", err)
	}
	return nil
}

// GetArRegistryBlob returns blob bytes by digest.
func (s *Store) GetArRegistryBlob(digest string) ([]byte, bool, error) {
	var data []byte
	err := s.db.QueryRow(`SELECT data FROM ar_registry_blobs WHERE digest = ?`, digest).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get ar registry blob: %w", err)
	}
	return data, true, nil
}

// ArRegistryBlobSize returns the stored size for digest.
func (s *Store) ArRegistryBlobSize(digest string) (int64, bool, error) {
	var n int64
	err := s.db.QueryRow(`SELECT size_bytes FROM ar_registry_blobs WHERE digest = ?`, digest).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("ar registry blob size: %w", err)
	}
	return n, true, nil
}

// LinkArRegistryBlob records that digest was uploaded under imageName (project/repo/image).
func (s *Store) LinkArRegistryBlob(digest, imageName string) error {
	if digest == "" || imageName == "" {
		return fmt.Errorf("ar registry blob link requires digest and image name")
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO ar_registry_blob_links (digest, image_name) VALUES (?, ?)`,
		digest, imageName,
	)
	if err != nil {
		return fmt.Errorf("link ar registry blob: %w", err)
	}
	return nil
}

// ListArRegistryBlobLinksForRepo returns blobs whose image name maps to project/repo.
func (s *Store) ListArRegistryBlobLinksForRepo(projectID, repoID string) ([]ArRegistryBlobLink, error) {
	rows, err := s.db.Query(
		`SELECT l.digest, l.image_name, b.size_bytes, b.created_at
		 FROM ar_registry_blob_links l
		 JOIN ar_registry_blobs b ON b.digest = l.digest
		 ORDER BY l.image_name, l.digest`,
	)
	if err != nil {
		return nil, fmt.Errorf("list ar registry blob links: %w", err)
	}
	defer rows.Close()
	var out []ArRegistryBlobLink
	for rows.Next() {
		var row ArRegistryBlobLink
		if err := rows.Scan(&row.Digest, &row.ImageName, &row.SizeBytes, &row.CreatedAt); err != nil {
			return nil, err
		}
		if arImageInRepo(row.ImageName, projectID, repoID) {
			out = append(out, row)
		}
	}
	return out, rows.Err()
}

func arImageInRepo(imageName, projectID, repoID string) bool {
	if imageName == "" || repoID == "" {
		return false
	}
	if projectID != "" {
		pref := projectID + "/" + repoID
		if imageName == pref || strings.HasPrefix(imageName, pref+"/") {
			return true
		}
	}
	return imageName == repoID || strings.HasPrefix(imageName, repoID+"/")
}

// PutArRegistryManifest stores a manifest for name+reference.
func (s *Store) PutArRegistryManifest(m ArRegistryManifest) error {
	if m.Name == "" || m.Reference == "" || m.Digest == "" {
		return fmt.Errorf("ar registry manifest requires name, reference, and digest")
	}
	if m.MediaType == "" {
		m.MediaType = "application/vnd.docker.distribution.manifest.v2+json"
	}
	if m.Data == nil {
		m.Data = []byte{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if m.CreatedAt == "" {
		m.CreatedAt = now
	}
	if m.UpdatedAt == "" {
		m.UpdatedAt = now
	}
	_, err := s.db.Exec(
		`INSERT INTO ar_registry_manifests (name, reference, digest, media_type, data, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name, reference) DO UPDATE SET
		   digest = excluded.digest,
		   media_type = excluded.media_type,
		   data = excluded.data,
		   updated_at = excluded.updated_at`,
		m.Name, m.Reference, m.Digest, m.MediaType, m.Data, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("put ar registry manifest: %w", err)
	}
	return nil
}

// GetArRegistryManifest loads by name+reference, or by digest when reference is a digest.
func (s *Store) GetArRegistryManifest(name, reference string) (ArRegistryManifest, bool, error) {
	var m ArRegistryManifest
	err := s.db.QueryRow(
		`SELECT name, reference, digest, media_type, data, created_at, updated_at
		 FROM ar_registry_manifests WHERE name = ? AND reference = ?`,
		name, reference,
	).Scan(&m.Name, &m.Reference, &m.Digest, &m.MediaType, &m.Data, &m.CreatedAt, &m.UpdatedAt)
	if err == nil {
		return m, true, nil
	}
	if err != sql.ErrNoRows {
		return ArRegistryManifest{}, false, fmt.Errorf("get ar registry manifest: %w", err)
	}
	if !strings.HasPrefix(strings.ToLower(reference), "sha256:") {
		return ArRegistryManifest{}, false, nil
	}
	err = s.db.QueryRow(
		`SELECT name, reference, digest, media_type, data, created_at, updated_at
		 FROM ar_registry_manifests WHERE name = ? AND digest = ? LIMIT 1`,
		name, reference,
	).Scan(&m.Name, &m.Reference, &m.Digest, &m.MediaType, &m.Data, &m.CreatedAt, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return ArRegistryManifest{}, false, nil
	}
	if err != nil {
		return ArRegistryManifest{}, false, fmt.Errorf("get ar registry manifest by digest: %w", err)
	}
	return m, true, nil
}

// UpsertArPackage inserts or updates package metadata.
func (s *Store) UpsertArPackage(pkg ArPackage) error {
	if pkg.Name == "" || pkg.RepositoryName == "" || pkg.PackageID == "" {
		return fmt.Errorf("ar package requires name, repository, and package id")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if pkg.CreatedAt == "" {
		pkg.CreatedAt = now
	}
	if pkg.UpdatedAt == "" {
		pkg.UpdatedAt = now
	}
	if pkg.DisplayName == "" {
		pkg.DisplayName = pkg.PackageID
	}
	_, err := s.db.Exec(
		`INSERT INTO ar_packages (name, repository_name, package_id, display_name, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET display_name = excluded.display_name, updated_at = excluded.updated_at`,
		pkg.Name, pkg.RepositoryName, pkg.PackageID, pkg.DisplayName, pkg.CreatedAt, pkg.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert ar package: %w", err)
	}
	return nil
}

// UpsertArVersion inserts or updates version metadata.
func (s *Store) UpsertArVersion(v ArVersion) error {
	if v.Name == "" || v.PackageName == "" || v.VersionID == "" {
		return fmt.Errorf("ar version requires name, package, and version id")
	}
	if v.RelatedTagsJSON == "" {
		v.RelatedTagsJSON = "[]"
	}
	if v.MetadataJSON == "" {
		v.MetadataJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if v.CreatedAt == "" {
		v.CreatedAt = now
	}
	if v.UpdatedAt == "" {
		v.UpdatedAt = now
	}
	_, err := s.db.Exec(
		`INSERT INTO ar_versions
		 (name, package_name, version_id, description, related_tags_json, metadata_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   description = excluded.description,
		   related_tags_json = excluded.related_tags_json,
		   metadata_json = excluded.metadata_json,
		   updated_at = excluded.updated_at`,
		v.Name, v.PackageName, v.VersionID, v.Description, v.RelatedTagsJSON, v.MetadataJSON, v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert ar version: %w", err)
	}
	_, _ = s.db.Exec(`UPDATE ar_packages SET updated_at = ? WHERE name = ?`, v.UpdatedAt, v.PackageName)
	return nil
}
