package workflows_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/workflows"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestWorkflowsCreateFromNameLabelsAndUnknownMethods(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &workflows.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	loc := workflows.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/workflows"

	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(
		`{"name":"projects/noctaxris-gcp-local/locations/`+loc+`/workflows/from-name","sourceContents":"main: return 1","labels":{"env":"lab"},"serviceAccount":"runtime@noctaxris-gcp-local.iam.gserviceaccount.com"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create from name status=%d body=%s", rec.Code, rec.Body.String())
	}
	var wf map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &wf)
	labels, _ := wf["labels"].(map[string]any)
	if labels["env"] != "lab" {
		t.Fatalf("labels=%#v", wf)
	}

	req = httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(`{"sourceContents":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing workflowId status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"?workflowId=from-name",
		bytes.NewReader([]byte(`{"sourceContents":"dup"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, base+"/from-name?updateMask=labels,serviceAccount",
		bytes.NewReader([]byte(`{"labels":{"env":"prod"},"serviceAccount":"runtime@noctaxris-gcp-local.iam.gserviceaccount.com"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch labels status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, base+"/missing-wf",
		bytes.NewReader([]byte(`{"description":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch missing status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/from-name:export", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown workflow method status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/from-name/executions", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create execution status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ex map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &ex)
	exName, _ := ex["name"].(string)
	parts := bytes.Split([]byte(exName), []byte("/"))
	execID := string(parts[len(parts)-1])

	req = httptest.NewRequest(http.MethodGet, base+"/from-name/executions/"+execID+":bogus", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown execution get method status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/from-name/executions/"+execID+":pause", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown execution post method status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/missing/executions", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("list executions missing wf status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/from-name/executions?pageToken=bad-token", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad execution pageToken status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"?pageSize=-3", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("negative pageSize status=%d body=%s", rec.Code, rec.Body.String())
	}
}
