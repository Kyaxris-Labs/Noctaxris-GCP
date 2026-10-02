package cloudbuild

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestRequireBuildSASecretAccessProjectGrant(t *testing.T) {
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

	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	buildSA := "builder@" + project + ".iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: project, Email: buildSA, UniqueID: "builder", DisplayName: "builder",
	}); err != nil {
		t.Fatal(err)
	}
	secretName := "projects/" + project + "/secrets/build-token"
	if _, _, err := st.CreateSecret(secretName, project); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSecretVersion(secretName, []byte("tok")); err != nil {
		t.Fatal(err)
	}

	r := &EngineRunner{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	if err := r.requireBuildSASecretAccess(buildSA, secretName); err == nil {
		t.Fatal("expected deny without secret or project grant")
	}

	// Project-level secret accessor must allow via EvaluateAny (not secret-only Evaluate).
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/secretmanager.secretAccessor",
			Members: []string{"serviceAccount:" + buildSA},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.requireBuildSASecretAccess(buildSA, secretName); err != nil {
		t.Fatalf("project grant should allow secretEnv access: %v", err)
	}
}

func TestRequireBuildSASecretAccessSecretGrant(t *testing.T) {
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

	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	buildSA := "builder@" + project + ".iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: project, Email: buildSA, UniqueID: "builder", DisplayName: "builder",
	}); err != nil {
		t.Fatal(err)
	}
	secretName := "projects/" + project + "/secrets/step-secret"
	if _, _, err := st.CreateSecret(secretName, project); err != nil {
		t.Fatal(err)
	}
	r := &EngineRunner{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	if err := st.PutIAMPolicyJSON(secretName, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/secretmanager.secretAccessor",
			Members: []string{"serviceAccount:" + buildSA},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.requireBuildSASecretAccess(buildSA, secretName); err != nil {
		t.Fatalf("secret grant should allow secretEnv access: %v", err)
	}
}

func TestRequireBuildSASecretAccessNilAuthzFailClosed(t *testing.T) {
	r := &EngineRunner{Authz: nil}
	if err := r.requireBuildSASecretAccess("sa@example.iam.gserviceaccount.com", "projects/p/secrets/s"); err == nil {
		t.Fatal("nil Authz must deny secretEnv access")
	}
}
