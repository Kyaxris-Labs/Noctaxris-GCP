package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
)

// ErrInvalidSeedName is returned when a standalone seed name cannot be a project id.
var ErrInvalidSeedName = errors.New("invalid standalone seed name")

// StandaloneProject is a project with an empty CRM parent and its minted owner token.
// AccessToken is returned to the root caller. It is not the root access token.
type StandaloneProject struct {
	ProjectID           string
	ServiceAccountEmail string
	AccessToken         string
}

// SeedStandaloneProject creates a project with an empty parent, or returns the
// existing seed when seedName was already stored. The second call returns the
// same access token and does not mint another. The service account is
// roles/owner on projects/{id} only. rootSAEmail is never minted or bound.
// rootAccessToken is never stored. A minted token equal to it is discarded.
func (s *Store) SeedStandaloneProject(seedName, rootSAEmail, rootAccessToken string) (StandaloneProject, error) {
	seedName = strings.TrimSpace(seedName)
	if !validStandaloneProjectID(seedName) {
		return StandaloneProject{}, ErrInvalidSeedName
	}
	existing, ok, err := s.lookupStandalone(seedName)
	if err != nil {
		return StandaloneProject{}, err
	}
	if ok {
		return existing, nil
	}

	email, err := standaloneServiceAccountEmail(seedName, rootSAEmail)
	if err != nil {
		return StandaloneProject{}, err
	}
	token, err := s.mintNonRootToken(email, rootAccessToken)
	if err != nil {
		return StandaloneProject{}, err
	}
	keepToken := false
	defer func() {
		if !keepToken {
			_ = s.deleteAccessToken(authn.HashToken(token))
		}
	}()

	tx, err := s.db.Begin()
	if err != nil {
		return StandaloneProject{}, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.Exec(
		`INSERT INTO projects (id, display_name, state, labels_json, parent, seed_name, seed_access_token, seed_sa_email, created_at)
		 VALUES (?, ?, 'ACTIVE', '{}', '', ?, ?, ?, ?)`,
		seedName, seedName, seedName, token, email, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			_ = tx.Rollback()
			again, found, lookErr := s.lookupStandalone(seedName)
			if lookErr != nil {
				return StandaloneProject{}, lookErr
			}
			if found {
				return again, nil
			}
			return StandaloneProject{}, ErrAlreadyExists
		}
		return StandaloneProject{}, fmt.Errorf("seed standalone project: %w", err)
	}

	uniqueID := fmt.Sprintf("1%019d", time.Now().UnixNano()%1_000_000_000_000_000_000)
	if _, err := tx.Exec(
		`INSERT INTO service_accounts (project_id, email, unique_id, display_name, disabled, deleted_at, created_at)
		 VALUES (?, ?, ?, ?, 0, '', ?)`,
		seedName, email, uniqueID, "standalone", now,
	); err != nil {
		return StandaloneProject{}, fmt.Errorf("seed standalone service account: %w", err)
	}

	resource := "projects/" + seedName
	pol := authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/owner",
			Members: []string{"serviceAccount:" + email},
		}},
		Etag: "ACAB",
	}
	raw, err := json.Marshal(pol)
	if err != nil {
		return StandaloneProject{}, err
	}
	if _, err := tx.Exec(
		`INSERT INTO iam_policies (resource, etag, policy_json) VALUES (?, ?, ?)`,
		resource, pol.Etag, string(raw),
	); err != nil {
		return StandaloneProject{}, fmt.Errorf("seed standalone iam policy: %w", err)
	}
	if err := enableLabSeedServices(tx, seedName); err != nil {
		return StandaloneProject{}, err
	}
	if err := tx.Commit(); err != nil {
		return StandaloneProject{}, err
	}
	keepToken = true
	return StandaloneProject{
		ProjectID:           seedName,
		ServiceAccountEmail: email,
		AccessToken:         token,
	}, nil
}

func (s *Store) lookupStandalone(seedName string) (StandaloneProject, bool, error) {
	var out StandaloneProject
	err := s.db.QueryRow(
		`SELECT id, COALESCE(seed_sa_email, ''), COALESCE(seed_access_token, '') FROM projects WHERE seed_name = ?`,
		seedName,
	).Scan(&out.ProjectID, &out.ServiceAccountEmail, &out.AccessToken)
	if err == sql.ErrNoRows {
		return StandaloneProject{}, false, nil
	}
	if err != nil {
		return StandaloneProject{}, false, err
	}
	if out.AccessToken == "" || out.ServiceAccountEmail == "" {
		return StandaloneProject{}, false, fmt.Errorf("standalone seed %q is incomplete", seedName)
	}
	return out, true, nil
}

func (s *Store) mintNonRootToken(email, rootAccessToken string) (string, error) {
	rootAccessToken = strings.TrimSpace(rootAccessToken)
	for attempt := 0; attempt < 5; attempt++ {
		token, _, err := labtoken.Mint(s, email, 0)
		if err != nil {
			return "", err
		}
		if rootAccessToken == "" || token != rootAccessToken {
			return token, nil
		}
		if err := s.deleteAccessToken(authn.HashToken(token)); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("minted token matched root access token")
}

func (s *Store) deleteAccessToken(tokenHash string) error {
	if tokenHash == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM access_tokens WHERE token_hash = ?`, tokenHash)
	return err
}

func standaloneServiceAccountEmail(projectID, rootSA string) (string, error) {
	rootSA = strings.TrimSpace(rootSA)
	candidates := []string{
		"standalone@" + projectID + ".iam.gserviceaccount.com",
		"standalone-sa@" + projectID + ".iam.gserviceaccount.com",
	}
	for _, email := range candidates {
		if rootSA != "" && strings.EqualFold(email, rootSA) {
			continue
		}
		return email, nil
	}
	return "", fmt.Errorf("standalone service account email collides with root")
}

func validStandaloneProjectID(id string) bool {
	if id == "" || id == "-" || strings.ContainsAny(id, "/: \t\n") {
		return false
	}
	return true
}
