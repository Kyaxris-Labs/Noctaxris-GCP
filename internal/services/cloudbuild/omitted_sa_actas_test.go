package cloudbuild_test

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
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudbuild"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCreateBuildOmittedSARequiresActAsOnDefaultCompute(t *testing.T) {
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
	workload := "noctaxris-gcp-local"
	root := "root@" + workload + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(workload, root); err != nil {
		t.Fatal(err)
	}
	caller := "builds@" + workload + ".iam.gserviceaccount.com"
	computeSA := labtoken.DefaultComputeSAEmail(workload)
	for _, email := range []string{caller, computeSA} {
		if err := st.CreateServiceAccount(store.ServiceAccount{
			ProjectID: workload, Email: email, UniqueID: strings.Split(email, "@")[0], DisplayName: email,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CreateCustomRole(workload, "buildOnly", "Build", "", "GA", []string{"cloudbuild.builds.create"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCustomRole(workload, "actAsOnly", "ActAs", "", "GA", []string{"iam.serviceAccounts.actAs"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+workload, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + workload + "/roles/buildOnly",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &cloudbuild.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: caller, IsRoot: false}, true
	})

	body := `{"steps":[{"name":"alpine:3.23","args":["true"]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/builds", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("omitted SA without actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	if err := st.PutIAMPolicyJSON("projects/"+workload+"/serviceAccounts/"+computeSA, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + workload + "/roles/actAsOnly",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/builds", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("actAs default compute createBuild status=%d body=%s", rec.Code, rec.Body.String())
	}
}
