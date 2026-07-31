package store_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"

	_ "modernc.org/sqlite"
)

func TestWIFProviderAllowedAudiencesCreateGetUpdate(t *testing.T) {
	st := openTestStore(t)
	pool, err := st.CreateWIFPool("noctaxris-gcp-local", "global", "aud-pool", "Aud", "", false)
	if err != nil {
		t.Fatal(err)
	}
	audJSON := `["https://app.example/aud"," https://app.example/aud ","https://other"]`
	prov, err := st.CreateWIFProvider(pool.Name, "oidc-aud", "OIDC", "", "https://issuer.example", "{}", audJSON, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(prov.AllowedAudiences) != 2 {
		t.Fatalf("create audiences = %#v", prov.AllowedAudiences)
	}
	if prov.AllowedAudiences[0] != "https://app.example/aud" || prov.AllowedAudiences[1] != "https://other" {
		t.Fatalf("deduped audiences = %#v", prov.AllowedAudiences)
	}

	got, ok, err := st.GetWIFProvider(prov.Name)
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	if len(got.AllowedAudiences) != 2 {
		t.Fatalf("get audiences = %#v", got.AllowedAudiences)
	}

	updated, ok, err := st.UpdateWIFProvider(prov.Name, "", "", "", "", `["https://patched"]`, false,
		false, false, false, false, true, false)
	if err != nil || !ok {
		t.Fatalf("update ok=%v err=%v", ok, err)
	}
	if len(updated.AllowedAudiences) != 1 || updated.AllowedAudiences[0] != "https://patched" {
		t.Fatalf("patched = %#v", updated.AllowedAudiences)
	}
}

func TestWIFProviderAllowedAudiencesPersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(dir, "data")
	st, err := store.Open(dataRoot, key)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := st.CreateWIFPool("noctaxris-gcp-local", "global", "reopen-pool", "R", "", false)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := st.CreateWIFProvider(pool.Name, "oidc-re", "OIDC", "", "https://example.com", "{}", `["https://persist"]`, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	st2, err := store.Open(dataRoot, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	got, ok, err := st2.GetWIFProvider(prov.Name)
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	if len(got.AllowedAudiences) != 1 || got.AllowedAudiences[0] != "https://persist" {
		t.Fatalf("persisted audiences = %#v", got.AllowedAudiences)
	}
}

func TestWIFProviderAllowedAudiencesMigrateFromLegacySchema(t *testing.T) {
	dir := t.TempDir()
	dataRoot := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataRoot, "state.db")
	dsn := "file:" + filepath.ToSlash(dbPath) + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE wif_pools (
  name TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  location TEXT NOT NULL,
  pool_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  disabled INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE wif_providers (
  name TEXT PRIMARY KEY,
  pool_name TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  disabled INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  attribute_map_json TEXT NOT NULL DEFAULT '{}',
  issuer_uri TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
INSERT INTO wif_pools VALUES (
  'projects/noctaxris-gcp-local/locations/global/workloadIdentityPools/legacy-pool',
  'noctaxris-gcp-local','global','legacy-pool','','',0,'ACTIVE','t','t');
INSERT INTO wif_providers VALUES (
  'projects/noctaxris-gcp-local/locations/global/workloadIdentityPools/legacy-pool/providers/legacy-prov',
  'projects/noctaxris-gcp-local/locations/global/workloadIdentityPools/legacy-pool',
  'legacy-prov','','',0,'ACTIVE','{}','https://issuer.example','t','t');
`)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()

	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dataRoot, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	name := "projects/noctaxris-gcp-local/locations/global/workloadIdentityPools/legacy-pool/providers/legacy-prov"
	got, ok, err := st.GetWIFProvider(name)
	if err != nil || !ok {
		t.Fatalf("get after migrate ok=%v err=%v", ok, err)
	}
	if len(got.AllowedAudiences) != 0 {
		t.Fatalf("legacy default audiences = %#v", got.AllowedAudiences)
	}
	updated, ok, err := st.UpdateWIFProvider(name, "", "", "", "", `["https://migrated"]`, false,
		false, false, false, false, true, false)
	if err != nil || !ok {
		t.Fatalf("update after migrate ok=%v err=%v", ok, err)
	}
	if len(updated.AllowedAudiences) != 1 || updated.AllowedAudiences[0] != "https://migrated" {
		t.Fatalf("write after migrate = %#v", updated.AllowedAudiences)
	}
}
