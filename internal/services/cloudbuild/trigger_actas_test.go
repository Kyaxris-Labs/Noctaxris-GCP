package cloudbuild_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudbuild"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCreateTriggerAndRunRequireActAs(t *testing.T) {
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
	caller := "builder@" + workload + ".iam.gserviceaccount.com"
	namedSA := "ci-runner@" + workload + ".iam.gserviceaccount.com"
	computeSA := labtoken.DefaultComputeSAEmail(workload)
	for _, email := range []string{caller, namedSA, computeSA} {
		if err := st.EnsureServiceAccount(workload, email, "lab"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CreateCustomRole(workload, "buildTrig", "BuildTrig", "", "GA", []string{
		"cloudbuild.triggers.create", "cloudbuild.builds.create",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCustomRole(workload, "actAsOnly", "ActAs", "", "GA", []string{"iam.serviceAccounts.actAs"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+workload, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + workload + "/roles/buildTrig",
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

	trigBase := "/v1/projects/" + workload + "/triggers"

	// Negative: createTrigger without actAs on default Compute SA.
	req := httptest.NewRequest(http.MethodPost, trigBase,
		bytes.NewReader([]byte(`{"id":"no-actas","filename":"cloudbuild.yaml"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("createTrigger without actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Positive: grant actAs on default Compute SA, create omitted-SA trigger.
	if err := st.PutIAMPolicyJSON("projects/"+workload+"/serviceAccounts/"+computeSA, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + workload + "/roles/actAsOnly",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, trigBase,
		bytes.NewReader([]byte(`{"id":"with-default","filename":"cloudbuild.yaml"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("createTrigger with actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Positive: :run with actAs on default Compute SA.
	req = httptest.NewRequest(http.MethodPost, trigBase+"/with-default:run",
		bytes.NewReader([]byte(`{"branchName":"main"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runTrigger with actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Negative: named SA create without actAs on that SA (still has compute actAs only).
	req = httptest.NewRequest(http.MethodPost, trigBase,
		bytes.NewReader([]byte(`{"id":"named-sa","serviceAccount":"`+namedSA+`","filename":"cloudbuild.yaml"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("createTrigger named SA without actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Seed a named-SA trigger as root, then deny :run without actAs on named SA.
	rootMux := http.NewServeMux()
	rootSvc := &cloudbuild.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	rootSvc.Mount(rootMux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	})
	req = httptest.NewRequest(http.MethodPost, trigBase,
		bytes.NewReader([]byte(`{"id":"run-named","serviceAccount":"`+namedSA+`","filename":"cloudbuild.yaml"}`)))
	rec = httptest.NewRecorder()
	rootMux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("root create named trigger status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, trigBase+"/run-named:run",
		bytes.NewReader([]byte(`{"branchName":"main"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("runTrigger named SA without actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Positive: grant actAs on named SA and :run succeeds.
	if err := st.PutIAMPolicyJSON("projects/"+workload+"/serviceAccounts/"+namedSA, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + workload + "/roles/actAsOnly",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, trigBase+"/run-named:run",
		bytes.NewReader([]byte(`{"branchName":"main"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runTrigger named SA with actAs status=%d body=%s", rec.Code, rec.Body.String())
	}
}
