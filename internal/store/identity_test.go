package store_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func openIdentityStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestIdentityProjectAndPolicy(t *testing.T) {
	st := openIdentityStore(t)
	p, ok, err := st.GetProject("noctaxris-gcp-local")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || p.State != "ACTIVE" {
		t.Fatalf("project = %#v ok=%v", p, ok)
	}

	pol := authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/viewer",
			Members: []string{"serviceAccount:viewer@noctaxris-gcp-local.iam.gserviceaccount.com"},
		}},
		Etag: "etag-1",
	}
	if err := st.PutIAMPolicyJSON("projects/noctaxris-gcp-local", pol); err != nil {
		t.Fatal(err)
	}
	raw, ok, err := st.GetIAMPolicyJSON("projects/noctaxris-gcp-local")
	if err != nil || !ok {
		t.Fatalf("get policy ok=%v err=%v", ok, err)
	}
	if len(raw) == 0 {
		t.Fatal("empty policy")
	}
}

func TestIdentityServiceAccountAndKey(t *testing.T) {
	st := openIdentityStore(t)
	email := "app@noctaxris-gcp-local.iam.gserviceaccount.com"
	err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID:   "noctaxris-gcp-local",
		Email:       email,
		UniqueID:    "1001",
		DisplayName: "App",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: "noctaxris-gcp-local",
		Email:     email,
		UniqueID:  "1002",
	}); !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("duplicate create err = %v", err)
	}

	sa, ok, err := st.GetServiceAccountInProject("noctaxris-gcp-local", email)
	if err != nil || !ok || sa.DisplayName != "App" {
		t.Fatalf("get sa = %#v ok=%v err=%v", sa, ok, err)
	}

	plain := []byte(`{"type":"service_account","token":"sa-token-1"}`)
	sealed, err := st.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	keyName := "projects/noctaxris-gcp-local/serviceAccounts/" + email + "/keys/abc"
	now := time.Now().UTC().Format(time.RFC3339)
	if err := st.CreateServiceAccountKey(store.ServiceAccountKey{
		Name:            keyName,
		SAEmail:         email,
		PrivateKeyData:  sealed,
		ValidAfterTime:  now,
		ValidBeforeTime: "9999-12-31T23:59:59Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutAccessToken(authn.HashToken("sa-token-1"), email, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	keys, err := st.ListServiceAccountKeys(email)
	if err != nil || len(keys) != 1 {
		t.Fatalf("keys = %d err=%v", len(keys), err)
	}
	out, err := st.Unseal(keys[0].PrivateKeyData)
	if err != nil || string(out) != string(plain) {
		t.Fatalf("unseal = %q err=%v", out, err)
	}
}

func TestIdentityServiceUsage(t *testing.T) {
	st := openIdentityStore(t)
	list, err := st.ListServiceUsage("noctaxris-gcp-local", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 3 {
		t.Fatalf("expected seeded services, got %d", len(list))
	}
	if err := st.SetServiceUsageState("noctaxris-gcp-local", "storage.googleapis.com", "DISABLED"); err != nil {
		t.Fatal(err)
	}
	u, ok, err := st.GetServiceUsage("noctaxris-gcp-local", "storage.googleapis.com")
	if err != nil || !ok || u.State != "DISABLED" {
		t.Fatalf("usage = %#v ok=%v err=%v", u, ok, err)
	}
	enabled, err := st.ListServiceUsage("noctaxris-gcp-local", "ENABLED")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range enabled {
		if row.State != "ENABLED" {
			t.Fatalf("filter ENABLED got %#v", row)
		}
		if row.ServiceName == "storage.googleapis.com" {
			t.Fatal("disabled service should not appear in ENABLED filter")
		}
	}
	if err := st.BatchEnableServiceUsage("noctaxris-gcp-local", []string{"storage.googleapis.com", "bigquery.googleapis.com"}); err != nil {
		t.Fatal(err)
	}
	u, ok, err = st.GetServiceUsage("noctaxris-gcp-local", "storage.googleapis.com")
	if err != nil || !ok || u.State != "ENABLED" {
		t.Fatalf("batch enable storage = %#v ok=%v err=%v", u, ok, err)
	}
	if err := st.BatchDisableServiceUsage("noctaxris-gcp-local", []string{"storage.googleapis.com"}); err != nil {
		t.Fatal(err)
	}
	u, ok, err = st.GetServiceUsage("noctaxris-gcp-local", "storage.googleapis.com")
	if err != nil || !ok || u.State != "DISABLED" {
		t.Fatalf("batch disable storage = %#v ok=%v err=%v", u, ok, err)
	}
}

func TestIdentityServiceAccountDisableAndPatch(t *testing.T) {
	st := openIdentityStore(t)
	email := "ops@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID:   "noctaxris-gcp-local",
		Email:       email,
		UniqueID:    "2001",
		DisplayName: "Ops",
	}); err != nil {
		t.Fatal(err)
	}
	sa, ok, err := st.SetServiceAccountDisabled(email, true)
	if err != nil || !ok || !sa.Disabled {
		t.Fatalf("disable = %#v ok=%v err=%v", sa, ok, err)
	}
	sa, ok, err = st.UpdateServiceAccountDisplayName(email, "Ops Renamed")
	if err != nil || !ok || sa.DisplayName != "Ops Renamed" {
		t.Fatalf("patch = %#v ok=%v err=%v", sa, ok, err)
	}
	p, ok, err := st.UpdateProjectDisplayName("noctaxris-gcp-local", "Lab Project")
	if err != nil || !ok || p.DisplayName != "Lab Project" {
		t.Fatalf("project patch = %#v ok=%v err=%v", p, ok, err)
	}
}

