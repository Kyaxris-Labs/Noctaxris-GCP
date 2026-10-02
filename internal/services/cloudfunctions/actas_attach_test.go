package cloudfunctions_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudfunctions"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCloudFunctionsCreateRequiresActAs(t *testing.T) {
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
	caller := "fn-admin@" + project + ".iam.gserviceaccount.com"
	runtimeSA := "fn-runtime@" + project + ".iam.gserviceaccount.com"
	computeSA := labtoken.DefaultComputeSAEmail(project)
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{caller, runtimeSA, computeSA} {
		if err := st.EnsureServiceAccount(project, email, "lab"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CreateCustomRole(project, "fnCreate", "FnCreate", "", "GA", []string{"cloudfunctions.functions.create"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCustomRole(project, "actAsOnly", "ActAs", "", "GA", []string{"iam.serviceAccounts.actAs"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + project + "/roles/fnCreate",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &cloudfunctions.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: caller, IsRoot: false}, true
	})

	base := "/v2/projects/" + project + "/locations/" + cloudfunctions.DefaultLocation + "/functions"
	body := `{"serviceConfig":{"serviceAccountEmail":"` + runtimeSA + `"},"labResponse":{"ok":true}}`

	req := httptest.NewRequest(http.MethodPost, base+"?functionId=no-actas", bytes.NewReader([]byte(body)))
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
	req = httptest.NewRequest(http.MethodPost, base+"?functionId=with-actas", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create with actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	omitBody := `{"labResponse":{"ok":true}}`
	req = httptest.NewRequest(http.MethodPost, base+"?functionId=omit-no", bytes.NewReader([]byte(omitBody)))
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
	req = httptest.NewRequest(http.MethodPost, base+"?functionId=omit-yes", bytes.NewReader([]byte(omitBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("omitted SA with compute actAs status=%d body=%s", rec.Code, rec.Body.String())
	}
}
