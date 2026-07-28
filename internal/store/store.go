package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"

	_ "modernc.org/sqlite"
)

// Store is the SQLite-backed lab state store.
type Store struct {
	db       *sql.DB
	master   MasterKey
	dataRoot string
}

// Open opens or creates state.db under dataRoot and runs migrations.
func Open(dataRoot string, master MasterKey) (*Store, error) {
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create data root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataRoot, "gcs"), 0o700); err != nil {
		return nil, fmt.Errorf("create gcs root: %w", err)
	}
	dbPath := filepath.Join(dataRoot, "state.db")
	dsn := "file:" + filepath.ToSlash(dbPath) + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Serialize writers across request handlers and Eventarc delivery goroutines.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, master: master, dataRoot: dataRoot}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	// Apply service schemas before ensureDataColumns ALTERs that target those tables.
	if err := s.migrateAnalytics(); err != nil {
		return err
	}
	if err := s.migrateArtifactCloudbuild(); err != nil {
		return err
	}
	if err := s.migrateAppEngineCRM(); err != nil {
		return err
	}
	if err := s.migrateWorkflowsSpanner(); err != nil {
		return err
	}
	if err := s.migrateDNSDataflow(); err != nil {
		return err
	}
	if err := s.migrateComputeEngine(); err != nil {
		return err
	}
	if err := s.migrateBigtableMemorystore(); err != nil {
		return err
	}
	if err := s.migrateSecurity(); err != nil {
		return err
	}
	if err := s.migrateFilestore(); err != nil {
		return err
	}
	if err := s.migrateWIF(); err != nil {
		return err
	}
	if err := s.migrateCRMTags(); err != nil {
		return err
	}
	if err := s.ensureDataColumns(); err != nil {
		return err
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM schema_version`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := s.db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, schemaVersion); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureDataColumns() error {
	alters := []string{
		`ALTER TABLE buckets ADD COLUMN labels_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE buckets ADD COLUMN metageneration INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE buckets ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN metadata_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE objects ADD COLUMN cache_control TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN content_disposition TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN content_encoding TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN content_language TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN metageneration INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE pubsub_topics ADD COLUMN labels_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE pubsub_subscriptions ADD COLUMN push_endpoint TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE pubsub_subscriptions ADD COLUMN labels_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE pubsub_subscriptions ADD COLUMN filter TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE pubsub_subscriptions ADD COLUMN dead_letter_topic TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE pubsub_subscriptions ADD COLUMN max_delivery_attempts INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE pubsub_subscriptions ADD COLUMN enable_exactly_once_delivery INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE pubsub_messages ADD COLUMN delivery_attempts INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE secrets ADD COLUMN labels_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE secrets ADD COLUMN annotations_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE secrets ADD COLUMN replication_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE secrets ADD COLUMN cmek_kms_key_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE secrets ADD COLUMN rotation_period TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE secrets ADD COLUMN next_rotation_time TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE secrets ADD COLUMN topics_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE run_services ADD COLUMN traffic_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE scheduler_jobs ADD COLUMN oidc_audience TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE scheduler_jobs ADD COLUMN next_run_time TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE cloud_tasks_queues ADD COLUMN rate_limits_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE cloud_tasks_queues ADD COLUMN retry_config_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE cloud_tasks_queues ADD COLUMN app_engine_routing_override_json TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE cloud_tasks ADD COLUMN app_engine_http_request_json TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE cloud_tasks ADD COLUMN response_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE service_accounts ADD COLUMN deleted_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE kms_keys ADD COLUMN labels_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE projects ADD COLUMN labels_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE spanner_databases ADD COLUMN ddl_statements_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE appengine_services ADD COLUMN split_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE appengine_services ADD COLUMN shard_by TEXT NOT NULL DEFAULT 'UNSPECIFIED'`,
		`ALTER TABLE appengine_services ADD COLUMN migrate_traffic INTEGER NOT NULL DEFAULT 0`,
	}
	for _, stmt := range alters {
		if _, err := s.db.Exec(stmt); err != nil {
			// SQLite duplicate-column errors are expected on fresh schemas.
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migrate column: %w", err)
			}
		}
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS gcs_upload_sessions (
  upload_id TEXT PRIMARY KEY,
  bucket TEXT NOT NULL,
  name TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT 'application/octet-stream',
  created_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("migrate upload sessions: %w", err)
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS pubsub_snapshots (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  topic TEXT NOT NULL,
  subscription TEXT NOT NULL DEFAULT '',
  labels_json TEXT NOT NULL DEFAULT '{}',
  expire_time TEXT NOT NULL,
  created_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("migrate pubsub snapshots: %w", err)
	}
	return nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DataRoot returns the configured data directory.
func (s *Store) DataRoot() string { return s.dataRoot }

// Seal encrypts plaintext with the store master key.
func (s *Store) Seal(plaintext []byte) ([]byte, error) {
	return Seal(s.master, plaintext)
}

// Unseal decrypts ciphertext with the store master key.
func (s *Store) Unseal(ciphertext []byte) ([]byte, error) {
	return Unseal(s.master, ciphertext)
}

// EnsureRoot seeds the default project and binds root SA as roles/owner on the project.
// Root access tokens are held in process memory from config (not persisted).
func (s *Store) EnsureRoot(projectID, rootSAEmail string) error {
	if projectID == "" {
		return fmt.Errorf("project id required")
	}
	if rootSAEmail == "" {
		return fmt.Errorf("root service account required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO projects (id, display_name, state, created_at) VALUES (?, ?, 'ACTIVE', ?)`,
		projectID, projectID, now,
	); err != nil {
		return fmt.Errorf("seed project: %w", err)
	}

	uniqueID := fmt.Sprintf("1%019d", time.Now().UnixNano()%1_000_000_000_000_000_000)
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO service_accounts (project_id, email, unique_id, display_name, disabled, created_at)
		 VALUES (?, ?, ?, 'Noctaxris-GCP root', 0, ?)`,
		projectID, rootSAEmail, uniqueID, now,
	); err != nil {
		return fmt.Errorf("seed root service account: %w", err)
	}

	resource := "projects/" + projectID
	pol := authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/owner",
			Members: []string{"serviceAccount:" + rootSAEmail},
		}},
		Etag: "ACAB",
	}
	raw, err := json.Marshal(pol)
	if err != nil {
		return err
	}
	var existing string
	qerr := tx.QueryRow(`SELECT policy_json FROM iam_policies WHERE resource = ?`, resource).Scan(&existing)
	if qerr == sql.ErrNoRows {
		if _, err := tx.Exec(
			`INSERT INTO iam_policies (resource, etag, policy_json) VALUES (?, ?, ?)`,
			resource, pol.Etag, string(raw),
		); err != nil {
			return fmt.Errorf("seed iam policy: %w", err)
		}
	} else if qerr != nil {
		return qerr
	}

	wave1 := []string{
		"cloudresourcemanager.googleapis.com",
		"iam.googleapis.com",
		"serviceusage.googleapis.com",
		"storage.googleapis.com",
		"pubsub.googleapis.com",
		"secretmanager.googleapis.com",
		"firestore.googleapis.com",
		"cloudkms.googleapis.com",
		"logging.googleapis.com",
		"run.googleapis.com",
		"cloudfunctions.googleapis.com",
		"cloudscheduler.googleapis.com",
		"cloudtasks.googleapis.com",
		"bigquery.googleapis.com",
		"identitytoolkit.googleapis.com",
		"monitoring.googleapis.com",
		"datastore.googleapis.com",
		"eventarc.googleapis.com",
		"appengine.googleapis.com",
		"artifactregistry.googleapis.com",
		"cloudbuild.googleapis.com",
		"workflows.googleapis.com",
		"spanner.googleapis.com",
		"dns.googleapis.com",
		"dataflow.googleapis.com",
		"compute.googleapis.com",
		"bigtableadmin.googleapis.com",
		"redis.googleapis.com",
		"certificatemanager.googleapis.com",
		"file.googleapis.com",
		"aiplatform.googleapis.com",
	}
	for _, svc := range wave1 {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO service_usage (project_id, service_name, state) VALUES (?, ?, 'ENABLED')`,
			projectID, svc,
		); err != nil {
			return fmt.Errorf("seed service usage %s: %w", svc, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// Lab org is outside the project seed transaction so concurrent
	// schema work can own the organizations table without fighting this tx.
	return s.EnsureLabOrganization()
}

// LookupAccessToken resolves a token hash to a principal when not expired.
func (s *Store) LookupAccessToken(tokenHash string, now time.Time) (principalEmail string, ok bool, err error) {
	var email, expiresAt string
	err = s.db.QueryRow(
		`SELECT principal_email, expires_at FROM access_tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&email, &expiresAt)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	exp, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		exp, err = time.Parse(time.RFC3339, expiresAt)
		if err != nil {
			return "", false, fmt.Errorf("parse token expiry: %w", err)
		}
	}
	if !now.Before(exp) {
		return "", false, nil
	}
	return email, true, nil
}

// GetIAMPolicyJSON returns the stored IAM policy document for resource.
func (s *Store) GetIAMPolicyJSON(resource string) ([]byte, bool, error) {
	var raw string
	err := s.db.QueryRow(`SELECT policy_json FROM iam_policies WHERE resource = ?`, resource).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return []byte(raw), true, nil
}

// PutAccessToken registers a hashed bearer token for principalEmail until expiresAt.
func (s *Store) PutAccessToken(tokenHash, principalEmail string, expiresAt time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO access_tokens (token_hash, principal_email, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		tokenHash, principalEmail, expiresAt.UTC().Format(time.RFC3339Nano), now,
	)
	return err
}