func TestIdentitySoftDeleteUndeleteAndKeyPagination(t *testing.T) {
	st := openIdentityStore(t)
	email := "soft@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: "noctaxris-gcp-local",
		Email:     email,
		UniqueID:  "3001",
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		sealed, err := st.Seal([]byte("k"))
		if err != nil {
			t.Fatal(err)
		}
		name := "projects/noctaxris-gcp-local/serviceAccounts/" + email + "/keys/k" + string(rune('a'+i))
		if err := st.CreateServiceAccountKey(store.ServiceAccountKey{
			Name:           name,
			SAEmail:        email,
			PrivateKeyData: sealed,
			ValidAfterTime: "2020-01-01T00:00:00Z",
			ValidBeforeTime: "9999-12-31T23:59:59Z",
		}); err != nil {
			t.Fatal(err)
		}
	}
	page1, next, err := st.ListServiceAccountKeysPage(email, 2, "")
	if err != nil || len(page1) != 2 || next == "" {
		t.Fatalf("page1 len=%d next=%q err=%v", len(page1), next, err)
	}
	page2, next2, err := st.ListServiceAccountKeysPage(email, 2, next)
	if err != nil || len(page2) != 1 || next2 != "" {
		t.Fatalf("page2 len=%d next=%q err=%v", len(page2), next2, err)
	}

	ok, err := st.DeleteServiceAccount(email)
	if err != nil || !ok {
		t.Fatalf("soft delete ok=%v err=%v", ok, err)
	}
	if _, found, err := st.GetServiceAccount(email); err != nil || found {
		t.Fatalf("active get after delete found=%v err=%v", found, err)
	}
	del, found, err := st.GetDeletedServiceAccountInProject("noctaxris-gcp-local", email)
	if err != nil || !found || del.DeletedAt == "" {
		t.Fatalf("deleted row = %#v found=%v err=%v", del, found, err)
	}
	restored, found, err := st.UndeleteServiceAccount(email)
	if err != nil || !found || restored.DeletedAt != "" {
		t.Fatalf("undelete = %#v found=%v err=%v", restored, found, err)
	}
}

func TestIdentityListSearchProjectsAndBatchGet(t *testing.T) {
	st := openIdentityStore(t)
	list, err := st.ListProjects()
	if err != nil || len(list) < 1 {
		t.Fatalf("list projects = %d err=%v", len(list), err)
	}
	found, err := st.SearchProjects("noctaxris")
	if err != nil || len(found) < 1 {
		t.Fatalf("search = %d err=%v", len(found), err)
	}
	batch, err := st.BatchGetServiceUsage("noctaxris-gcp-local", []string{"storage.googleapis.com", "missing.googleapis.com"})
	if err != nil || len(batch) != 2 {
		t.Fatalf("batchGet = %#v err=%v", batch, err)
	}
	if batch[0].State != "ENABLED" || batch[1].State != "DISABLED" {
		t.Fatalf("batch states = %#v", batch)
	}
	enabled, err := st.IsServiceEnabled("noctaxris-gcp-local", "iam.googleapis.com")
	if err != nil || !enabled {
		t.Fatalf("iam enabled = %v err=%v", enabled, err)
	}
}
