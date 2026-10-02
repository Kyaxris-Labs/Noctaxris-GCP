package cloudrun_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCloudRunCreateRequiresActAs(t *testing.T) {
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

	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	caller := "deployer@" + project + ".iam.gserviceaccount.com"
	runtimeSA := "runtime@" + project + ".iam.gserviceaccount.com"
	computeSA := labtoken.DefaultComputeSAEmail(project)
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{caller, runtimeSA, computeSA} {
		if err := st.EnsureServiceAccount(project, email, "lab"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CreateCustomRole(project, "runCreate", "RunCreate", "", "GA", []string{"run.services.create"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCustomRole(project, "actAsOnly", "ActAs", "", "GA", []string{"iam.serviceAccounts.actAs"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + project + "/roles/runCreate",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &cloudrun.Service{
		Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}, Invoker: compute.MockInvoker{},
	}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: caller, IsRoot: false}, true
	})

	base := "/v2/projects/" + project + "/locations/" + cloudrun.DefaultLocation + "/services"
	body := `{"template":{"containers":[{"image":"gcr.io/demo"}],"serviceAccount":"` + runtimeSA + `"}}`

	req := httptest.NewRequest(http.MethodPost, base+"?serviceId=no-actas", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("create without actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	if err := st.PutIAMPolicyJSON("projects/"+project+"/serviceAccounts/"+runtimeSA, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + project + "/roles/actAsOnly",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, base+"?serviceId=with-actas", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create with actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Omitted SA requires actAs on default Compute Engine SA.
	omitBody := `{"template":{"containers":[{"image":"gcr.io/demo"}]}}`
	req = httptest.NewRequest(http.MethodPost, base+"?serviceId=omit-no", bytes.NewReader([]byte(omitBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("omitted SA without actAs status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := st.PutIAMPolicyJSON("projects/"+project+"/serviceAccounts/"+computeSA, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + project + "/roles/actAsOnly",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, base+"?serviceId=omit-yes", bytes.NewReader([]byte(omitBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("omitted SA with compute actAs status=%d body=%s", rec.Code, rec.Body.String())
	}
}
