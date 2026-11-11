package store_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestEnsureRootWritesLabOrgParent(t *testing.T) {
	st := openTestStore(t)
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	p, ok, err := st.GetProject("noctaxris-gcp-local")
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	if p.Parent != store.DefaultOrganizationName {
		t.Fatalf("parent = %q", p.Parent)
	}
	parent, found, err := st.CRMParent("projects/noctaxris-gcp-local")
	if err != nil || !found || parent != store.DefaultOrganizationName {
		t.Fatalf("CRMParent = %q found=%v err=%v", parent, found, err)
	}
}

func TestCreateProjectPersistsAuthorizedParent(t *testing.T) {
	st := openTestStore(t)
	p, created, err := st.CreateProject(store.Project{
		ID: "under-folder", DisplayName: "Under Folder", Parent: "folders/team-a",
	})
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if p.Parent != "folders/team-a" {
		t.Fatalf("parent = %q", p.Parent)
	}
	parent, found, err := st.CRMParent("projects/under-folder")
	if err != nil || !found || parent != "folders/team-a" {
		t.Fatalf("CRMParent = %q found=%v err=%v", parent, found, err)
	}

	omitted, created, err := st.CreateProject(store.Project{ID: "omitted-parent"})
	if err != nil || !created {
		t.Fatalf("omitted created=%v err=%v", created, err)
	}
	if omitted.Parent != store.DefaultOrganizationName {
		t.Fatalf("omitted parent = %q", omitted.Parent)
	}
}

