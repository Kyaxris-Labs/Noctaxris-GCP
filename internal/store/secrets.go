package store

import (
	"database/sql"
	"encoding/json"
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
	Name             string
	ProjectID        string
	Labels           map[string]string
	Annotations      map[string]string
	Replication      map[string]any
	CMEKKmsKeyName   string
	RotationPeriod   string // duration theatre, e.g. "86400s"
	NextRotationTime string // RFC3339
	TopicsJSON       string // JSON array theatre
	CreatedAt        string
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
	return s.CreateSecretWithMeta(name, projectID, nil, nil, nil, "")
}

// CreateSecretWithMeta inserts a secret with optional labels/annotations/replication/CMEK name.
// replication is stored as JSON theatre; CMEK key name is stored but not enforced for seal/unseal.
func (s *Store) CreateSecretWithMeta(name, projectID string, labels, annotations map[string]string, replication map[string]any, cmekKmsKeyName string) (*Secret, bool, error) {
	return s.CreateSecretWithRotation(name, projectID, labels, annotations, replication, cmekKmsKeyName, "", "", nil)
}

// CreateSecretWithRotation inserts a secret including optional rotation theatre fields.
func (s *Store) CreateSecretWithRotation(name, projectID string, labels, annotations map[string]string, replication map[string]any, cmekKmsKeyName, rotationPeriod, nextRotationTime string, topics []map[string]string) (*Secret, bool, error) {
	name = strings.TrimSpace(name)
	projectID = strings.TrimSpace(projectID)
	if name == "" || projectID == "" {
		return nil, false, fmt.Errorf("secret name and project required")
	}
	if labels == nil {
		labels = map[string]string{}
	}
	if annotations == nil {
		annotations = map[string]string{}
	}
	if replication == nil {
		replication = map[string]any{"automatic": map[string]any{}}
	}
	topicsJSON := "[]"
	if len(topics) > 0 {
		raw, err := json.Marshal(topics)
		if err != nil {
			return nil, false, err
		}
		topicsJSON = string(raw)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO secrets
		 (name, project_id, labels_json, annotations_json, replication_json, cmek_kms_key_name,
		  rotation_period, next_rotation_time, topics_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name, projectID, encodeStringMap(labels), encodeStringMap(annotations), encodeAnyMap(replication),
		strings.TrimSpace(cmekKmsKeyName), strings.TrimSpace(rotationPeriod), strings.TrimSpace(nextRotationTime), topicsJSON, now,
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
	return &Secret{
		Name: name, ProjectID: projectID, Labels: labels, Annotations: annotations,
		Replication: replication, CMEKKmsKeyName: strings.TrimSpace(cmekKmsKeyName),
		RotationPeriod: strings.TrimSpace(rotationPeriod), NextRotationTime: strings.TrimSpace(nextRotationTime),
		TopicsJSON: topicsJSON, CreatedAt: now,
	}, true, nil
}

// GetSecret loads secret metadata.
func (s *Store) GetSecret(name string) (*Secret, bool, error) {
	var sec Secret
	var labelsJSON, annotationsJSON, replicationJSON string
	err := s.db.QueryRow(
		`SELECT name, project_id, COALESCE(labels_json, '{}'), COALESCE(annotations_json, '{}'),
		 COALESCE(replication_json, '{}'), COALESCE(cmek_kms_key_name, ''),
		 COALESCE(rotation_period, ''), COALESCE(next_rotation_time, ''), COALESCE(topics_json, '[]'),
		 created_at FROM secrets WHERE name = ?`, name,
	).Scan(&sec.Name, &sec.ProjectID, &labelsJSON, &annotationsJSON, &replicationJSON, &sec.CMEKKmsKeyName,
		&sec.RotationPeriod, &sec.NextRotationTime, &sec.TopicsJSON, &sec.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	sec.Labels = decodeStringMap(labelsJSON)
	sec.Annotations = decodeStringMap(annotationsJSON)
	sec.Replication = decodeAnyMap(replicationJSON)
	if sec.TopicsJSON == "" {
		sec.TopicsJSON = "[]"
	}
	return &sec, true, nil
}

// PatchSecret updates labels and/or annotations. Nil pointers leave fields unchanged.
func (s *Store) PatchSecret(name string, labels, annotations *map[string]string) (*Secret, error) {
	return s.PatchSecretMeta(name, labels, annotations, nil, nil, nil)
}

// PatchSecretMeta patches labels/annotations and optional rotation theatre fields.
// rotationPeriod / nextRotationTime / topics: nil = leave unchanged; empty string clears.
func (s *Store) PatchSecretMeta(name string, labels, annotations *map[string]string, rotationPeriod, nextRotationTime *string, topics *[]map[string]string) (*Secret, error) {
	sec, ok, err := s.GetSecret(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("secret not found")
	}
	if labels != nil {
		sec.Labels = *labels
		if sec.Labels == nil {
			sec.Labels = map[string]string{}
		}
	}
	if annotations != nil {
		sec.Annotations = *annotations
		if sec.Annotations == nil {
			sec.Annotations = map[string]string{}
		}
	}
	if rotationPeriod != nil {
		sec.RotationPeriod = strings.TrimSpace(*rotationPeriod)
	}
	if nextRotationTime != nil {
		sec.NextRotationTime = strings.TrimSpace(*nextRotationTime)
	}
	if topics != nil {
		if len(*topics) == 0 {
			sec.TopicsJSON = "[]"
		} else {
			raw, err := json.Marshal(*topics)
			if err != nil {
				return nil, err
			}
			sec.TopicsJSON = string(raw)
		}
	}
	_, err = s.db.Exec(
		`UPDATE secrets SET labels_json = ?, annotations_json = ?, rotation_period = ?, next_rotation_time = ?, topics_json = ? WHERE name = ?`,
		encodeStringMap(sec.Labels), encodeStringMap(sec.Annotations), sec.RotationPeriod, sec.NextRotationTime, sec.TopicsJSON, name,
	)
	if err != nil {
		return nil, err
	}
	return sec, nil
}

// RotateSecretTheatre appends a new enabled version. If payload is nil/empty, copies the latest
// enabled payload (or stores "rotated" when no prior version exists). Advances nextRotationTime
// by rotationPeriod when both are set (lite theatre; no Pub/Sub publish).
func (s *Store) RotateSecretTheatre(secretName string, payload []byte) (*SecretVersion, *Secret, error) {
	sec, ok, err := s.GetSecret(secretName)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, fmt.Errorf("secret not found")
	}
	plain := payload
	if len(plain) == 0 {
		prev, found, err := s.GetSecretVersion(secretName, "latest")
		if err != nil {
			return nil, nil, err
		}
		if found && prev.State == SecretVersionEnabled {
			plain, err = s.Unseal(prev.PayloadCiphertext)
			if err != nil {
				return nil, nil, fmt.Errorf("unseal previous version: %w", err)
			}
		} else {
			plain = []byte("rotated")
		}
	}
	v, err := s.AddSecretVersion(secretName, plain)
	if err != nil {
		return nil, nil, err
	}
	if sec.RotationPeriod != "" && sec.NextRotationTime != "" {
		if next, ok := advanceRotationTime(sec.NextRotationTime, sec.RotationPeriod); ok {
			sec.NextRotationTime = next
			_, _ = s.db.Exec(`UPDATE secrets SET next_rotation_time = ? WHERE name = ?`, next, secretName)
		}
	}
	return v, sec, nil
}

func advanceRotationTime(nextRFC3339, period string) (string, bool) {
	t, err := time.Parse(time.RFC3339Nano, nextRFC3339)
	if err != nil {
		t, err = time.Parse(time.RFC3339, nextRFC3339)
		if err != nil {
			return "", false
		}
	}
	period = strings.TrimSpace(period)
	if !strings.HasSuffix(period, "s") {
		return "", false
	}
	secs, err := strconv.ParseFloat(strings.TrimSuffix(period, "s"), 64)
	if err != nil || secs <= 0 {
		return "", false
	}
	return t.Add(time.Duration(secs * float64(time.Second))).UTC().Format(time.RFC3339Nano), true
}

// ListSecrets lists secrets for a project id.
func (s *Store) ListSecrets(projectID string) ([]Secret, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, COALESCE(labels_json, '{}'), COALESCE(annotations_json, '{}'),
		 COALESCE(replication_json, '{}'), COALESCE(cmek_kms_key_name, ''),
		 COALESCE(rotation_period, ''), COALESCE(next_rotation_time, ''), COALESCE(topics_json, '[]'),
		 created_at
		 FROM secrets WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Secret
	for rows.Next() {
		var sec Secret
		var labelsJSON, annotationsJSON, replicationJSON string
		if err := rows.Scan(&sec.Name, &sec.ProjectID, &labelsJSON, &annotationsJSON, &replicationJSON, &sec.CMEKKmsKeyName,
			&sec.RotationPeriod, &sec.NextRotationTime, &sec.TopicsJSON, &sec.CreatedAt); err != nil {
			return nil, err
		}
		sec.Labels = decodeStringMap(labelsJSON)
		sec.Annotations = decodeStringMap(annotationsJSON)
		sec.Replication = decodeAnyMap(replicationJSON)
		if sec.TopicsJSON == "" {
			sec.TopicsJSON = "[]"
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
// stateFilter may be empty (all) or ENABLED / DISABLED / DESTROYED.
func (s *Store) ListSecretVersions(secretName, stateFilter string) ([]SecretVersion, error) {
	stateFilter = strings.ToUpper(strings.TrimSpace(stateFilter))
	var rows *sql.Rows
	var err error
	if stateFilter == "" {
		rows, err = s.db.Query(
			`SELECT name, secret_name, version_id, payload_ciphertext, state, created_at
			 FROM secret_versions WHERE secret_name = ? ORDER BY CAST(version_id AS INTEGER)`,
			secretName,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT name, secret_name, version_id, payload_ciphertext, state, created_at
			 FROM secret_versions WHERE secret_name = ? AND state = ? ORDER BY CAST(version_id AS INTEGER)`,
			secretName, stateFilter,
		)
	}
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

func encodeAnyMap(m map[string]any) string {
	if m == nil {
		return "{}"
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func decodeAnyMap(raw string) map[string]any {
	out := map[string]any{}
	if raw == "" || raw == "{}" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
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
