package eventarc_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/eventarc"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestEventarcOmittedSARequiresActAsOnDefaultCompute(t *testing.T) {
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
	caller := "ea@" + project + ".iam.gserviceaccount.com"
	computeSA := labtoken.DefaultComputeSAEmail(project)
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{caller, computeSA} {
		if err := st.EnsureServiceAccount(project, email, "lab"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CreateCustomRole(project, "eaOnly", "Eventarc", "", "GA", []string{"eventarc.triggers.create"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCustomRole(project, "actAsOnly", "ActAs", "", "GA", []string{"iam.serviceAccounts.actAs"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + project + "/roles/eaOnly",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	principal := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: caller, IsRoot: false}, true
	}
	mux := http.NewServeMux()
	svc := &eventarc.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/triggers", restlab.Wrap(principal, svc.CreateTriggerHTTP))

	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/eventarc-omitted"
	body := `{"eventFilters":[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}],"destination":{"httpEndpoint":{"uri":"` + catcher + `"}}}`
	base := "/v1/projects/" + project + "/locations/us-central1/triggers"
	req := httptest.NewRequest(http.MethodPost, base+"?triggerId=no-actas", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
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
	req = httptest.NewRequest(http.MethodPost, base+"?triggerId=with-actas", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("actAs default compute create status=%d body=%s", rec.Code, rec.Body.String())
	}
}
