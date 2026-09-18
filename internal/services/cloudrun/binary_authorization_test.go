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

func binauthzMux(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
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
	return mux, project
}

func putEnforcedBinauthzPolicy(t *testing.T, mux http.Handler, project string) {
	t.Helper()
	policyBody := `{"defaultAdmissionRule":{"evaluationMode":"REQUIRE_ATTESTATION","enforcementMode":"ENFORCED_BLOCK_AND_AUDIT_LOG"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/projects/"+project+"/policy", bytes.NewReader([]byte(policyBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put policy status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func createOccurrence(t *testing.T, mux http.Handler, project, image, occurrenceID string) {
	t.Helper()
	occ := `{"resourceUri":"` + image + `","kind":"ATTESTATION","noteName":"projects/` + project + `/notes/attestor"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/occurrences?occurrenceId="+occurrenceID, bytes.NewReader([]byte(occ)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create occurrence status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func serveJSON(mux http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestBinaryAuthorizationAdmitsCloudRunImage(t *testing.T) {
	mux, project := binauthzMux(t)
	putEnforcedBinauthzPolicy(t, mux, project)

	loc := cloudrun.DefaultLocation
	base := "/v2/projects/" + project + "/locations/" + loc + "/services"
	image := "us-docker.pkg.dev/" + project + "/apps/web:1"
	create := `{"template":{"containers":[{"image":"` + image + `"}]}}`
	rec := serveJSON(mux, http.MethodPost, base+"?serviceId=blocked", create)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied deploy status=%d body=%s", rec.Code, rec.Body.String())
	}

	createOccurrence(t, mux, project, image, "att-1")

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/occurrences?filter=resourceUrl=\""+image+"\"", nil)
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

	rec = serveJSON(mux, http.MethodPost, base+"?serviceId=admitted", create)
	if rec.Code != http.StatusOK {
		t.Fatalf("admitted deploy status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBinaryAuthorizationAdmitsCloudRunJob(t *testing.T) {
	mux, project := binauthzMux(t)
	putEnforcedBinauthzPolicy(t, mux, project)

	loc := cloudrun.DefaultLocation
	base := "/v2/projects/" + project + "/locations/" + loc + "/jobs"
	image := "us-docker.pkg.dev/" + project + "/apps/web:1"
	create := `{"template":{"containers":[{"image":"` + image + `"}]}}`
	rec := serveJSON(mux, http.MethodPost, base+"?jobId=blocked", create)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied job create status=%d body=%s", rec.Code, rec.Body.String())
	}

	nested := `{"template":{"template":{"containers":[{"image":"` + image + `"}]}}}`
	rec = serveJSON(mux, http.MethodPost, base+"?jobId=blocked-nested", nested)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied nested job create status=%d body=%s", rec.Code, rec.Body.String())
	}

	createOccurrence(t, mux, project, image, "att-job-1")

	rec = serveJSON(mux, http.MethodPost, base+"?jobId=admitted", create)
	if rec.Code != http.StatusOK {
		t.Fatalf("admitted job create status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = serveJSON(mux, http.MethodPost, base+"?jobId=admitted-nested", nested)
	if rec.Code != http.StatusOK {
		t.Fatalf("admitted nested job create status=%d body=%s", rec.Code, rec.Body.String())
	}

	evil := "us-docker.pkg.dev/" + project + "/apps/evil:1"
	patch := `{"template":{"containers":[{"image":"` + evil + `"}]}}`
	rec = serveJSON(mux, http.MethodPatch, base+"/admitted", patch)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied job patch status=%d body=%s", rec.Code, rec.Body.String())
	}

	createOccurrence(t, mux, project, evil, "att-job-2")
	rec = serveJSON(mux, http.MethodPatch, base+"/admitted", patch)
	if rec.Code != http.StatusOK {
		t.Fatalf("admitted job patch status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBinaryAuthorizationDeniesUnattestedSidecar(t *testing.T) {
	mux, project := binauthzMux(t)
	putEnforcedBinauthzPolicy(t, mux, project)

	loc := cloudrun.DefaultLocation
	imgA := "us-docker.pkg.dev/" + project + "/apps/web:1"
	imgB := "us-docker.pkg.dev/" + project + "/apps/sidecar:1"
	createOccurrence(t, mux, project, imgA, "att-sidecar-a")

	svcBody := `{"template":{"containers":[{"image":"` + imgA + `"},{"image":"` + imgB + `"}]}}`
	svcBase := "/v2/projects/" + project + "/locations/" + loc + "/services"
	rec := serveJSON(mux, http.MethodPost, svcBase+"?serviceId=sidecar-blocked", svcBody)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied sidecar service status=%d body=%s", rec.Code, rec.Body.String())
	}

	jobBody := `{"template":{"template":{"containers":[{"image":"` + imgA + `"},{"image":"` + imgB + `"}]}}}`
	jobBase := "/v2/projects/" + project + "/locations/" + loc + "/jobs"
	rec = serveJSON(mux, http.MethodPost, jobBase+"?jobId=sidecar-blocked", jobBody)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied sidecar job status=%d body=%s", rec.Code, rec.Body.String())
	}

	createOccurrence(t, mux, project, imgB, "att-sidecar-b")

	rec = serveJSON(mux, http.MethodPost, svcBase+"?serviceId=sidecar-ok", svcBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("admitted sidecar service status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = serveJSON(mux, http.MethodPost, jobBase+"?jobId=sidecar-ok", jobBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("admitted sidecar job status=%d body=%s", rec.Code, rec.Body.String())
	}
}
