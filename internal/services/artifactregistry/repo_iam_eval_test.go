package artifactregistry_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/artifactregistry"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestArtifactRegistryRepoIAMEvaluate(t *testing.T) {
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

	mux := http.NewServeMux()
	reader := "reader@example.com"
	cur := authn.Principal{Email: root, IsRoot: true}
	svc := &artifactregistry.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return cur, true })

	loc := artifactregistry.DefaultLocation
	base := "/v1/projects/" + project + "/locations/" + loc + "/repositories"
	req := httptest.NewRequest(http.MethodPost, base+"?repositoryId=bound",
		bytes.NewReader([]byte(`{"format":"DOCKER"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	repo := "projects/" + project + "/locations/" + loc + "/repositories/bound"
	if err := st.PutIAMPolicyJSON(repo, authz.Policy{
		Bindings: []authz.Binding{{
			Role: "roles/artifactregistry.reader", Members: []string{"serviceAccount:" + reader},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	cur = authn.Principal{Email: reader, IsRoot: false}
	req = httptest.NewRequest(http.MethodGet, base+"/bound/packages", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("repo reader list packages: %d %s", rec.Code, rec.Body.String())
	}

	cur = authn.Principal{Email: "stranger@example.com", IsRoot: false}
	req = httptest.NewRequest(http.MethodGet, base+"/bound/packages", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stranger want 403, got %d", rec.Code)
	}
}
