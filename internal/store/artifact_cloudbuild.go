package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const artifactCloudbuildSchema = `
CREATE TABLE IF NOT EXISTS ar_repositories (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  repository_id TEXT NOT NULL,
  format TEXT NOT NULL DEFAULT 'DOCKER',
  description TEXT NOT NULL DEFAULT '',
  labels_json TEXT NOT NULL DEFAULT '{}',
  mode TEXT NOT NULL DEFAULT 'STANDARD_REPOSITORY',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ar_packages (
  name TEXT PRIMARY KEY,
  repository_name TEXT NOT NULL,
  package_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ar_versions (
  name TEXT PRIMARY KEY,
  package_name TEXT NOT NULL,
  version_id TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  related_tags_json TEXT NOT NULL DEFAULT '[]',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cb_builds (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL DEFAULT 'global',
  build_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'WORKING',
  status_detail TEXT NOT NULL DEFAULT '',
  project_number TEXT NOT NULL DEFAULT '0',
  build_json TEXT NOT NULL DEFAULT '{}',
  create_time TEXT NOT NULL,
  start_time TEXT NOT NULL DEFAULT '',
  finish_time TEXT NOT NULL DEFAULT '',
  log_url TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS cb_triggers (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  trigger_id TEXT NOT NULL,
  trigger_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`

func (s *Store) migrateArtifactCloudbuild() error {
	if _, err := s.db.Exec(artifactCloudbuildSchema); err != nil {
		return fmt.Errorf("apply artifact registry/cloud build schema: %w", err)
	}
	return nil
}

// --- Artifact Registry ---

// ArRepository is an Artifact Registry repository metadata row.
type ArRepository struct {
	Name         string
	ProjectID    string
	Location     string
	RepositoryID string
	Format       string
	Description  string
	LabelsJSON   string
	Mode         string
	CreatedAt    string
	UpdatedAt    string
}

// ArPackage is an Artifact Registry package metadata row (no blob storage).
type ArPackage struct {
	Name            string
	RepositoryName  string
	PackageID       string
	DisplayName     string
	CreatedAt       string
	UpdatedAt       string
}

// ArVersion is an Artifact Registry version metadata row (no blob storage).
type ArVersion struct {
	Name            string
	PackageName     string
	VersionID       string
	Description     string
	RelatedTagsJSON string
	MetadataJSON    string
	CreatedAt       string
	UpdatedAt       string
}

