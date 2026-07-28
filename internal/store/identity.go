package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

// Project is a Cloud Resource Manager project row.
type Project struct {
	ID          string
	DisplayName string
	State       string
	CreatedAt   string
}

// ServiceAccount is an IAM service account row.
type ServiceAccount struct {
	ProjectID   string
	Email       string
	UniqueID    string
	DisplayName string
	Disabled    bool
	CreatedAt   string
}

// ServiceAccountKey is an IAM service account key row (private material sealed).
type ServiceAccountKey struct {
	Name            string
	SAEmail         string
	KeyAlgorithm    string
	PrivateKeyType  string
	PrivateKeyData  []byte // sealed ciphertext at rest
	ValidAfterTime  string
	ValidBeforeTime string
	CreatedAt       string
}

// ServiceUsage is a Service Usage row for a project.
type ServiceUsage struct {
	ProjectID   string
	ServiceName string
	State       string
}

// GetProject loads a project by id.
func (s *Store) GetProject(id string) (Project, bool, error) {
	var p Project
	err := s.db.QueryRow(
		`SELECT id, display_name, state, created_at FROM projects WHERE id = ?`,
		id,
	).Scan(&p.ID, &p.DisplayName, &p.State, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return Project{}, false, nil
	}
	if err != nil {
		return Project{}, false, err
	}
	return p, true, nil
}

