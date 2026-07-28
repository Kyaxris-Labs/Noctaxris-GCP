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
	list, err := st.ListServiceUsage("noctaxris-gcp-local")
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
}
