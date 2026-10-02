package dataflow_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/dataflow"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCreateJobRequiresActAs(t *testing.T) {
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
	caller := "caller@" + project + ".iam.gserviceaccount.com"
	runtime := "runtime@" + project + ".iam.gserviceaccount.com"
	computeSA := labtoken.DefaultComputeSAEmail(project)
	for _, email := range []string{caller, runtime, computeSA} {
		if err := st.CreateServiceAccount(store.ServiceAccount{
			ProjectID: project, Email: email, UniqueID: strings.Split(email, "@")[0], DisplayName: email,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CreateCustomRole(project, "dfCreate", "DF", "", "GA", []string{"dataflow.jobs.create"}); err != nil {
		t.Fatal(err)
	}
	role := "projects/" + project + "/roles/dfCreate"
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{Role: role, Members: []string{"serviceAccount:" + caller}}},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	(&dataflow.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}).Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: caller, IsRoot: false}, true
	})
	path := "/v1b3/projects/" + project + "/locations/" + dataflow.DefaultLocation + "/jobs"
	body := `{"name":"j1","environment":{"serviceAccountEmail":"` + runtime + `"}}`
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	if err := st.PutIAMPolicyJSON("projects/"+project+"/serviceAccounts/"+runtime, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/iam.serviceAccountUser",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Default Compute Engine SA when environment.serviceAccountEmail is omitted.
	req = httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{"name":"j2"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("default SA without actAs status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := st.PutIAMPolicyJSON("projects/"+project+"/serviceAccounts/"+computeSA, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/iam.serviceAccountUser",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{"name":"j2"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("default SA with actAs status=%d body=%s", rec.Code, rec.Body.String())
	}
}
