package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// --- Firebase Auth ---

// FirebaseUser is an Identity Toolkit user row.
type FirebaseUser struct {
	LocalID          string
	ProjectID        string
	Email            string
	PasswordHash     string
	DisplayName      string
	Disabled         bool
	CustomAttributes string
	CreatedAt        string
}

// CreateFirebaseUser inserts a user. created=false means email already exists.
func (s *Store) CreateFirebaseUser(u FirebaseUser) (*FirebaseUser, bool, error) {
	u.ProjectID = strings.TrimSpace(u.ProjectID)
	u.Email = strings.TrimSpace(strings.ToLower(u.Email))
	if u.ProjectID == "" || u.Email == "" || u.PasswordHash == "" {
		return nil, false, fmt.Errorf("project, email, and password required")
	}
	if u.LocalID == "" {
		u.LocalID = uuid.NewString()
	}
	if u.CustomAttributes == "" {
		u.CustomAttributes = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	u.CreatedAt = now
	disabled := 0
	if u.Disabled {
		disabled = 1
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO firebase_users
		 (local_id, project_id, email, password_hash, display_name, disabled, custom_attributes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.LocalID, u.ProjectID, u.Email, u.PasswordHash, u.DisplayName, disabled, u.CustomAttributes, now,
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
	return &u, true, nil
}

// GetFirebaseUserByEmail loads a user by project+email.
func (s *Store) GetFirebaseUserByEmail(projectID, email string) (*FirebaseUser, bool, error) {
	return s.scanFirebaseUser(
		`SELECT local_id, project_id, email, password_hash, display_name, disabled, custom_attributes, created_at
		 FROM firebase_users WHERE project_id = ? AND email = ?`,
		projectID, strings.ToLower(strings.TrimSpace(email)),
	)
}

// GetFirebaseUserByLocalID loads a user by local id.
func (s *Store) GetFirebaseUserByLocalID(localID string) (*FirebaseUser, bool, error) {
	return s.scanFirebaseUser(
		`SELECT local_id, project_id, email, password_hash, display_name, disabled, custom_attributes, created_at
		 FROM firebase_users WHERE local_id = ?`,
		localID,
	)
}

func (s *Store) scanFirebaseUser(q string, args ...any) (*FirebaseUser, bool, error) {
	var u FirebaseUser
	var disabled int
	err := s.db.QueryRow(q, args...).Scan(
		&u.LocalID, &u.ProjectID, &u.Email, &u.PasswordHash, &u.DisplayName, &disabled, &u.CustomAttributes, &u.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	u.Disabled = disabled != 0
	return &u, true, nil
}

// ListFirebaseUsers lists users for a project.
func (s *Store) ListFirebaseUsers(projectID string) ([]FirebaseUser, error) {
	list, _, err := s.ListFirebaseUsersPage(projectID, 0, "")
	return list, err
}

// ListFirebaseUsersPage lists users with optional pageSize and pageToken (local_id cursor).
func (s *Store) ListFirebaseUsersPage(projectID string, pageSize int, pageToken string) ([]FirebaseUser, string, error) {
	if pageSize <= 0 {
		pageSize = 1000
	}
	q := `SELECT local_id, project_id, email, password_hash, display_name, disabled, custom_attributes, created_at
	      FROM firebase_users WHERE project_id = ?`
	args := []any{projectID}
	if pageToken != "" {
		q += ` AND local_id > ?`
		args = append(args, pageToken)
	}
	q += ` ORDER BY local_id LIMIT ?`
	args = append(args, pageSize+1)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []FirebaseUser
	for rows.Next() {
		var u FirebaseUser
		var disabled int
		if err := rows.Scan(&u.LocalID, &u.ProjectID, &u.Email, &u.PasswordHash, &u.DisplayName, &disabled, &u.CustomAttributes, &u.CreatedAt); err != nil {
			return nil, "", err
		}
		u.Disabled = disabled != 0
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > pageSize {
		next = out[pageSize-1].LocalID
		out = out[:pageSize]
	}
	return out, next, nil
}

// FirebaseOOBCode is a password-reset (or similar) out-of-band code.
type FirebaseOOBCode struct {
	OOBCode     string
	ProjectID   string
	Email       string
	RequestType string
	LocalID     string
	CreatedAt   string
	ExpiresAt   string
	Used        bool
}

// CreateFirebaseOOBCode stores a lab OOB code (password reset theatre).
func (s *Store) CreateFirebaseOOBCode(c FirebaseOOBCode) error {
	if c.OOBCode == "" || c.ProjectID == "" || c.Email == "" || c.RequestType == "" {
		return fmt.Errorf("oob code, project, email, and request type required")
	}
	now := time.Now().UTC()
	if c.CreatedAt == "" {
		c.CreatedAt = now.Format(time.RFC3339Nano)
	}
	if c.ExpiresAt == "" {
		c.ExpiresAt = now.Add(time.Hour).Format(time.RFC3339Nano)
	}
	used := 0
	if c.Used {
		used = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO firebase_oob_codes (oob_code, project_id, email, request_type, local_id, created_at, expires_at, used)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.OOBCode, c.ProjectID, strings.ToLower(strings.TrimSpace(c.Email)), c.RequestType, c.LocalID, c.CreatedAt, c.ExpiresAt, used,
	)
	return err
}

// GetFirebaseOOBCode loads an unused, unexpired OOB code.
func (s *Store) GetFirebaseOOBCode(code string) (*FirebaseOOBCode, bool, error) {
	var c FirebaseOOBCode
	var used int
	err := s.db.QueryRow(
		`SELECT oob_code, project_id, email, request_type, local_id, created_at, expires_at, used
		 FROM firebase_oob_codes WHERE oob_code = ?`, code,
	).Scan(&c.OOBCode, &c.ProjectID, &c.Email, &c.RequestType, &c.LocalID, &c.CreatedAt, &c.ExpiresAt, &used)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	c.Used = used != 0
	return &c, true, nil
}

// ConsumeFirebaseOOBCode marks a code used if still valid.
func (s *Store) ConsumeFirebaseOOBCode(code string) (*FirebaseOOBCode, bool, error) {
	c, ok, err := s.GetFirebaseOOBCode(code)
	if err != nil || !ok {
		return nil, ok, err
	}
	if c.Used {
		return nil, false, nil
	}
	exp, err := time.Parse(time.RFC3339Nano, c.ExpiresAt)
	if err != nil {
		exp, err = time.Parse(time.RFC3339, c.ExpiresAt)
		if err != nil {
			return nil, false, err
		}
	}
	if !time.Now().UTC().Before(exp) {
		return nil, false, nil
	}
	res, err := s.db.Exec(`UPDATE firebase_oob_codes SET used = 1 WHERE oob_code = ? AND used = 0`, code)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return nil, false, err
	}
	c.Used = true
	return c, true, nil
}

// UpdateFirebaseUser patches display name / disabled / custom attributes.
func (s *Store) UpdateFirebaseUser(u FirebaseUser) error {
	disabled := 0
	if u.Disabled {
		disabled = 1
	}
	if u.CustomAttributes == "" {
		u.CustomAttributes = "{}"
	}
	_, err := s.db.Exec(
		`UPDATE firebase_users SET display_name = ?, disabled = ?, custom_attributes = ?, password_hash = COALESCE(NULLIF(?, ''), password_hash)
		 WHERE local_id = ?`,
		u.DisplayName, disabled, u.CustomAttributes, u.PasswordHash, u.LocalID,
	)
	return err
}

// DeleteFirebaseUser removes a user by local id.
func (s *Store) DeleteFirebaseUser(localID string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM firebase_users WHERE local_id = ?`, localID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
