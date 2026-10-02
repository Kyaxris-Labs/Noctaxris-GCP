package containeranalysis_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/containeranalysis"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestContainerAnalysisGetOccurrenceAndFixablePackageIssue(t *testing.T) {
	mux, _, project := caMux(t)
	img := "gcr.io/" + project + "/svc:2"
	body := `{
		"resourceUri":"` + img + `",
		"kind":"VULNERABILITY",
		"vulnerability":{
			"effectiveSeverity":"CRITICAL",
			"packageIssue":[{"fixedVersion":{"name":"1.2.3"},"fixedCpeUri":""}]
		}
	}`
	postOccurrence(t, mux, project, "crit-fix", body)

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/occurrences/crit-fix", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["kind"] != "VULNERABILITY" {
		t.Fatalf("got=%#v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/occurrences/missing-occ", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get status=%d body=%s", rec.Code, rec.Body.String())
	}

	postOccurrence(t, mux, project, "cpe-fix", `{
		"resourceUri":"`+img+`",
		"kind":"VULNERABILITY",
		"vulnerability":{"severity":"MEDIUM","packageIssue":[{"fixedCpeUri":"cpe:/a:fix"}]}
	}`)
	sumReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/occurrences:vulnerabilitySummary", nil)
	sumRec := httptest.NewRecorder()
	mux.ServeHTTP(sumRec, sumReq)
	if sumRec.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", sumRec.Code, sumRec.Body.String())
	}
	var sum map[string]any
	_ = json.Unmarshal(sumRec.Body.Bytes(), &sum)
	counts, _ := sum["counts"].([]any)
	if len(counts) == 0 {
		t.Fatalf("empty summary %#v", sum)
	}
}

func TestContainerAnalysisAuthzDenyAndBadFilter(t *testing.T) {
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
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	eval := &authz.Evaluator{Policies: st}
	(&containeranalysis.Service{Store: st, Authz: eval}).Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/occurrences", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("list deny status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/occurrences/x", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("get deny status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/occurrences:vulnerabilitySummary", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("summary deny status=%d body=%s", rec.Code, rec.Body.String())
	}

	muxOK, _, project := caMux(t)
	bad := `/v1/projects/` + project + `/occurrences?filter=` + url.QueryEscape(`kind="ATTESTATION" AND`)
	out, code := listOccurrences(t, muxOK, bad)
	if code == http.StatusOK {
		t.Fatalf("expected bad filter reject, got %#v", out)
	}
}

func TestContainerAnalysisImplicitAndFilter(t *testing.T) {
	mux, _, project := caMux(t)
	img := "gcr.io/" + project + "/implicit:1"
	postOccurrence(t, mux, project, "a1", `{"resourceUri":"`+img+`","kind":"ATTESTATION"}`)
	postOccurrence(t, mux, project, "v1", `{"resourceUri":"`+img+`","kind":"VULNERABILITY","vulnerability":{"severity":"HIGH"}}`)

	filter := `kind="ATTESTATION" resourceUrl="` + img + `"`
	out, code := listOccurrences(t, mux, "/v1/projects/"+project+"/occurrences?filter="+url.QueryEscape(filter))
	if code != http.StatusOK {
		t.Fatalf("status=%d body=%v", code, out)
	}
	occs, _ := out["occurrences"].([]any)
	if len(occs) != 1 {
		t.Fatalf("implicit AND want 1 got %#v", out)
	}
}
