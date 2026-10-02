package resourcemanager_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/iam"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/resourcemanager"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// Glass Kiln intersection shape: custom Cloud Build create on the workload
// project, actAs-only on the builder SA resource, and workerPoolUser on a
// separate pool-host project. testIamPermissions must report each grant only
// on the resource where it is bound.
func TestTestIamPermissionsCustomRoleIntersection(t *testing.T) {
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

	const (
		project    = "noctaxris-gcp-local"
		poolHost   = "cb-host"
		rootEmail  = "root@noctaxris-gcp-local.iam.gserviceaccount.com"
		ciEmail    = "ci-runner@noctaxris-gcp-local.iam.gserviceaccount.com"
		builderID  = "builder"
		buildsRole = "projects/noctaxris-gcp-local/roles/buildCreator"
		actAsRole  = "projects/noctaxris-gcp-local/roles/actAsOnly"
	)
	builderEmail := builderID + "@" + project + ".iam.gserviceaccount.com"

	if err := st.EnsureRoot(project, rootEmail); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateProject(store.Project{
		ID: poolHost, DisplayName: "Worker pool host", State: "ACTIVE",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: project, Email: ciEmail, UniqueID: "ci-runner", DisplayName: "CI",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: project, Email: builderEmail, UniqueID: builderID, DisplayName: "Builder",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.CreateCustomRole(project, "buildCreator", "Build Creator", "", "GA", []string{
		"cloudbuild.builds.create",
		"cloudbuild.builds.list",
		"cloudbuild.builds.get",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCustomRole(project, "actAsOnly", "Act As Only", "", "GA", []string{
		"iam.serviceAccounts.actAs",
	}); err != nil {
		t.Fatal(err)
	}

	ciMember := "serviceAccount:" + ciEmail
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Etag: "ACAB",
		Bindings: []authz.Binding{{
			Role: buildsRole, Members: []string{ciMember},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+project+"/serviceAccounts/"+builderEmail, authz.Policy{
		Etag: "ACAB",
		Bindings: []authz.Binding{{
			Role: actAsRole, Members: []string{ciMember},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+poolHost, authz.Policy{
		Etag: "ACAB",
		Bindings: []authz.Binding{{
			Role: "roles/cloudbuild.workerPoolUser", Members: []string{ciMember},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	var who authn.Principal
	setWho := func(email string, isRoot bool) {
		who = authn.Principal{Email: email, IsRoot: isRoot}
	}
	eval := &authz.Evaluator{Policies: st, Roles: st}
	mux := http.NewServeMux()
	(&resourcemanager.Handler{
		Store: st,
		Authz: eval,
		Principal: func(*http.Request) (authn.Principal, bool) {
			if who.Email == "" && !who.IsRoot {
				return authn.Principal{}, false
			}
			return who, true
		},
	}).Mount(mux)
	(&iam.Handler{
		Store: st,
		Authz: eval,
		Principal: func(*http.Request) (authn.Principal, bool) {
			if who.Email == "" && !who.IsRoot {
				return authn.Principal{}, false
			}
			return who, true
		},
	}).Mount(mux)

	postJSON := func(path, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	permsOf := func(body map[string]any) []string {
		t.Helper()
		raw, _ := body["permissions"].([]any)
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	mustContain := func(got []string, want string) {
		t.Helper()
		if !slices.Contains(got, want) {
			t.Fatalf("expected %q in %v", want, got)
		}
	}
	mustNotContain := func(got []string, deny string) {
		t.Helper()
		if slices.Contains(got, deny) {
			t.Fatalf("did not expect %q in %v", deny, got)
		}
	}

	setWho(ciEmail, false)

	code, body := postJSON("/v3/projects/"+project+":testIamPermissions",
		`{"permissions":["cloudbuild.builds.create","cloudbuild.builds.list","iam.serviceAccounts.actAs","cloudbuild.workerpools.use"]}`)
	if code != http.StatusOK {
		t.Fatalf("project testIamPermissions: %d %#v", code, body)
	}
	got := permsOf(body)
	mustContain(got, "cloudbuild.builds.create")
	mustContain(got, "cloudbuild.builds.list")
	mustNotContain(got, "iam.serviceAccounts.actAs")
	mustNotContain(got, "cloudbuild.workerpools.use")

	code, body = postJSON("/v1/projects/"+project+"/serviceAccounts/"+builderEmail+":testIamPermissions",
		`{"permissions":["iam.serviceAccounts.actAs","cloudbuild.builds.create","iam.serviceAccounts.getAccessToken"]}`)
	if code != http.StatusOK {
		t.Fatalf("SA testIamPermissions: %d %#v", code, body)
	}
	got = permsOf(body)
	mustContain(got, "iam.serviceAccounts.actAs")
	// Project bindings inherit onto the SA resource, so builds.create may also
	// appear here. actAs must not be visible when tested on the project alone.
	mustNotContain(got, "iam.serviceAccounts.getAccessToken")

	code, body = postJSON("/v3/projects/"+poolHost+":testIamPermissions",
		`{"permissions":["cloudbuild.workerpools.use","cloudbuild.workerpools.get","cloudbuild.builds.create"]}`)
	if code != http.StatusOK {
		t.Fatalf("pool-host testIamPermissions: %d %#v", code, body)
	}
	got = permsOf(body)
	mustContain(got, "cloudbuild.workerpools.use")
	mustContain(got, "cloudbuild.workerpools.get")
	mustNotContain(got, "cloudbuild.builds.create")
}
