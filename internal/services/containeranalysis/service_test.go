package containeranalysis_test

import (
	"bytes"
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

func caMux(t *testing.T) (*http.ServeMux, *store.Store, string) {
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
	(&containeranalysis.Service{Store: st, Authz: eval}).Mount(mux, who)
	return mux, st, project
}

func postOccurrence(t *testing.T, mux http.Handler, project, id, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/occurrences?occurrenceId="+id, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create %s status=%d body=%s", id, rec.Code, rec.Body.String())
	}
}

func listOccurrences(t *testing.T, mux http.Handler, path string) (map[string]any, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode status=%d body=%s err=%v", rec.Code, rec.Body.String(), err)
	}
	return out, rec.Code
}

func TestContainerAnalysisListUnfiltered(t *testing.T) {
	mux, _, project := caMux(t)
	postOccurrence(t, mux, project, "a1", `{"resourceUri":"img:a","kind":"ATTESTATION","noteName":"projects/`+project+`/notes/n1"}`)
	postOccurrence(t, mux, project, "v1", `{"resourceUri":"img:a","kind":"VULNERABILITY","vulnerability":{"severity":"HIGH"}}`)

	out, code := listOccurrences(t, mux, "/v1/projects/"+project+"/occurrences")
	if code != http.StatusOK {
		t.Fatalf("status=%d", code)
	}
	occs, _ := out["occurrences"].([]any)
	if len(occs) != 2 {
		t.Fatalf("want 2 got %#v", out)
	}
}

func TestContainerAnalysisListKindAndSpacedResourceURL(t *testing.T) {
	mux, _, project := caMux(t)
	img := "us-docker.pkg.dev/" + project + "/apps/web:1"
	postOccurrence(t, mux, project, "att", `{"resourceUri":"`+img+`","kind":"ATTESTATION","noteName":"projects/`+project+`/notes/attestor"}`)
	postOccurrence(t, mux, project, "vuln", `{"resourceUri":"`+img+`","kind":"VULNERABILITY","vulnerability":{"severity":"LOW"}}`)
	postOccurrence(t, mux, project, "other", `{"resourceUri":"other:1","kind":"ATTESTATION"}`)

	filter := `kind = "ATTESTATION" AND resourceUrl = "` + img + `"`
	path := "/v1/projects/" + project + "/occurrences?filter=" + url.QueryEscape(filter)
	out, code := listOccurrences(t, mux, path)
	if code != http.StatusOK {
		t.Fatalf("status=%d body=%v", code, out)
	}
	occs, _ := out["occurrences"].([]any)
	if len(occs) != 1 {
		t.Fatalf("want 1 got %#v", out)
	}
}

func TestContainerAnalysisListHTTPSVsBare(t *testing.T) {
	mux, _, project := caMux(t)
	bare := "gcr.io/" + project + "/app@sha256:abc"
	postOccurrence(t, mux, project, "bare", `{"resourceUri":"`+bare+`","kind":"ATTESTATION"}`)

	filter := `resourceUrl="https://` + bare + `"`
	out, code := listOccurrences(t, mux, "/v1/projects/"+project+"/occurrences?filter="+url.QueryEscape(filter))
	if code != http.StatusOK {
		t.Fatalf("status=%d", code)
	}
	if occs, _ := out["occurrences"].([]any); len(occs) != 1 {
		t.Fatalf("https filter vs bare store %#v", out)
	}

	postOccurrence(t, mux, project, "https-stored", `{"resourceUri":"https://gcr.io/`+project+`/other:1","kind":"ATTESTATION"}`)
	filter2 := `resourceUri="gcr.io/` + project + `/other:1"`
	out, code = listOccurrences(t, mux, "/v1/projects/"+project+"/occurrences?filter="+url.QueryEscape(filter2))
	if code != http.StatusOK {
		t.Fatalf("status=%d", code)
	}
	if occs, _ := out["occurrences"].([]any); len(occs) != 1 {
		t.Fatalf("bare filter vs https store %#v", out)
	}
}

func TestContainerAnalysisVulnerabilitySummary(t *testing.T) {
	mux, _, project := caMux(t)
	img := "gcr.io/" + project + "/web:1"
	postOccurrence(t, mux, project, "h1", `{"resourceUri":"`+img+`","kind":"VULNERABILITY","vulnerability":{"severity":"HIGH","fixAvailable":true}}`)
	postOccurrence(t, mux, project, "l1", `{"resourceUri":"`+img+`","kind":"VULNERABILITY","vulnerability":{"severity":"LOW","fixAvailable":false}}`)
	postOccurrence(t, mux, project, "att", `{"resourceUri":"`+img+`","kind":"ATTESTATION"}`)

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/occurrences:vulnerabilitySummary", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	counts, _ := out["counts"].([]any)
	var sawTotal bool
	for _, c := range counts {
		m, _ := c.(map[string]any)
		if m["severity"] == "SEVERITY_UNSPECIFIED" && m["totalCount"] == "2" && m["fixableCount"] == "1" {
			sawTotal = true
		}
	}
	if !sawTotal {
		t.Fatalf("missing SEVERITY_UNSPECIFIED total %#v", out)
	}
}

