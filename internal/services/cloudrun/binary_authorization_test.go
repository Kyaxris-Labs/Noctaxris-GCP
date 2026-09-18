package cloudrun_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/binaryauthorization"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/containeranalysis"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestBinaryAuthorizationAdmitsCloudRunImage(t *testing.T) {
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
	project := "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	who := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	}
	eval := &authz.Evaluator{Policies: st}
	(&cloudrun.Service{Store: st, Authz: eval, Invoker: compute.MockInvoker{}}).Mount(mux, who)
	(&containeranalysis.Service{Store: st, Authz: eval}).Mount(mux, who)
	(&binaryauthorization.Service{Store: st, Authz: eval}).Mount(mux, who)

	policyBody := `{"defaultAdmissionRule":{"evaluationMode":"REQUIRE_ATTESTATION","enforcementMode":"ENFORCED_BLOCK_AND_AUDIT_LOG"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/projects/"+project+"/policy", bytes.NewReader([]byte(policyBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put policy status=%d body=%s", rec.Code, rec.Body.String())
	}

	loc := cloudrun.DefaultLocation
	base := "/v2/projects/" + project + "/locations/" + loc + "/services"
	image := "us-docker.pkg.dev/" + project + "/apps/web:1"
	create := `{"template":{"containers":[{"image":"` + image + `"}]}}`
	req = httptest.NewRequest(http.MethodPost, base+"?serviceId=blocked", bytes.NewReader([]byte(create)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied deploy status=%d body=%s", rec.Code, rec.Body.String())
	}

	occ := `{"resourceUri":"` + image + `","kind":"ATTESTATION","noteName":"projects/` + project + `/notes/attestor"}`
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/occurrences?occurrenceId=att-1", bytes.NewReader([]byte(occ)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create occurrence status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/occurrences?filter=resourceUrl=\""+image+"\"", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list occurrences status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if occs, _ := listed["occurrences"].([]any); len(occs) != 1 {
		t.Fatalf("occurrences %#v", listed)
	}

	req = httptest.NewRequest(http.MethodPost, base+"?serviceId=admitted", bytes.NewReader([]byte(create)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admitted deploy status=%d body=%s", rec.Code, rec.Body.String())
	}
}
