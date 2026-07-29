package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DefaultOrganizationName is the seeded lab organization resource name.
const DefaultOrganizationName = "organizations/noctaxris-gcp-org"

// DefaultOrganizationID is the id segment of DefaultOrganizationName.
const DefaultOrganizationID = "noctaxris-gcp-org"

const appEngineCRMSchema = `
CREATE TABLE IF NOT EXISTS crm_organizations (
  name TEXT PRIMARY KEY,
  org_id TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS crm_folders (
  name TEXT PRIMARY KEY,
  folder_id TEXT NOT NULL UNIQUE,
  parent TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  etag TEXT NOT NULL DEFAULT 'ACAB',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  delete_time TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_crm_folders_parent ON crm_folders (parent);

CREATE TABLE IF NOT EXISTS appengine_apps (
  name TEXT PRIMARY KEY,
  app_id TEXT NOT NULL UNIQUE,
  location_id TEXT NOT NULL DEFAULT 'us-central',
  serving_status TEXT NOT NULL DEFAULT 'SERVING',
  auth_domain TEXT NOT NULL DEFAULT 'gmail.com',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS appengine_services (
  name TEXT PRIMARY KEY,
  app_id TEXT NOT NULL,
  service_id TEXT NOT NULL,
  split_json TEXT NOT NULL DEFAULT '{}',
  shard_by TEXT NOT NULL DEFAULT 'UNSPECIFIED',
  migrate_traffic INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (app_id, service_id)
);

CREATE TABLE IF NOT EXISTS appengine_versions (
  name TEXT PRIMARY KEY,
  app_id TEXT NOT NULL,
  service_id TEXT NOT NULL,
  version_id TEXT NOT NULL,
  runtime TEXT NOT NULL DEFAULT '',
  env TEXT NOT NULL DEFAULT 'standard',
  env_variables_json TEXT NOT NULL DEFAULT '{}',
  serving_status TEXT NOT NULL DEFAULT 'SERVING',
  created_at TEXT NOT NULL,
  UNIQUE (app_id, service_id, version_id)
);

CREATE INDEX IF NOT EXISTS idx_appengine_versions_svc ON appengine_versions (app_id, service_id);
`

func (s *Store) migrateAppEngineCRM() error {
	if _, err := s.db.Exec(appEngineCRMSchema); err != nil {
		return fmt.Errorf("apply appengine/crm schema: %w", err)
	}
	return nil
}

// Organization is a Cloud Resource Manager organization row.
type Organization struct {
	Name        string
	OrgID       string
	DisplayName string
	State       string
	CreatedAt   string
	UpdatedAt   string
}

// Folder is a Cloud Resource Manager folder row.
type Folder struct {
	Name        string
	FolderID    string
	Parent      string
	DisplayName string
	State       string
	Etag        string
	CreatedAt   string
	UpdatedAt   string
	DeleteTime  string
}

// AppEngineApp is an App Engine Application row (control-plane theatre).
type AppEngineApp struct {
	Name          string
	AppID         string
	LocationID    string
	ServingStatus string
	AuthDomain    string
	CreatedAt     string
	UpdatedAt     string
}

// AppEngineService is an App Engine Service row.
type AppEngineService struct {
	Name           string
	AppID          string
	ServiceID      string
	SplitJSON      string
	ShardBy        string
	MigrateTraffic bool
	CreatedAt      string
	UpdatedAt      string
}

// AppEngineVersion is an App Engine Version metadata row (no serving).
type AppEngineVersion struct {
	Name              string
	AppID             string
	ServiceID         string
	VersionID         string
	Runtime           string
	Env               string
	EnvVariablesJSON  string
	ServingStatus     string
	CreatedAt         string
}