func TestContainerAnalysisPagination(t *testing.T) {
	mux, _, project := caMux(t)
	for i := 0; i < 5; i++ {
		id := "occ-" + string(rune('a'+i))
		postOccurrence(t, mux, project, id, `{"resourceUri":"img:`+id+`","kind":"ATTESTATION"}`)
	}
	out, code := listOccurrences(t, mux, "/v1/projects/"+project+"/occurrences?pageSize=2")
	if code != http.StatusOK {
		t.Fatalf("status=%d", code)
	}
	occs, _ := out["occurrences"].([]any)
	if len(occs) != 2 {
		t.Fatalf("page1 %#v", out)
	}
	tok, _ := out["nextPageToken"].(string)
	if tok == "" {
		t.Fatal("expected nextPageToken")
	}
	out2, code := listOccurrences(t, mux, "/v1/projects/"+project+"/occurrences?pageSize=2&pageToken="+tok)
	if code != http.StatusOK {
		t.Fatalf("status=%d", code)
	}
	occs2, _ := out2["occurrences"].([]any)
	if len(occs2) != 2 {
		t.Fatalf("page2 %#v", out2)
	}
	tok2, _ := out2["nextPageToken"].(string)
	out3, code := listOccurrences(t, mux, "/v1/projects/"+project+"/occurrences?pageSize=2&pageToken="+tok2)
	if code != http.StatusOK {
		t.Fatalf("status=%d", code)
	}
	occs3, _ := out3["occurrences"].([]any)
	if len(occs3) != 1 {
		t.Fatalf("page3 %#v", out3)
	}
	if _, ok := out3["nextPageToken"]; ok {
		t.Fatalf("unexpected next on last page %#v", out3)
	}
}

func TestContainerAnalysisLocationScopedRoutes(t *testing.T) {
	mux, _, project := caMux(t)
	body := `{"resourceUri":"loc-img:1","kind":"ATTESTATION"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/locations/us/occurrences?occurrenceId=loc-1", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	out, code := listOccurrences(t, mux, "/v1/projects/"+project+"/locations/us/occurrences?filter="+url.QueryEscape(`resourceUrl="loc-img:1"`))
	if code != http.StatusOK {
		t.Fatalf("list status=%d", code)
	}
	if occs, _ := out["occurrences"].([]any); len(occs) != 1 {
		t.Fatalf("location list %#v", out)
	}
	sumReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/locations/us/occurrences:vulnerabilitySummary", nil)
	sumRec := httptest.NewRecorder()
	mux.ServeHTTP(sumRec, sumReq)
	if sumRec.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", sumRec.Code, sumRec.Body.String())
	}
}

func TestContainerAnalysisListEmitsGcloudArtifactsResourceURI(t *testing.T) {
	mux, _, project := caMux(t)
	stored := "us-central1-docker.pkg.dev/" + project + "/repo/app@sha256:deadbeef"
	want := "https://us-central1-docker.pkg.dev/" + project + "/repo/app@sha256-deadbeef"
	postOccurrence(t, mux, project, "att-digest", `{"resourceUri":"`+stored+`","kind":"ATTESTATION"}`)
	postOccurrence(t, mux, project, "vuln-digest", `{"resourceUri":"`+stored+`","kind":"VULNERABILITY","vulnerability":{"severity":"HIGH"}}`)

	out, code := listOccurrences(t, mux, "/v1/projects/"+project+"/occurrences?filter="+url.QueryEscape(`kind="ATTESTATION"`))
	if code != http.StatusOK {
		t.Fatalf("status=%d", code)
	}
	occs, _ := out["occurrences"].([]any)
	if len(occs) != 1 {
		t.Fatalf("want 1 got %#v", out)
	}
	m, _ := occs[0].(map[string]any)
	if m["resourceUri"] != want {
		t.Fatalf("resourceUri=%v want %s", m["resourceUri"], want)
	}

	filter := `resourceUrl="us-central1-docker.pkg.dev/` + project + `/repo/app@sha256-deadbeef"`
	out, code = listOccurrences(t, mux, "/v1/projects/"+project+"/occurrences?filter="+url.QueryEscape(filter))
	if code != http.StatusOK {
		t.Fatalf("filter status=%d", code)
	}
	if occs, _ := out["occurrences"].([]any); len(occs) != 2 {
		t.Fatalf("hyphen filter should match colon store %#v", out)
	}

	sumReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/occurrences:vulnerabilitySummary", nil)
	sumRec := httptest.NewRecorder()
	mux.ServeHTTP(sumRec, sumReq)
	if sumRec.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", sumRec.Code, sumRec.Body.String())
	}
	var sum map[string]any
	if err := json.Unmarshal(sumRec.Body.Bytes(), &sum); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range sum["counts"].([]any) {
		row, _ := c.(map[string]any)
		if row["resourceUri"] == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("summary resourceUri not rewritten %#v", sum)
	}
}

func TestContainerAnalysisBinaryAuthzExactResourceURI(t *testing.T) {
	_, st, project := caMux(t)
	img := "us-docker.pkg.dev/" + project + "/apps/web:1"
	o := store.ContainerOccurrence{
		Name: "projects/" + project + "/occurrences/att-exact", ProjectID: project, OccurrenceID: "att-exact",
		ResourceURI: img, Kind: "ATTESTATION", NoteName: "projects/" + project + "/notes/n", BodyJSON: `{}`,
	}
	if err := st.PutContainerOccurrence(o); err != nil {
		t.Fatal(err)
	}
	if err := st.PutBinaryAuthzPolicy(project, "ENFORCED_BLOCK_AND_AUDIT_LOG", `{}`); err != nil {
		t.Fatal(err)
	}
	ok, err := st.BinaryAuthzAllows(project, img)
	if err != nil || !ok {
		t.Fatalf("exact admit ok=%v err=%v", ok, err)
	}
	ok, err = st.BinaryAuthzAllows(project, "https://"+img)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("BinaryAuthzAllows must stay exact resource_uri match (https form should not admit bare store)")
	}
}
