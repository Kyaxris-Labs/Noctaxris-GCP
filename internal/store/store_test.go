package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestSealRoundTrip(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("sensitive-lab-secret")
	sealed, err := store.Seal(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	out, err := store.Unseal(key, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(plain) {
		t.Fatalf("got %q want %q", out, plain)
	}
}

func TestOpenAndEnsureRoot(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "secrets", "master.key")
	key, err := store.LoadOrCreateMasterKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(dir, "data")
	st, err := store.Open(dataRoot, key)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	rootSA := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureRoot("noctaxris-gcp-local", rootSA); err != nil {
		t.Fatal(err)
	}
	// Idempotent.
	if err := st.EnsureRoot("noctaxris-gcp-local", rootSA); err != nil {
		t.Fatal(err)
	}

	raw, ok, err := st.GetIAMPolicyJSON("projects/noctaxris-gcp-local")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected project IAM policy")
	}
	ev := &authz.Evaluator{Policies: st}
	allowed, err := ev.Evaluate(rootSA, false, "storage.buckets.create", "projects/noctaxris-gcp-local")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatalf("owner binding should allow; policy=%s", raw)
	}

	token := "participant-token-1"
	if err := st.PutAccessToken(authn.HashToken(token), "sa@noctaxris-gcp-local.iam.gserviceaccount.com", time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	email, ok, err := st.LookupAccessToken(authn.HashToken(token), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !ok || email != "sa@noctaxris-gcp-local.iam.gserviceaccount.com" {
		t.Fatalf("lookup = %q ok=%v", email, ok)
	}
}

func TestResolveMasterKeyPathDefaultsOutsideDataRoot(t *testing.T) {
	t.Setenv(store.EnvAllowMasterKeyInDataRoot, "")
	dataRoot := filepath.Join(t.TempDir(), "noctaxris-gcp")
	path, err := store.ResolveMasterKeyPath("", dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := store.DefaultMasterKeyPath(dataRoot)
	if path != want {
		t.Fatalf("path = %q want %q", path, want)
	}
}

// openTestStore is shared by store package tests (GCS/Pub/Sub/Secrets/Firestore/KMS/Logging).
func openTestStore(t *testing.T) *store.Store {
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
	return st
}