// CreateArRepository inserts a repository. Returns false when the name already exists.
func (s *Store) CreateArRepository(repo ArRepository) (created bool, err error) {
	if repo.Name == "" || repo.ProjectID == "" || repo.Location == "" || repo.RepositoryID == "" {
		return false, fmt.Errorf("ar repository requires name, project, location, and repository id")
	}
	if repo.Format == "" {
		repo.Format = "DOCKER"
	}
	if repo.Mode == "" {
		repo.Mode = "STANDARD_REPOSITORY"
	}
	if repo.LabelsJSON == "" {
		repo.LabelsJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if repo.CreatedAt == "" {
		repo.CreatedAt = now
	}
	if repo.UpdatedAt == "" {
		repo.UpdatedAt = repo.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO ar_repositories
		 (name, project_id, location, repository_id, format, description, labels_json, mode, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		repo.Name, repo.ProjectID, repo.Location, repo.RepositoryID, repo.Format, repo.Description,
		repo.LabelsJSON, repo.Mode, repo.CreatedAt, repo.UpdatedAt,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetArRepository returns a repository by resource name.
func (s *Store) GetArRepository(name string) (ArRepository, bool, error) {
	var r ArRepository
	err := s.db.QueryRow(
		`SELECT name, project_id, location, repository_id, format, description, labels_json, mode, created_at, updated_at
		 FROM ar_repositories WHERE name = ?`, name,
	).Scan(&r.Name, &r.ProjectID, &r.Location, &r.RepositoryID, &r.Format, &r.Description,
		&r.LabelsJSON, &r.Mode, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return ArRepository{}, false, nil
	}
	if err != nil {
		return ArRepository{}, false, err
	}
	return r, true, nil
}

// ListArRepositories lists repositories for a project/location.
func (s *Store) ListArRepositories(projectID, location string) ([]ArRepository, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, repository_id, format, description, labels_json, mode, created_at, updated_at
		 FROM ar_repositories WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArRepository
	for rows.Next() {
		var r ArRepository
		if err := rows.Scan(&r.Name, &r.ProjectID, &r.Location, &r.RepositoryID, &r.Format, &r.Description,
			&r.LabelsJSON, &r.Mode, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateArRepository updates description and labels; empty fields leave existing values.
func (s *Store) UpdateArRepository(name, description, labelsJSON string) (ArRepository, bool, error) {
	cur, ok, err := s.GetArRepository(name)
	if err != nil || !ok {
		return ArRepository{}, ok, err
	}
	if description != "" {
		cur.Description = description
	}
	if labelsJSON != "" {
		cur.LabelsJSON = labelsJSON
	}
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(
		`UPDATE ar_repositories SET description = ?, labels_json = ?, updated_at = ? WHERE name = ?`,
		cur.Description, cur.LabelsJSON, cur.UpdatedAt, name,
	)
	if err != nil {
		return ArRepository{}, false, err
	}
	return cur, true, nil
}

// DeleteArRepository deletes a repository and its packages/versions. Returns false if missing.
func (s *Store) DeleteArRepository(name string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	pkgs, err := tx.Query(`SELECT name FROM ar_packages WHERE repository_name = ?`, name)
	if err != nil {
		return false, err
	}
	var pkgNames []string
	for pkgs.Next() {
		var n string
		if err := pkgs.Scan(&n); err != nil {
			_ = pkgs.Close()
			return false, err
		}
		pkgNames = append(pkgNames, n)
	}
	_ = pkgs.Close()
	if err := pkgs.Err(); err != nil {
		return false, err
	}
	for _, pn := range pkgNames {
		if _, err := tx.Exec(`DELETE FROM ar_versions WHERE package_name = ?`, pn); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(`DELETE FROM ar_packages WHERE repository_name = ?`, name); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM ar_repositories WHERE name = ?`, name)
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

// CreateArPackage inserts package metadata. Returns false when the name already exists.
func (s *Store) CreateArPackage(pkg ArPackage) (bool, error) {
	if pkg.Name == "" || pkg.RepositoryName == "" || pkg.PackageID == "" {
		return false, fmt.Errorf("ar package requires name, repository, and package id")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if pkg.CreatedAt == "" {
		pkg.CreatedAt = now
	}
	if pkg.UpdatedAt == "" {
		pkg.UpdatedAt = pkg.CreatedAt
	}
	if pkg.DisplayName == "" {
		pkg.DisplayName = pkg.PackageID
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO ar_packages (name, repository_name, package_id, display_name, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		pkg.Name, pkg.RepositoryName, pkg.PackageID, pkg.DisplayName, pkg.CreatedAt, pkg.UpdatedAt,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// GetArPackage returns a package by resource name.
func (s *Store) GetArPackage(name string) (ArPackage, bool, error) {
	var p ArPackage
	err := s.db.QueryRow(
		`SELECT name, repository_name, package_id, display_name, created_at, updated_at FROM ar_packages WHERE name = ?`,
		name,
	).Scan(&p.Name, &p.RepositoryName, &p.PackageID, &p.DisplayName, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return ArPackage{}, false, nil
	}
	if err != nil {
		return ArPackage{}, false, err
	}
	return p, true, nil
}

// ListArPackages lists packages under a repository.
func (s *Store) ListArPackages(repositoryName string) ([]ArPackage, error) {
	rows, err := s.db.Query(
		`SELECT name, repository_name, package_id, display_name, created_at, updated_at
		 FROM ar_packages WHERE repository_name = ? ORDER BY name`, repositoryName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArPackage
	for rows.Next() {
		var p ArPackage
		if err := rows.Scan(&p.Name, &p.RepositoryName, &p.PackageID, &p.DisplayName, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteArPackage deletes a package and its versions. Returns false if missing.
func (s *Store) DeleteArPackage(name string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM ar_versions WHERE package_name = ?`, name); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM ar_packages WHERE name = ?`, name)
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

// CreateArVersion inserts version metadata. Returns false when the name already exists.
func (s *Store) CreateArVersion(v ArVersion) (bool, error) {
	if v.Name == "" || v.PackageName == "" || v.VersionID == "" {
		return false, fmt.Errorf("ar version requires name, package, and version id")
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
		v.UpdatedAt = v.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO ar_versions
		 (name, package_name, version_id, description, related_tags_json, metadata_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Name, v.PackageName, v.VersionID, v.Description, v.RelatedTagsJSON, v.MetadataJSON, v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 {
		_, _ = s.db.Exec(`UPDATE ar_packages SET updated_at = ? WHERE name = ?`, v.UpdatedAt, v.PackageName)
	}
	return n > 0, nil
}

// GetArVersion returns a version by resource name.
func (s *Store) GetArVersion(name string) (ArVersion, bool, error) {
	var v ArVersion
	err := s.db.QueryRow(
		`SELECT name, package_name, version_id, description, related_tags_json, metadata_json, created_at, updated_at
		 FROM ar_versions WHERE name = ?`, name,
	).Scan(&v.Name, &v.PackageName, &v.VersionID, &v.Description, &v.RelatedTagsJSON, &v.MetadataJSON, &v.CreatedAt, &v.UpdatedAt)
	if err == sql.ErrNoRows {
		return ArVersion{}, false, nil
	}
	if err != nil {
		return ArVersion{}, false, err
	}
	return v, true, nil
}

// ListArVersions lists versions under a package.
func (s *Store) ListArVersions(packageName string) ([]ArVersion, error) {
	rows, err := s.db.Query(
		`SELECT name, package_name, version_id, description, related_tags_json, metadata_json, created_at, updated_at
		 FROM ar_versions WHERE package_name = ? ORDER BY name`, packageName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArVersion
	for rows.Next() {
		var v ArVersion
		if err := rows.Scan(&v.Name, &v.PackageName, &v.VersionID, &v.Description, &v.RelatedTagsJSON, &v.MetadataJSON, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DeleteArVersion deletes a version. Returns false if missing.
func (s *Store) DeleteArVersion(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM ar_versions WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// --- Cloud Build ---

// CbBuild is a Cloud Build build theatre row (no container execution).
type CbBuild struct {
	Name          string
	ProjectID     string
	Location      string
	BuildID       string
	Status        string
	StatusDetail  string
	ProjectNumber string
	BuildJSON     string
	CreateTime    string
	StartTime     string
	FinishTime    string
	LogURL        string
}

// CbTrigger is a Cloud Build trigger metadata row.
type CbTrigger struct {
	Name        string
	ProjectID   string
	Location    string
	TriggerID   string
	TriggerJSON string
	CreatedAt   string
	UpdatedAt   string
}

// CreateCbBuild inserts a build. Returns false when the name already exists.
func (s *Store) CreateCbBuild(b CbBuild) (bool, error) {
	if b.Name == "" || b.ProjectID == "" || b.BuildID == "" {
		return false, fmt.Errorf("cb build requires name, project, and build id")
	}
	if b.Location == "" {
		b.Location = "global"
	}
	if b.Status == "" {
		b.Status = "WORKING"
	}
	if b.BuildJSON == "" {
		b.BuildJSON = "{}"
	}
	if b.ProjectNumber == "" {
		b.ProjectNumber = "0"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if b.CreateTime == "" {
		b.CreateTime = now
	}
	if b.StartTime == "" {
		b.StartTime = b.CreateTime
	}
	if b.LogURL == "" {
		b.LogURL = fmt.Sprintf("http://127.0.0.1:4588/v1/%s/logs", b.Name)
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cb_builds
		 (name, project_id, location, build_id, status, status_detail, project_number, build_json, create_time, start_time, finish_time, log_url)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.Name, b.ProjectID, b.Location, b.BuildID, b.Status, b.StatusDetail, b.ProjectNumber,
		b.BuildJSON, b.CreateTime, b.StartTime, b.FinishTime, b.LogURL,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// GetCbBuild returns a build by resource name.
func (s *Store) GetCbBuild(name string) (CbBuild, bool, error) {
	var b CbBuild
	err := s.db.QueryRow(
		`SELECT name, project_id, location, build_id, status, status_detail, project_number, build_json,
		        create_time, start_time, finish_time, log_url
		 FROM cb_builds WHERE name = ?`, name,
	).Scan(&b.Name, &b.ProjectID, &b.Location, &b.BuildID, &b.Status, &b.StatusDetail, &b.ProjectNumber,
		&b.BuildJSON, &b.CreateTime, &b.StartTime, &b.FinishTime, &b.LogURL)
	if err == sql.ErrNoRows {
		return CbBuild{}, false, nil
	}
	if err != nil {
		return CbBuild{}, false, err
	}
	return b, true, nil
}

// GetCbBuildByID looks up a build by project and build id (any location).
func (s *Store) GetCbBuildByID(projectID, buildID string) (CbBuild, bool, error) {
	var b CbBuild
	err := s.db.QueryRow(
		`SELECT name, project_id, location, build_id, status, status_detail, project_number, build_json,
		        create_time, start_time, finish_time, log_url
		 FROM cb_builds WHERE project_id = ? AND build_id = ? ORDER BY create_time DESC LIMIT 1`,
		projectID, buildID,
	).Scan(&b.Name, &b.ProjectID, &b.Location, &b.BuildID, &b.Status, &b.StatusDetail, &b.ProjectNumber,
		&b.BuildJSON, &b.CreateTime, &b.StartTime, &b.FinishTime, &b.LogURL)
	if err == sql.ErrNoRows {
		return CbBuild{}, false, nil
	}
	if err != nil {
		return CbBuild{}, false, err
	}
	return b, true, nil
}

// ListCbBuilds lists builds for a project, optionally filtered by location.
func (s *Store) ListCbBuilds(projectID, location string) ([]CbBuild, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if location == "" || location == "-" {
		rows, err = s.db.Query(
			`SELECT name, project_id, location, build_id, status, status_detail, project_number, build_json,
			        create_time, start_time, finish_time, log_url
			 FROM cb_builds WHERE project_id = ? ORDER BY create_time DESC`, projectID,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT name, project_id, location, build_id, status, status_detail, project_number, build_json,
			        create_time, start_time, finish_time, log_url
			 FROM cb_builds WHERE project_id = ? AND location = ? ORDER BY create_time DESC`,
			projectID, location,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CbBuild
	for rows.Next() {
		var b CbBuild
		if err := rows.Scan(&b.Name, &b.ProjectID, &b.Location, &b.BuildID, &b.Status, &b.StatusDetail, &b.ProjectNumber,
			&b.BuildJSON, &b.CreateTime, &b.StartTime, &b.FinishTime, &b.LogURL); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AdvanceCbBuildToSuccess flips WORKING builds to SUCCESS theatre and returns the updated row.
// Persists per-step status SUCCESS into build_json so getBuild shows STEPS.
func (s *Store) AdvanceCbBuildToSuccess(name string) (CbBuild, bool, error) {
	b, ok, err := s.GetCbBuild(name)
	if err != nil || !ok {
		return CbBuild{}, ok, err
	}
	if b.Status == "WORKING" || b.Status == "QUEUED" || b.Status == "PENDING" {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		b.Status = "SUCCESS"
		b.StatusDetail = "lab theatre: steps marked SUCCESS (not executed)"
		b.FinishTime = now
		b.BuildJSON = markCbBuildStepsStatus(b.BuildJSON, "SUCCESS")
		_, err = s.db.Exec(
			`UPDATE cb_builds SET status = ?, status_detail = ?, finish_time = ?, build_json = ? WHERE name = ?`,
			b.Status, b.StatusDetail, b.FinishTime, b.BuildJSON, name,
		)
		if err != nil {
			return CbBuild{}, false, err
		}
	}
	return b, true, nil
}

// markCbBuildStepsStatus sets status on each step in build JSON (Cloud Build BuildStep.status).
func markCbBuildStepsStatus(buildJSON, status string) string {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(buildJSON), &cfg); err != nil || cfg == nil {
		cfg = map[string]any{}
	}
	steps, _ := cfg["steps"].([]any)
	if len(steps) == 0 {
		raw, _ := json.Marshal(cfg)
		if buildJSON == "" {
			return "{}"
		}
		if len(cfg) == 0 {
			return buildJSON
		}
		return string(raw)
	}
	outSteps := make([]any, 0, len(steps))
	for _, step := range steps {
		sm, ok := step.(map[string]any)
		if !ok {
			outSteps = append(outSteps, step)
			continue
		}
		sm["status"] = status
		outSteps = append(outSteps, sm)
	}
	cfg["steps"] = outSteps
	raw, err := json.Marshal(cfg)
	if err != nil {
		return buildJSON
	}
	return string(raw)
}

// CreateCbTrigger inserts a trigger. Returns false when the name already exists.
func (s *Store) CreateCbTrigger(t CbTrigger) (bool, error) {
	if t.Name == "" || t.ProjectID == "" || t.Location == "" || t.TriggerID == "" {
		return false, fmt.Errorf("cb trigger requires name, project, location, and trigger id")
	}
	if t.TriggerJSON == "" {
		t.TriggerJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if t.CreatedAt == "" {
		t.CreatedAt = now
	}
	if t.UpdatedAt == "" {
		t.UpdatedAt = t.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cb_triggers (name, project_id, location, trigger_id, trigger_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.ProjectID, t.Location, t.TriggerID, t.TriggerJSON, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// GetCbTrigger returns a trigger by resource name.
func (s *Store) GetCbTrigger(name string) (CbTrigger, bool, error) {
	var t CbTrigger
	err := s.db.QueryRow(
		`SELECT name, project_id, location, trigger_id, trigger_json, created_at, updated_at FROM cb_triggers WHERE name = ?`,
		name,
	).Scan(&t.Name, &t.ProjectID, &t.Location, &t.TriggerID, &t.TriggerJSON, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return CbTrigger{}, false, nil
	}
	if err != nil {
		return CbTrigger{}, false, err
	}
	return t, true, nil
}

// ListCbTriggers lists triggers for a project/location.
func (s *Store) ListCbTriggers(projectID, location string) ([]CbTrigger, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, trigger_id, trigger_json, created_at, updated_at
		 FROM cb_triggers WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CbTrigger
	for rows.Next() {
		var t CbTrigger
		if err := rows.Scan(&t.Name, &t.ProjectID, &t.Location, &t.TriggerID, &t.TriggerJSON, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteCbTrigger deletes a trigger. Returns false if missing.
func (s *Store) DeleteCbTrigger(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM cb_triggers WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// NewCbBuildID returns a unique build id for theatre creates.
func NewCbBuildID() string {
	return uuid.NewString()
}

// NewCbTriggerID returns a unique trigger id when the client omits one.
func NewCbTriggerID() string {
	return uuid.NewString()
}
