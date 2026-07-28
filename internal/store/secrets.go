package store

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	SecretVersionEnabled   = "ENABLED"
	SecretVersionDisabled  = "DISABLED"
	SecretVersionDestroyed = "DESTROYED"
)

// Secret is Secret Manager secret metadata.
type Secret struct {
	Name      string
	ProjectID string
	CreatedAt string
}

// SecretVersion is a sealed secret version row.
type SecretVersion struct {
	Name              string
	SecretName        string
	VersionID         string
	PayloadCiphertext []byte
	State             string
	CreatedAt         string
}

// CreateSecret inserts a secret. created=false means already exists.
func (s *Store) CreateSecret(name, projectID string) (*Secret, bool, error) {
	name = strings.TrimSpace(name)
	projectID = strings.TrimSpace(projectID)
	if name == "" || projectID == "" {
		return nil, false, fmt.Errorf("secret name and project required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO secrets (name, project_id, created_at) VALUES (?, ?, ?)`,
		name, projectID, now,
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
	return &Secret{Name: name, ProjectID: projectID, CreatedAt: now}, true, nil
}

// GetSecret loads secret metadata.
func (s *Store) GetSecret(name string) (*Secret, bool, error) {
	var sec Secret
	err := s.db.QueryRow(
		`SELECT name, project_id, created_at FROM secrets WHERE name = ?`, name,
	).Scan(&sec.Name, &sec.ProjectID, &sec.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &sec, true, nil
}

// ListSecrets lists secrets for a project id.
func (s *Store) ListSecrets(projectID string) ([]Secret, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, created_at FROM secrets WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Secret
	for rows.Next() {
		var sec Secret
		if err := rows.Scan(&sec.Name, &sec.ProjectID, &sec.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sec)
	}
	return out, rows.Err()
}

// DeleteSecret removes a secret and its versions.
func (s *Store) DeleteSecret(name string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM secret_versions WHERE secret_name = ?`, name); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM secrets WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// AddSecretVersion seals plaintext and appends a version.
func (s *Store) AddSecretVersion(secretName string, plaintext []byte) (*SecretVersion, error) {
	if _, ok, err := s.GetSecret(secretName); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("secret not found")
	}
	var maxID int
	err := s.db.QueryRow(
		`SELECT COALESCE(MAX(CAST(version_id AS INTEGER)), 0) FROM secret_versions WHERE secret_name = ?`,
		secretName,
	).Scan(&maxID)
	if err != nil {
		return nil, err
	}
	versionID := strconv.Itoa(maxID + 1)
	sealed, err := s.Seal(plaintext)
	if err != nil {
		return nil, fmt.Errorf("seal secret payload: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	name := secretName + "/versions/" + versionID
	_, err = s.db.Exec(
		`INSERT INTO secret_versions (name, secret_name, version_id, payload_ciphertext, state, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		name, secretName, versionID, sealed, SecretVersionEnabled, now,
	)
	if err != nil {
		return nil, err
	}
	return &SecretVersion{
		Name: name, SecretName: secretName, VersionID: versionID,
		PayloadCiphertext: sealed, State: SecretVersionEnabled, CreatedAt: now,
	}, nil
}

// GetSecretVersion loads a version. versionID may be "latest".
func (s *Store) GetSecretVersion(secretName, versionID string) (*SecretVersion, bool, error) {
	versionID = strings.TrimSpace(versionID)
	if versionID == "" || versionID == "latest" {
		var v SecretVersion
		err := s.db.QueryRow(
			`SELECT name, secret_name, version_id, payload_ciphertext, state, created_at
			 FROM secret_versions WHERE secret_name = ?
			 ORDER BY CAST(version_id AS INTEGER) DESC LIMIT 1`,
			secretName,
		).Scan(&v.Name, &v.SecretName, &v.VersionID, &v.PayloadCiphertext, &v.State, &v.CreatedAt)
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return &v, true, nil
	}
	var v SecretVersion
	err := s.db.QueryRow(
		`SELECT name, secret_name, version_id, payload_ciphertext, state, created_at
		 FROM secret_versions WHERE secret_name = ? AND version_id = ?`,
		secretName, versionID,
	).Scan(&v.Name, &v.SecretName, &v.VersionID, &v.PayloadCiphertext, &v.State, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &v, true, nil
}

// AccessSecretVersion unseals an ENABLED version. DESTROYED/DISABLED refuse access.
func (s *Store) AccessSecretVersion(secretName, versionID string) (plaintext []byte, version *SecretVersion, err error) {
	v, ok, err := s.GetSecretVersion(secretName, versionID)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, fmt.Errorf("version not found")
	}
	switch v.State {
	case SecretVersionDestroyed:
		return nil, v, fmt.Errorf("secret version destroyed")
	case SecretVersionDisabled:
		return nil, v, fmt.Errorf("secret version disabled")
	case SecretVersionEnabled:
		plain, err := s.Unseal(v.PayloadCiphertext)
		if err != nil {
			return nil, v, fmt.Errorf("unseal secret payload: %w", err)
		}
		return plain, v, nil
	default:
		return nil, v, fmt.Errorf("secret version state %q", v.State)
	}
}

// SetSecretVersionState updates version state. Destroy clears ciphertext.
func (s *Store) SetSecretVersionState(secretName, versionID, state string) (*SecretVersion, error) {
	v, ok, err := s.GetSecretVersion(secretName, versionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("version not found")
	}
	if v.State == SecretVersionDestroyed {
		return nil, fmt.Errorf("secret version destroyed")
	}
	switch state {
	case SecretVersionEnabled, SecretVersionDisabled, SecretVersionDestroyed:
	default:
		return nil, fmt.Errorf("invalid state %q", state)
	}
	if state == SecretVersionDestroyed {
		_, err = s.db.Exec(
			`UPDATE secret_versions SET state = ?, payload_ciphertext = X'' WHERE name = ?`,
			state, v.Name,
		)
	} else {
		_, err = s.db.Exec(`UPDATE secret_versions SET state = ? WHERE name = ?`, state, v.Name)
	}
	if err != nil {
		return nil, err
	}
	v.State = state
	if state == SecretVersionDestroyed {
		v.PayloadCiphertext = nil
	}
	return v, nil
}

// ListSecretVersions lists versions for a secret.
func (s *Store) ListSecretVersions(secretName string) ([]SecretVersion, error) {
	rows, err := s.db.Query(
		`SELECT name, secret_name, version_id, payload_ciphertext, state, created_at
		 FROM secret_versions WHERE secret_name = ? ORDER BY CAST(version_id AS INTEGER)`,
		secretName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SecretVersion
	for rows.Next() {
		var v SecretVersion
		if err := rows.Scan(&v.Name, &v.SecretName, &v.VersionID, &v.PayloadCiphertext, &v.State, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ParseSecretVersionName splits projects/.../secrets/.../versions/... or .../secrets/... + version.
func ParseSecretVersionName(name string) (secretName, versionID string, ok bool) {
	name = strings.TrimSpace(name)
	const marker = "/versions/"
	i := strings.LastIndex(name, marker)
	if i < 0 {
		return "", "", false
	}
	return name[:i], name[i+len(marker):], true
}