// EnsureLabOrganization inserts the default lab organization when missing.
func (s *Store) EnsureLabOrganization() error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO crm_organizations (name, org_id, display_name, state, created_at, updated_at)
		 VALUES (?, ?, ?, 'ACTIVE', ?, ?)`,
		DefaultOrganizationName, DefaultOrganizationID, "Noctaxris-GCP Lab Org", now, now,
	)
	return err
}

// CreateOrganization inserts an organization (lab; real GCP does not expose create).
func (s *Store) CreateOrganization(org Organization) (bool, error) {
	if org.OrgID == "" {
		return false, fmt.Errorf("org id required")
	}
	if org.Name == "" {
		org.Name = "organizations/" + org.OrgID
	}
	if org.DisplayName == "" {
		org.DisplayName = org.OrgID
	}
	if org.State == "" {
		org.State = "ACTIVE"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if org.CreatedAt == "" {
		org.CreatedAt = now
	}
	if org.UpdatedAt == "" {
		org.UpdatedAt = org.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO crm_organizations (name, org_id, display_name, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		org.Name, org.OrgID, org.DisplayName, org.State, org.CreatedAt, org.UpdatedAt,
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

// GetOrganization loads an organization by resource name or id.
func (s *Store) GetOrganization(nameOrID string) (Organization, bool, error) {
	nameOrID = strings.TrimPrefix(strings.TrimSpace(nameOrID), "organizations/")
	if nameOrID == "" {
		return Organization{}, false, nil
	}
	name := "organizations/" + nameOrID
	var o Organization
	err := s.db.QueryRow(
		`SELECT name, org_id, display_name, state, created_at, updated_at FROM crm_organizations
		 WHERE name = ? OR org_id = ?`,
		name, nameOrID,
	).Scan(&o.Name, &o.OrgID, &o.DisplayName, &o.State, &o.CreatedAt, &o.UpdatedAt)
	if err == sql.ErrNoRows {
		return Organization{}, false, nil
	}
	if err != nil {
		return Organization{}, false, err
	}
	return o, true, nil
}

// CRMParent implements authz.CRMParentStore for projects and folders.
// Seeded projects hang under DefaultOrganizationName (same theatre as getAncestry /
// project JSON parent). Folders use the stored parent chain. Organizations have
// no parent.
func (s *Store) CRMParent(resource string) (string, bool, error) {
	resource = strings.TrimSpace(resource)
	switch {
	case strings.HasPrefix(resource, "organizations/"):
		rest := strings.TrimPrefix(resource, "organizations/")
		if rest == "" || strings.Contains(rest, "/") {
			return "", false, nil
		}
		return "", false, nil
	case strings.HasPrefix(resource, "folders/"):
		rest := strings.TrimPrefix(resource, "folders/")
		if rest == "" || strings.Contains(rest, "/") {
			return "", false, nil
		}
		f, ok, err := s.GetFolder(resource)
		if err != nil || !ok {
			return "", ok, err
		}
		if f.Parent == "" {
			return "", false, nil
		}
		return f.Parent, true, nil
	case strings.HasPrefix(resource, "projects/"):
		rest := strings.TrimPrefix(resource, "projects/")
		if rest == "" || strings.Contains(rest, "/") {
			return "", false, nil
		}
		return DefaultOrganizationName, true, nil
	default:
		return "", false, nil
	}
}

// ListOrganizations returns all organizations ordered by id.
func (s *Store) ListOrganizations() ([]Organization, error) {
	rows, err := s.db.Query(
		`SELECT name, org_id, display_name, state, created_at, updated_at FROM crm_organizations ORDER BY org_id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Organization
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.Name, &o.OrgID, &o.DisplayName, &o.State, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CreateFolder inserts a folder under parent (organizations/... or folders/...).
func (s *Store) CreateFolder(f Folder) (Folder, bool, error) {
	if f.Parent == "" {
		return Folder{}, false, fmt.Errorf("parent required")
	}
	if f.DisplayName == "" {
		return Folder{}, false, fmt.Errorf("displayName required")
	}
	if f.FolderID == "" {
		// UUID-based numeric-looking id; time.Now().UnixNano() collides on coarse clocks (Windows).
		f.FolderID = strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	}
	if f.Name == "" {
		f.Name = "folders/" + f.FolderID
	}
	if f.State == "" {
		f.State = "ACTIVE"
	}
	if f.Etag == "" {
		f.Etag = "ACAB"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if f.CreatedAt == "" {
		f.CreatedAt = now
	}
	if f.UpdatedAt == "" {
		f.UpdatedAt = f.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO crm_folders
		 (name, folder_id, parent, display_name, state, etag, created_at, updated_at, delete_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, '')`,
		f.Name, f.FolderID, f.Parent, f.DisplayName, f.State, f.Etag, f.CreatedAt, f.UpdatedAt,
	)
	if err != nil {
		return Folder{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Folder{}, false, err
	}
	if n == 0 {
		return Folder{}, false, nil
	}
	out, ok, err := s.GetFolder(f.Name)
	return out, ok, err
}

// GetFolder loads a folder by resource name or id.
func (s *Store) GetFolder(nameOrID string) (Folder, bool, error) {
	nameOrID = strings.TrimPrefix(strings.TrimSpace(nameOrID), "folders/")
	if nameOrID == "" {
		return Folder{}, false, nil
	}
	name := "folders/" + nameOrID
	var f Folder
	err := s.db.QueryRow(
		`SELECT name, folder_id, parent, display_name, state, etag, created_at, updated_at, delete_time
		 FROM crm_folders WHERE name = ? OR folder_id = ?`,
		name, nameOrID,
	).Scan(&f.Name, &f.FolderID, &f.Parent, &f.DisplayName, &f.State, &f.Etag, &f.CreatedAt, &f.UpdatedAt, &f.DeleteTime)
	if err == sql.ErrNoRows {
		return Folder{}, false, nil
	}
	if err != nil {
		return Folder{}, false, err
	}
	return f, true, nil
}

// ListFolders returns folders under parent. When showDeleted is false, DELETE_REQUESTED are omitted.
func (s *Store) ListFolders(parent string, showDeleted bool) ([]Folder, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if showDeleted {
		rows, err = s.db.Query(
			`SELECT name, folder_id, parent, display_name, state, etag, created_at, updated_at, delete_time
			 FROM crm_folders WHERE parent = ? ORDER BY display_name`,
			parent,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT name, folder_id, parent, display_name, state, etag, created_at, updated_at, delete_time
			 FROM crm_folders WHERE parent = ? AND state = 'ACTIVE' ORDER BY display_name`,
			parent,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.Name, &f.FolderID, &f.Parent, &f.DisplayName, &f.State, &f.Etag, &f.CreatedAt, &f.UpdatedAt, &f.DeleteTime); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpdateFolderDisplayName patches displayName on an ACTIVE folder.
func (s *Store) UpdateFolderDisplayName(nameOrID, displayName string) (Folder, bool, error) {
	f, ok, err := s.GetFolder(nameOrID)
	if err != nil || !ok {
		return Folder{}, ok, err
	}
	if f.State != "ACTIVE" {
		return Folder{}, false, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	etag := uuid.NewString()[:8]
	res, err := s.db.Exec(
		`UPDATE crm_folders SET display_name = ?, updated_at = ?, etag = ? WHERE name = ? AND state = 'ACTIVE'`,
		displayName, now, etag, f.Name,
	)
	if err != nil {
		return Folder{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Folder{}, false, err
	}
	if n == 0 {
		return Folder{}, false, nil
	}
	return s.GetFolder(f.Name)
}

// DeleteFolder marks a folder DELETE_REQUESTED (GCP-shaped soft delete).
func (s *Store) DeleteFolder(nameOrID string) (Folder, bool, error) {
	f, ok, err := s.GetFolder(nameOrID)
	if err != nil || !ok {
		return Folder{}, ok, err
	}
	if f.State == "DELETE_REQUESTED" {
		return f, true, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE crm_folders SET state = 'DELETE_REQUESTED', delete_time = ?, updated_at = ? WHERE name = ?`,
		now, now, f.Name,
	)
	if err != nil {
		return Folder{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Folder{}, false, err
	}
	if n == 0 {
		return Folder{}, false, nil
	}
	return s.GetFolder(f.Name)
}

// UndeleteFolder restores a DELETE_REQUESTED folder to ACTIVE.
func (s *Store) UndeleteFolder(nameOrID string) (Folder, bool, error) {
	f, ok, err := s.GetFolder(nameOrID)
	if err != nil || !ok {
		return Folder{}, ok, err
	}
	if f.State == "ACTIVE" {
		return f, true, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	etag := uuid.NewString()[:8]
	res, err := s.db.Exec(
		`UPDATE crm_folders SET state = 'ACTIVE', delete_time = '', updated_at = ?, etag = ? WHERE name = ?`,
		now, etag, f.Name,
	)
	if err != nil {
		return Folder{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Folder{}, false, err
	}
	if n == 0 {
		return Folder{}, false, nil
	}
	return s.GetFolder(f.Name)
}

// MoveFolder sets a new parent on an ACTIVE folder.
func (s *Store) MoveFolder(nameOrID, destinationParent string) (Folder, bool, error) {
	if destinationParent == "" {
		return Folder{}, false, fmt.Errorf("destinationParent required")
	}
	f, ok, err := s.GetFolder(nameOrID)
	if err != nil || !ok {
		return Folder{}, ok, err
	}
	if f.State != "ACTIVE" {
		return Folder{}, false, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	etag := uuid.NewString()[:8]
	res, err := s.db.Exec(
		`UPDATE crm_folders SET parent = ?, updated_at = ?, etag = ? WHERE name = ? AND state = 'ACTIVE'`,
		destinationParent, now, etag, f.Name,
	)
	if err != nil {
		return Folder{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Folder{}, false, err
	}
	if n == 0 {
		return Folder{}, false, nil
	}
	return s.GetFolder(f.Name)
}

// SearchFolders returns folders matching a simple query substring on display name, parent, or state.
// Empty query returns all ACTIVE folders. Supports lite forms: displayName=, parent=, state=.
func (s *Store) SearchFolders(query string) ([]Folder, error) {
	query = strings.TrimSpace(query)
	rows, err := s.db.Query(
		`SELECT name, folder_id, parent, display_name, state, etag, created_at, updated_at, delete_time
		 FROM crm_folders ORDER BY display_name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	qLower := strings.ToLower(query)
	var out []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.Name, &f.FolderID, &f.Parent, &f.DisplayName, &f.State, &f.Etag, &f.CreatedAt, &f.UpdatedAt, &f.DeleteTime); err != nil {
			return nil, err
		}
		if query == "" {
			if f.State == "ACTIVE" {
				out = append(out, f)
			}
			continue
		}
		if strings.Contains(qLower, "=") {
			if matchFolderSearchQuery(f, qLower) {
				out = append(out, f)
			}
			continue
		}
		hay := strings.ToLower(f.DisplayName + " " + f.Parent + " " + f.State + " " + f.Name)
		if strings.Contains(hay, qLower) {
			out = append(out, f)
		}
	}
	return out, rows.Err()
}

func matchFolderSearchQuery(f Folder, qLower string) bool {
	// Support lite GCP query forms: displayName=..., parent=..., state=...
	parts := strings.Fields(qLower)
	matchedAny := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "and" || part == "or" {
			continue
		}
		matchedAny = true
		switch {
		case strings.HasPrefix(part, "displayname="):
			want := strings.Trim(strings.TrimPrefix(part, "displayname="), `"'`)
			want = strings.TrimSuffix(want, "*")
			if !strings.Contains(strings.ToLower(f.DisplayName), want) {
				return false
			}
		case strings.HasPrefix(part, "parent="):
			want := strings.Trim(strings.TrimPrefix(part, "parent="), `"'`)
			if !strings.EqualFold(f.Parent, want) {
				return false
			}
		case strings.HasPrefix(part, "state="), strings.HasPrefix(part, "lifecyclestate="):
			want := part[strings.IndexByte(part, '=')+1:]
			want = strings.Trim(want, `"'`)
			if !strings.EqualFold(f.State, want) {
				return false
			}
		default:
			hay := strings.ToLower(f.DisplayName + " " + f.Parent + " " + f.State)
			if !strings.Contains(hay, part) {
				return false
			}
		}
	}
	return matchedAny
}

// CreateAppEngineApp inserts an Application (one per app id / project).
func (s *Store) CreateAppEngineApp(app AppEngineApp) (bool, error) {
	if app.AppID == "" {
		return false, fmt.Errorf("app id required")
	}
	if app.Name == "" {
		app.Name = "apps/" + app.AppID
	}
	if app.LocationID == "" {
		app.LocationID = "us-central"
	}
	if app.ServingStatus == "" {
		app.ServingStatus = "SERVING"
	}
	if app.AuthDomain == "" {
		app.AuthDomain = "gmail.com"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if app.CreatedAt == "" {
		app.CreatedAt = now
	}
	if app.UpdatedAt == "" {
		app.UpdatedAt = app.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO appengine_apps
		 (name, app_id, location_id, serving_status, auth_domain, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		app.Name, app.AppID, app.LocationID, app.ServingStatus, app.AuthDomain, app.CreatedAt, app.UpdatedAt,
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

// GetAppEngineApp loads an application by app id.
func (s *Store) GetAppEngineApp(appID string) (AppEngineApp, bool, error) {
	appID = strings.TrimPrefix(strings.TrimSpace(appID), "apps/")
	if appID == "" {
		return AppEngineApp{}, false, nil
	}
	var a AppEngineApp
	err := s.db.QueryRow(
		`SELECT name, app_id, location_id, serving_status, auth_domain, created_at, updated_at
		 FROM appengine_apps WHERE app_id = ? OR name = ?`,
		appID, "apps/"+appID,
	).Scan(&a.Name, &a.AppID, &a.LocationID, &a.ServingStatus, &a.AuthDomain, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return AppEngineApp{}, false, nil
	}
	if err != nil {
		return AppEngineApp{}, false, err
	}
	return a, true, nil
}

// EnsureAppEngineService inserts a service under an app when missing.
func (s *Store) EnsureAppEngineService(appID, serviceID string) (AppEngineService, error) {
	if appID == "" || serviceID == "" {
		return AppEngineService{}, fmt.Errorf("app id and service id required")
	}
	name := fmt.Sprintf("apps/%s/services/%s", appID, serviceID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO appengine_services
		 (name, app_id, service_id, split_json, shard_by, migrate_traffic, created_at, updated_at)
		 VALUES (?, ?, ?, '{}', 'UNSPECIFIED', 0, ?, ?)`,
		name, appID, serviceID, now, now,
	)
	if err != nil {
		return AppEngineService{}, err
	}
	svc, ok, err := s.GetAppEngineService(appID, serviceID)
	if err != nil {
		return AppEngineService{}, err
	}
	if !ok {
		return AppEngineService{}, fmt.Errorf("service missing after insert")
	}
	return svc, nil
}

// GetAppEngineService loads a service.
func (s *Store) GetAppEngineService(appID, serviceID string) (AppEngineService, bool, error) {
	var svc AppEngineService
	var migrate int
	err := s.db.QueryRow(
		`SELECT name, app_id, service_id, COALESCE(split_json, '{}'), COALESCE(shard_by, 'UNSPECIFIED'),
		        COALESCE(migrate_traffic, 0), created_at, updated_at
		 FROM appengine_services WHERE app_id = ? AND service_id = ?`,
		appID, serviceID,
	).Scan(&svc.Name, &svc.AppID, &svc.ServiceID, &svc.SplitJSON, &svc.ShardBy, &migrate, &svc.CreatedAt, &svc.UpdatedAt)
	if err == sql.ErrNoRows {
		return AppEngineService{}, false, nil
	}
	if err != nil {
		return AppEngineService{}, false, err
	}
	svc.MigrateTraffic = migrate != 0
	return svc, true, nil
}

// ListAppEngineServices lists services for an app.
func (s *Store) ListAppEngineServices(appID string) ([]AppEngineService, error) {
	rows, err := s.db.Query(
		`SELECT name, app_id, service_id, COALESCE(split_json, '{}'), COALESCE(shard_by, 'UNSPECIFIED'),
		        COALESCE(migrate_traffic, 0), created_at, updated_at
		 FROM appengine_services WHERE app_id = ? ORDER BY service_id`,
		appID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppEngineService
	for rows.Next() {
		var svc AppEngineService
		var migrate int
		if err := rows.Scan(&svc.Name, &svc.AppID, &svc.ServiceID, &svc.SplitJSON, &svc.ShardBy, &migrate, &svc.CreatedAt, &svc.UpdatedAt); err != nil {
			return nil, err
		}
		svc.MigrateTraffic = migrate != 0
		out = append(out, svc)
	}
	return out, rows.Err()
}

// UpdateAppEngineServiceTraffic stores traffic split metadata (no serving).
func (s *Store) UpdateAppEngineServiceTraffic(appID, serviceID, splitJSON, shardBy string, migrateTraffic bool) (AppEngineService, bool, error) {
	svc, ok, err := s.GetAppEngineService(appID, serviceID)
	if err != nil || !ok {
		return AppEngineService{}, ok, err
	}
	if splitJSON == "" {
		splitJSON = "{}"
	}
	if shardBy == "" {
		shardBy = svc.ShardBy
	}
	if shardBy == "" {
		shardBy = "UNSPECIFIED"
	}
	migrate := 0
	if migrateTraffic {
		migrate = 1
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE appengine_services SET split_json = ?, shard_by = ?, migrate_traffic = ?, updated_at = ?
		 WHERE app_id = ? AND service_id = ?`,
		splitJSON, shardBy, migrate, now, appID, serviceID,
	)
	if err != nil {
		return AppEngineService{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return AppEngineService{}, false, err
	}
	if n == 0 {
		return AppEngineService{}, false, nil
	}
	return s.GetAppEngineService(appID, serviceID)
}

// CreateAppEngineVersion stores version metadata and ensures the parent service exists.
func (s *Store) CreateAppEngineVersion(v AppEngineVersion) (bool, error) {
	if v.AppID == "" || v.ServiceID == "" || v.VersionID == "" {
		return false, fmt.Errorf("app, service, and version id required")
	}
	if _, err := s.EnsureAppEngineService(v.AppID, v.ServiceID); err != nil {
		return false, err
	}
	if v.Name == "" {
		v.Name = fmt.Sprintf("apps/%s/services/%s/versions/%s", v.AppID, v.ServiceID, v.VersionID)
	}
	if v.EnvVariablesJSON == "" {
		v.EnvVariablesJSON = "{}"
	}
	if v.Env == "" {
		v.Env = "standard"
	}
	if v.ServingStatus == "" {
		v.ServingStatus = "SERVING"
	}
	if v.CreatedAt == "" {
		v.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO appengine_versions
		 (name, app_id, service_id, version_id, runtime, env, env_variables_json, serving_status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Name, v.AppID, v.ServiceID, v.VersionID, v.Runtime, v.Env, v.EnvVariablesJSON, v.ServingStatus, v.CreatedAt,
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

// GetAppEngineVersion loads a version.
func (s *Store) GetAppEngineVersion(appID, serviceID, versionID string) (AppEngineVersion, bool, error) {
	var v AppEngineVersion
	err := s.db.QueryRow(
		`SELECT name, app_id, service_id, version_id, runtime, env, env_variables_json, serving_status, created_at
		 FROM appengine_versions WHERE app_id = ? AND service_id = ? AND version_id = ?`,
		appID, serviceID, versionID,
	).Scan(&v.Name, &v.AppID, &v.ServiceID, &v.VersionID, &v.Runtime, &v.Env, &v.EnvVariablesJSON, &v.ServingStatus, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return AppEngineVersion{}, false, nil
	}
	if err != nil {
		return AppEngineVersion{}, false, err
	}
	return v, true, nil
}

// ListAppEngineVersions lists versions for a service.
func (s *Store) ListAppEngineVersions(appID, serviceID string) ([]AppEngineVersion, error) {
	rows, err := s.db.Query(
		`SELECT name, app_id, service_id, version_id, runtime, env, env_variables_json, serving_status, created_at
		 FROM appengine_versions WHERE app_id = ? AND service_id = ? ORDER BY version_id`,
		appID, serviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppEngineVersion
	for rows.Next() {
		var v AppEngineVersion
		if err := rows.Scan(&v.Name, &v.AppID, &v.ServiceID, &v.VersionID, &v.Runtime, &v.Env, &v.EnvVariablesJSON, &v.ServingStatus, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DeleteAppEngineVersion removes a version row.
func (s *Store) DeleteAppEngineVersion(appID, serviceID, versionID string) (bool, error) {
	res, err := s.db.Exec(
		`DELETE FROM appengine_versions WHERE app_id = ? AND service_id = ? AND version_id = ?`,
		appID, serviceID, versionID,
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
