package authz_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestDatastoreUserGrantsEntitiesWrite(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project := "noctaxris-gcp-local"
	email := "ds-user@example.com"
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role: "roles/datastore.user", Members: []string{"serviceAccount:" + email},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	e := &authz.Evaluator{Policies: st}
	for _, perm := range []string{
		"datastore.entities.create",
		"datastore.entities.update",
		"datastore.entities.delete",
		"datastore.entities.write",
	} {
		ok, err := e.Evaluate(email, false, perm, "projects/"+project)
		if err != nil || !ok {
			t.Fatalf("datastore.user should grant %s: ok=%v err=%v", perm, ok, err)
		}
	}
	ok, err := e.Evaluate(email, false, "datastore.databases.delete", "projects/"+project)
	if err != nil || ok {
		t.Fatalf("datastore.user must not delete databases: ok=%v err=%v", ok, err)
	}
}