func TestProjectParentBackfillOnce(t *testing.T) {
	dir := t.TempDir()
	dataRoot := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataRoot, "state.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'ACTIVE',
  labels_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO projects (id, display_name, state, created_at) VALUES ('legacy-proj', 'Legacy', 'ACTIVE', '2020-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dataRoot, key)
	if err != nil {
		t.Fatal(err)
	}
	legacy, ok, err := st.GetProject("legacy-proj")
	if err != nil || !ok {
		t.Fatalf("legacy ok=%v err=%v", ok, err)
	}
	if legacy.Parent != store.DefaultOrganizationName {
		t.Fatalf("backfill parent = %q", legacy.Parent)
	}

	const rootSA = "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	const rootToken = "root-token-not-minted"
	out, err := st.SeedStandaloneProject("solo-proj", rootSA, rootToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st2, err := store.Open(dataRoot, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	again, ok, err := st2.GetProject("legacy-proj")
	if err != nil || !ok || again.Parent != store.DefaultOrganizationName {
		t.Fatalf("legacy after reopen parent=%q ok=%v err=%v", again.Parent, ok, err)
	}
	solo, ok, err := st2.GetProject(out.ProjectID)
	if err != nil || !ok {
		t.Fatalf("solo ok=%v err=%v", ok, err)
	}
	if solo.Parent != "" || strings.HasPrefix(solo.Parent, "folders/") {
		t.Fatalf("standalone parent = %q", solo.Parent)
	}
	parent, found, err := st2.CRMParent("projects/" + out.ProjectID)
	if err != nil || found || parent != "" {
		t.Fatalf("empty CRMParent = %q found=%v err=%v", parent, found, err)
	}
}

func TestSeedStandaloneProjectOwnerAndIdempotent(t *testing.T) {
	st := openTestStore(t)
	const rootSA = "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	const rootToken = "exact-root-access-token"
	if err := st.EnsureRoot("noctaxris-gcp-local", rootSA); err != nil {
		t.Fatal(err)
	}

	out, err := st.SeedStandaloneProject("solo-lab", rootSA, rootToken)
	if err != nil {
		t.Fatal(err)
	}
	if out.AccessToken == "" || out.AccessToken == rootToken {
		t.Fatalf("token must be a new non-root bearer")
	}
	if out.ServiceAccountEmail == rootSA || !strings.HasPrefix(out.ServiceAccountEmail, "standalone@") {
		t.Fatalf("email = %s", out.ServiceAccountEmail)
	}
	p, ok, err := st.GetProject(out.ProjectID)
	if err != nil || !ok || p.Parent != "" {
		t.Fatalf("project parent=%q ok=%v err=%v", p.Parent, ok, err)
	}
	_, found, err := st.CRMParent("projects/" + out.ProjectID)
	if err != nil || found {
		t.Fatalf("CRMParent found=%v err=%v", found, err)
	}

	raw, ok, err := st.GetIAMPolicyJSON("projects/" + out.ProjectID)
	if err != nil || !ok {
		t.Fatalf("policy ok=%v err=%v", ok, err)
	}
	if strings.Contains(string(raw), rootSA) || strings.Contains(string(raw), rootToken) {
		t.Fatalf("policy leaked root material: %s", raw)
	}
	var pol authz.Policy
	if err := json.Unmarshal(raw, &pol); err != nil {
		t.Fatal(err)
	}
	if len(pol.Bindings) != 1 || pol.Bindings[0].Role != "roles/owner" {
		t.Fatalf("bindings = %#v", pol.Bindings)
	}
	if len(pol.Bindings[0].Members) != 1 || pol.Bindings[0].Members[0] != "serviceAccount:"+out.ServiceAccountEmail {
		t.Fatalf("members = %#v", pol.Bindings[0].Members)
	}

	usage, err := st.ListServiceUsage(out.ProjectID, "ENABLED")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{}{}
	for _, name := range store.LabSeedServiceNames() {
		want[name] = struct{}{}
	}
	if len(usage) != len(want) {
		t.Fatalf("service usage len=%d want=%d", len(usage), len(want))
	}
	for _, row := range usage {
		if _, ok := want[row.ServiceName]; !ok {
			t.Fatalf("unexpected service %s", row.ServiceName)
		}
	}

	auth := &authn.Authenticator{
		RootServiceAccount: rootSA,
		RootAccessToken:    rootToken,
		Tokens:             st,
	}
	principal, err := auth.AuthenticateToken(out.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if principal.IsRoot || principal.Email == rootSA || principal.Email != out.ServiceAccountEmail {
		t.Fatalf("principal = %#v", principal)
	}

	second, err := st.SeedStandaloneProject("solo-lab", rootSA, rootToken)
	if err != nil {
		t.Fatal(err)
	}
	if second.AccessToken != out.AccessToken || second.ServiceAccountEmail != out.ServiceAccountEmail {
		t.Fatalf("reseed rotated token or email: %#v", second)
	}

	ev := &authz.Evaluator{Policies: st, Parents: st}
	orgEmail := "orgwide@example.com"
	if err := st.PutIAMPolicyJSON(store.DefaultOrganizationName, authz.Policy{
		Etag: "ACAB",
		Bindings: []authz.Binding{{
			Role:    "roles/owner",
			Members: []string{"serviceAccount:" + orgEmail},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	allowed, err := ev.Evaluate(orgEmail, false, "resourcemanager.projects.get", "projects/noctaxris-gcp-local")
	if err != nil || !allowed {
		t.Fatalf("org IAM should grant on lab parent project: allowed=%v err=%v", allowed, err)
	}
	allowed, err = ev.Evaluate(orgEmail, false, "resourcemanager.projects.get", "projects/"+out.ProjectID)
	if err != nil || allowed {
		t.Fatalf("org IAM must not grant on empty parent: allowed=%v err=%v", allowed, err)
	}
	allowed, err = ev.Evaluate(out.ServiceAccountEmail, false, "resourcemanager.projects.get", "projects/noctaxris-gcp-local")
	if err != nil || allowed {
		t.Fatalf("standalone owner must not get the default project: allowed=%v err=%v", allowed, err)
	}
	allowed, err = ev.Evaluate(out.ServiceAccountEmail, false, "resourcemanager.organizations.setIamPolicy", store.DefaultOrganizationName)
	if err != nil || allowed {
		t.Fatalf("standalone owner must not set org IAM: allowed=%v err=%v", allowed, err)
	}
	_, err = st.SeedStandaloneProject("noctaxris-gcp-local", rootSA, rootToken)
	if err == nil {
		t.Fatal("seeding the default project id must not succeed")
	}
}