// UpdateProjectDisplayName sets the project display name.
func (s *Store) UpdateProjectDisplayName(id, displayName string) (Project, bool, error) {
	res, err := s.db.Exec(`UPDATE projects SET display_name = ? WHERE id = ?`, displayName, id)
	if err != nil {
		return Project{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Project{}, false, err
	}
	if n == 0 {
		return Project{}, false, nil
	}
	return s.GetProject(id)
}

// PutIAMPolicyJSON stores (or replaces) the IAM policy for resource.
func (s *Store) PutIAMPolicyJSON(resource string, policy authz.Policy) error {
	if resource == "" {
		return fmt.Errorf("resource required")
	}
	if policy.Etag == "" {
		policy.Etag = "ACAB"
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO iam_policies (resource, etag, policy_json) VALUES (?, ?, ?)
		 ON CONFLICT(resource) DO UPDATE SET etag = excluded.etag, policy_json = excluded.policy_json`,
		resource, policy.Etag, string(raw),
	)
	return err
}

// CreateServiceAccount inserts a service account.
func (s *Store) CreateServiceAccount(sa ServiceAccount) error {
	if sa.ProjectID == "" || sa.Email == "" || sa.UniqueID == "" {
		return fmt.Errorf("project_id, email, and unique_id required")
	}
	if sa.CreatedAt == "" {
		sa.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	disabled := 0
	if sa.Disabled {
		disabled = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO service_accounts (project_id, email, unique_id, display_name, disabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sa.ProjectID, sa.Email, sa.UniqueID, sa.DisplayName, disabled, sa.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

// GetServiceAccount loads a service account by email.
func (s *Store) GetServiceAccount(email string) (ServiceAccount, bool, error) {
	var sa ServiceAccount
	var disabled int
	err := s.db.QueryRow(
		`SELECT project_id, email, unique_id, display_name, disabled, created_at FROM service_accounts WHERE email = ?`,
		email,
	).Scan(&sa.ProjectID, &sa.Email, &sa.UniqueID, &sa.DisplayName, &disabled, &sa.CreatedAt)
	if err == sql.ErrNoRows {
		return ServiceAccount{}, false, nil
	}
	if err != nil {
		return ServiceAccount{}, false, err
	}
	sa.Disabled = disabled != 0
	return sa, true, nil
}

// GetServiceAccountInProject loads a service account by project and email or unique id.
func (s *Store) GetServiceAccountInProject(projectID, emailOrID string) (ServiceAccount, bool, error) {
	var sa ServiceAccount
	var disabled int
	err := s.db.QueryRow(
		`SELECT project_id, email, unique_id, display_name, disabled, created_at FROM service_accounts
		 WHERE project_id = ? AND (email = ? OR unique_id = ?)`,
		projectID, emailOrID, emailOrID,
	).Scan(&sa.ProjectID, &sa.Email, &sa.UniqueID, &sa.DisplayName, &disabled, &sa.CreatedAt)
	if err == sql.ErrNoRows {
		return ServiceAccount{}, false, nil
	}
	if err != nil {
		return ServiceAccount{}, false, err
	}
	sa.Disabled = disabled != 0
	return sa, true, nil
}

// ListServiceAccounts returns service accounts for a project.
func (s *Store) ListServiceAccounts(projectID string) ([]ServiceAccount, error) {
	rows, err := s.db.Query(
		`SELECT project_id, email, unique_id, display_name, disabled, created_at FROM service_accounts
		 WHERE project_id = ? ORDER BY email`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServiceAccount
	for rows.Next() {
		var sa ServiceAccount
		var disabled int
		if err := rows.Scan(&sa.ProjectID, &sa.Email, &sa.UniqueID, &sa.DisplayName, &disabled, &sa.CreatedAt); err != nil {
			return nil, err
		}
		sa.Disabled = disabled != 0
		out = append(out, sa)
	}
	return out, rows.Err()
}

// SetServiceAccountDisabled enables or disables a service account by email.
func (s *Store) SetServiceAccountDisabled(email string, disabled bool) (ServiceAccount, bool, error) {
	flag := 0
	if disabled {
		flag = 1
	}
	res, err := s.db.Exec(`UPDATE service_accounts SET disabled = ? WHERE email = ?`, flag, email)
	if err != nil {
		return ServiceAccount{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ServiceAccount{}, false, err
	}
	if n == 0 {
		return ServiceAccount{}, false, nil
	}
	return s.GetServiceAccount(email)
}

// UpdateServiceAccountDisplayName patches the display name of a service account.
func (s *Store) UpdateServiceAccountDisplayName(email, displayName string) (ServiceAccount, bool, error) {
	res, err := s.db.Exec(`UPDATE service_accounts SET display_name = ? WHERE email = ?`, displayName, email)
	if err != nil {
		return ServiceAccount{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ServiceAccount{}, false, err
	}
	if n == 0 {
		return ServiceAccount{}, false, nil
	}
	return s.GetServiceAccount(email)
}

// DeleteServiceAccount removes a service account and its keys.
func (s *Store) DeleteServiceAccount(email string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM sa_keys WHERE sa_email = ?`, email); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM service_accounts WHERE email = ?`, email)
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
	return true, tx.Commit()
}

// CreateServiceAccountKey inserts a sealed key row.
func (s *Store) CreateServiceAccountKey(key ServiceAccountKey) error {
	if key.Name == "" || key.SAEmail == "" || len(key.PrivateKeyData) == 0 {
		return fmt.Errorf("name, sa_email, and private_key_data required")
	}
	if key.KeyAlgorithm == "" {
		key.KeyAlgorithm = "KEY_ALG_RSA_2048"
	}
	if key.PrivateKeyType == "" {
		key.PrivateKeyType = "TYPE_GOOGLE_CREDENTIALS_FILE"
	}
	if key.CreatedAt == "" {
		key.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.Exec(
		`INSERT INTO sa_keys (name, sa_email, key_algorithm, private_key_type, private_key_data, valid_after_time, valid_before_time, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		key.Name, key.SAEmail, key.KeyAlgorithm, key.PrivateKeyType, key.PrivateKeyData,
		key.ValidAfterTime, key.ValidBeforeTime, key.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

// GetServiceAccountKey loads a key by resource name.
func (s *Store) GetServiceAccountKey(name string) (ServiceAccountKey, bool, error) {
	var k ServiceAccountKey
	err := s.db.QueryRow(
		`SELECT name, sa_email, key_algorithm, private_key_type, private_key_data, valid_after_time, valid_before_time, created_at
		 FROM sa_keys WHERE name = ?`,
		name,
	).Scan(&k.Name, &k.SAEmail, &k.KeyAlgorithm, &k.PrivateKeyType, &k.PrivateKeyData, &k.ValidAfterTime, &k.ValidBeforeTime, &k.CreatedAt)
	if err == sql.ErrNoRows {
		return ServiceAccountKey{}, false, nil
	}
	if err != nil {
		return ServiceAccountKey{}, false, err
	}
	return k, true, nil
}

// ListServiceAccountKeys lists keys for a service account email.
func (s *Store) ListServiceAccountKeys(saEmail string) ([]ServiceAccountKey, error) {
	rows, err := s.db.Query(
		`SELECT name, sa_email, key_algorithm, private_key_type, private_key_data, valid_after_time, valid_before_time, created_at
		 FROM sa_keys WHERE sa_email = ? ORDER BY created_at`,
		saEmail,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServiceAccountKey
	for rows.Next() {
		var k ServiceAccountKey
		if err := rows.Scan(&k.Name, &k.SAEmail, &k.KeyAlgorithm, &k.PrivateKeyType, &k.PrivateKeyData, &k.ValidAfterTime, &k.ValidBeforeTime, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteServiceAccountKey removes a key by resource name.
func (s *Store) DeleteServiceAccountKey(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM sa_keys WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListServiceUsage returns service usage rows for a project.
// When stateFilter is non-empty (ENABLED or DISABLED), only matching rows are returned.
func (s *Store) ListServiceUsage(projectID string, stateFilter string) ([]ServiceUsage, error) {
	stateFilter = strings.ToUpper(strings.TrimSpace(stateFilter))
	var (
		rows *sql.Rows
		err  error
	)
	if stateFilter == "" {
		rows, err = s.db.Query(
			`SELECT project_id, service_name, state FROM service_usage WHERE project_id = ? ORDER BY service_name`,
			projectID,
		)
	} else {
		if stateFilter != "ENABLED" && stateFilter != "DISABLED" {
			return nil, fmt.Errorf("state filter must be ENABLED or DISABLED")
		}
		rows, err = s.db.Query(
			`SELECT project_id, service_name, state FROM service_usage WHERE project_id = ? AND state = ? ORDER BY service_name`,
			projectID, stateFilter,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServiceUsage
	for rows.Next() {
		var u ServiceUsage
		if err := rows.Scan(&u.ProjectID, &u.ServiceName, &u.State); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// BatchEnableServiceUsage enables multiple services atomically (all or nothing).
func (s *Store) BatchEnableServiceUsage(projectID string, serviceNames []string) error {
	if len(serviceNames) == 0 {
		return fmt.Errorf("serviceIds required")
	}
	if len(serviceNames) > 20 {
		return fmt.Errorf("at most 20 services per batchEnable")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, name := range serviceNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("empty service id")
		}
		if _, err := tx.Exec(
			`INSERT INTO service_usage (project_id, service_name, state) VALUES (?, ?, 'ENABLED')
			 ON CONFLICT(project_id, service_name) DO UPDATE SET state = 'ENABLED'`,
			projectID, name,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetServiceUsage loads one service usage row.
func (s *Store) GetServiceUsage(projectID, serviceName string) (ServiceUsage, bool, error) {
	var u ServiceUsage
	err := s.db.QueryRow(
		`SELECT project_id, service_name, state FROM service_usage WHERE project_id = ? AND service_name = ?`,
		projectID, serviceName,
	).Scan(&u.ProjectID, &u.ServiceName, &u.State)
	if err == sql.ErrNoRows {
		return ServiceUsage{}, false, nil
	}
	if err != nil {
		return ServiceUsage{}, false, err
	}
	return u, true, nil
}

// SetServiceUsageState sets ENABLED or DISABLED for a service, inserting if missing.
func (s *Store) SetServiceUsageState(projectID, serviceName, state string) error {
	state = strings.ToUpper(strings.TrimSpace(state))
	if state != "ENABLED" && state != "DISABLED" {
		return fmt.Errorf("state must be ENABLED or DISABLED")
	}
	_, err := s.db.Exec(
		`INSERT INTO service_usage (project_id, service_name, state) VALUES (?, ?, ?)
		 ON CONFLICT(project_id, service_name) DO UPDATE SET state = excluded.state`,
		projectID, serviceName, state,
	)
	return err
}

// ErrAlreadyExists is returned when a unique constraint is violated on insert.
var ErrAlreadyExists = fmt.Errorf("already exists")

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "constraint failed")
}
