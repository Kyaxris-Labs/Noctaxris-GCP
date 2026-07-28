package store

import (
	"database/sql"
	"fmt"
	"time"
)

const (
	KMSStateEnabled   = "ENABLED"
	KMSStateDestroyed = "DESTROYED"
	KMSPurposeEncrypt = "ENCRYPT_DECRYPT"
	KMSAlgoSymmetric  = "GOOGLE_SYMMETRIC_ENCRYPTION"
)

// KMSKeyRing is a stored Cloud KMS key ring.
type KMSKeyRing struct {
	Name      string
	ProjectID string
	Location  string
	CreatedAt string
}

// KMSCryptoKey is a stored crypto key (metadata only).
type KMSCryptoKey struct {
	Name      string
	KeyRing   string
	Purpose   string
	Algorithm string
	CreatedAt string
}

// KMSKeyVersion holds sealed key material and lifecycle state.
type KMSKeyVersion struct {
	Name                   string
	CryptoKey              string
	VersionID              string
	State                  string
	KeyMaterialCiphertext  []byte
	CreatedAt              string
}

// CreateKMSKeyRing inserts a key ring. Returns false when the name already exists.
func (s *Store) CreateKMSKeyRing(kr KMSKeyRing) (created bool, err error) {
	if kr.Name == "" || kr.ProjectID == "" || kr.Location == "" {
		return false, fmt.Errorf("kms key ring requires name, project, and location")
	}
	if kr.CreatedAt == "" {
		kr.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO kms_keyrings (name, project_id, location, created_at) VALUES (?, ?, ?, ?)`,
		kr.Name, kr.ProjectID, kr.Location, kr.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create kms key ring: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetKMSKeyRing loads a key ring by name.
func (s *Store) GetKMSKeyRing(name string) (KMSKeyRing, bool, error) {
	var kr KMSKeyRing
	err := s.db.QueryRow(
		`SELECT name, project_id, location, created_at FROM kms_keyrings WHERE name = ?`, name,
	).Scan(&kr.Name, &kr.ProjectID, &kr.Location, &kr.CreatedAt)
	if err == sql.ErrNoRows {
		return KMSKeyRing{}, false, nil
	}
	if err != nil {
		return KMSKeyRing{}, false, fmt.Errorf("get kms key ring: %w", err)
	}
	return kr, true, nil
}

// ListKMSKeyRings lists key rings under a project/location.
func (s *Store) ListKMSKeyRings(projectID, location string) ([]KMSKeyRing, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, created_at FROM kms_keyrings
		 WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list kms key rings: %w", err)
	}
	defer rows.Close()
	var out []KMSKeyRing
	for rows.Next() {
		var kr KMSKeyRing
		if err := rows.Scan(&kr.Name, &kr.ProjectID, &kr.Location, &kr.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, kr)
	}
	return out, rows.Err()
}

// CreateKMSCryptoKey inserts a crypto key and its first version with sealed material.
func (s *Store) CreateKMSCryptoKey(key KMSCryptoKey, version KMSKeyVersion) (created bool, err error) {
	if key.Name == "" || key.KeyRing == "" {
		return false, fmt.Errorf("kms crypto key requires name and key ring")
	}
	if key.Purpose == "" {
		key.Purpose = KMSPurposeEncrypt
	}
	if key.Algorithm == "" {
		key.Algorithm = KMSAlgoSymmetric
	}
	if key.CreatedAt == "" {
		key.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if version.Name == "" || version.CryptoKey == "" || version.VersionID == "" {
		return false, fmt.Errorf("kms key version requires name, crypto key, and version id")
	}
	if version.State == "" {
		version.State = KMSStateEnabled
	}
	if len(version.KeyMaterialCiphertext) == 0 {
		return false, fmt.Errorf("kms key version requires sealed key material")
	}
	if version.CreatedAt == "" {
		version.CreatedAt = key.CreatedAt
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(
		`INSERT OR IGNORE INTO kms_keys (name, keyring, purpose, algorithm, created_at) VALUES (?, ?, ?, ?, ?)`,
		key.Name, key.KeyRing, key.Purpose, key.Algorithm, key.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create kms crypto key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if _, err := tx.Exec(
		`INSERT INTO kms_key_versions (name, crypto_key, version_id, state, key_material_ciphertext, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		version.Name, version.CryptoKey, version.VersionID, version.State, version.KeyMaterialCiphertext, version.CreatedAt,
	); err != nil {
		return false, fmt.Errorf("create kms key version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// GetKMSCryptoKey loads crypto key metadata.
func (s *Store) GetKMSCryptoKey(name string) (KMSCryptoKey, bool, error) {
	var k KMSCryptoKey
	err := s.db.QueryRow(
		`SELECT name, keyring, purpose, algorithm, created_at FROM kms_keys WHERE name = ?`, name,
	).Scan(&k.Name, &k.KeyRing, &k.Purpose, &k.Algorithm, &k.CreatedAt)
	if err == sql.ErrNoRows {
		return KMSCryptoKey{}, false, nil
	}
	if err != nil {
		return KMSCryptoKey{}, false, fmt.Errorf("get kms crypto key: %w", err)
	}
	return k, true, nil
}

// ListKMSCryptoKeys lists keys under a key ring.
func (s *Store) ListKMSCryptoKeys(keyRing string) ([]KMSCryptoKey, error) {
	rows, err := s.db.Query(
		`SELECT name, keyring, purpose, algorithm, created_at FROM kms_keys WHERE keyring = ? ORDER BY name`,
		keyRing,
	)
	if err != nil {
		return nil, fmt.Errorf("list kms crypto keys: %w", err)
	}
	defer rows.Close()
	var out []KMSCryptoKey
	for rows.Next() {
		var k KMSCryptoKey
		if err := rows.Scan(&k.Name, &k.KeyRing, &k.Purpose, &k.Algorithm, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// GetKMSKeyVersion loads a version by name.
func (s *Store) GetKMSKeyVersion(name string) (KMSKeyVersion, bool, error) {
	var v KMSKeyVersion
	err := s.db.QueryRow(
		`SELECT name, crypto_key, version_id, state, key_material_ciphertext, created_at
		 FROM kms_key_versions WHERE name = ?`, name,
	).Scan(&v.Name, &v.CryptoKey, &v.VersionID, &v.State, &v.KeyMaterialCiphertext, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return KMSKeyVersion{}, false, nil
	}
	if err != nil {
		return KMSKeyVersion{}, false, fmt.Errorf("get kms key version: %w", err)
	}
	return v, true, nil
}

// PrimaryKMSKeyVersion returns version "1" for a crypto key (lab default primary).
func (s *Store) PrimaryKMSKeyVersion(cryptoKey string) (KMSKeyVersion, bool, error) {
	return s.GetKMSKeyVersion(cryptoKey + "/cryptoKeyVersions/1")
}

// DestroyKMSKeyVersion marks a version DESTROYED.
func (s *Store) DestroyKMSKeyVersion(name string) (KMSKeyVersion, bool, error) {
	v, ok, err := s.GetKMSKeyVersion(name)
	if err != nil || !ok {
		return KMSKeyVersion{}, ok, err
	}
	if v.State == KMSStateDestroyed {
		return v, true, nil
	}
	_, err = s.db.Exec(`UPDATE kms_key_versions SET state = ? WHERE name = ?`, KMSStateDestroyed, name)
	if err != nil {
		return KMSKeyVersion{}, false, fmt.Errorf("destroy kms key version: %w", err)
	}
	v.State = KMSStateDestroyed
	return v, true, nil
}

// RestoreKMSKeyVersion marks a DESTROYED version ENABLED again (lab; Cloud KMS restore is narrower).
func (s *Store) RestoreKMSKeyVersion(name string) (KMSKeyVersion, bool, error) {
	v, ok, err := s.GetKMSKeyVersion(name)
	if err != nil || !ok {
		return KMSKeyVersion{}, ok, err
	}
	if v.State == KMSStateEnabled {
		return v, true, nil
	}
	_, err = s.db.Exec(`UPDATE kms_key_versions SET state = ? WHERE name = ?`, KMSStateEnabled, name)
	if err != nil {
		return KMSKeyVersion{}, false, fmt.Errorf("restore kms key version: %w", err)
	}
	v.State = KMSStateEnabled
	return v, true, nil
}

// ListKMSKeyVersions lists versions under a crypto key.
func (s *Store) ListKMSKeyVersions(cryptoKey string) ([]KMSKeyVersion, error) {
	rows, err := s.db.Query(
		`SELECT name, crypto_key, version_id, state, key_material_ciphertext, created_at
		 FROM kms_key_versions WHERE crypto_key = ? ORDER BY CAST(version_id AS INTEGER), version_id`,
		cryptoKey,
	)
	if err != nil {
		return nil, fmt.Errorf("list kms key versions: %w", err)
	}
	defer rows.Close()
	var out []KMSKeyVersion
	for rows.Next() {
		var v KMSKeyVersion
		if err := rows.Scan(&v.Name, &v.CryptoKey, &v.VersionID, &v.State, &v.KeyMaterialCiphertext, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
