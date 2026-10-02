package artifactregistry

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestRequireAnyAllowAndDeny(t *testing.T) {
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
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}

	rootP := authn.Principal{Email: root, IsRoot: true}
	if err := svc.requireAny(rootP, project, "artifactregistry.repositories.get", "artifactregistry.repositories.list"); err != nil {
		t.Fatalf("root requireAny: %v", err)
	}

	nobody := authn.Principal{Email: "nobody@example.com", IsRoot: false}
	if err := svc.requireAny(nobody, project, "artifactregistry.repositories.get"); err == nil {
		t.Fatal("expected deny")
	}

	// First permission denied, second allowed via root.
	if err := svc.requireAny(rootP, project,
		"artifactregistry.repositories.doesNotExist", "artifactregistry.repositories.get"); err != nil {
		t.Fatalf("root requireAny second perm: %v", err)
	}
}
