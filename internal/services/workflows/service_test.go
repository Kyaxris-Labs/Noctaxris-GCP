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

func TestWorkflowsCRUDAndExecutions(t *testing.T) {
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
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	loc := workflows.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/workflows"
	body := `{"sourceContents":"main:\n  steps:\n    - done:\n        return: ok\n","description":"lab"}`
	req := httptest.NewRequest(http.MethodPost, base+"?workflowId=demo", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var wf map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wf); err != nil {
		t.Fatal(err)
	}
	if wf["state"] != "ACTIVE" || wf["sourceContents"] == "" {
		t.Fatalf("workflow=%#v", wf)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/demo", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/demo/executions", bytes.NewReader([]byte(`{"argument":"{\"n\":1}"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create execution status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ex map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ex); err != nil {
		t.Fatal(err)
	}
	if ex["state"] != "ACTIVE" {
		t.Fatalf("execution create should be ACTIVE: %#v", ex)
	}
	exName, _ := ex["name"].(string)
	if exName == "" {
		t.Fatal("missing execution name")
	}
	parts := bytes.Split([]byte(exName), []byte("/"))
	execID := string(parts[len(parts)-1])

	req = httptest.NewRequest(http.MethodGet, base+"/demo/executions/"+execID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get execution status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ex)
	if ex["state"] != "SUCCEEDED" || ex["result"] == "" {
		t.Fatalf("get should advance to SUCCEEDED: %#v", ex)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/demo/executions", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list executions status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/demo", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWorkflowsDeepenPatchCancelPageSizeArgument(t *testing.T) {
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
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	loc := workflows.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/workflows"
	req := httptest.NewRequest(http.MethodPost, base+"?workflowId=deep",
		bytes.NewReader([]byte(`{"sourceContents":"v1","description":"d1"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var wf map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &wf)
	rev1, _ := wf["revisionId"].(string)

	req = httptest.NewRequest(http.MethodPatch, base+"/deep?updateMask=sourceContents,description",
		bytes.NewReader([]byte(`{"sourceContents":"v2","description":"d2"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &wf)
	if wf["sourceContents"] != "v2" || wf["description"] != "d2" {
		t.Fatalf("patched workflow=%#v", wf)
	}
	if wf["revisionId"] == rev1 {
		t.Fatalf("expected revision bump, still %v", wf["revisionId"])
	}

	req = httptest.NewRequest(http.MethodPost, base+"/deep/executions",
		bytes.NewReader([]byte(`{"argument":"not-json"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid argument status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/deep/executions",
		bytes.NewReader([]byte(`{"argument":"{\"k\":true}"}`)))
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

	req = httptest.NewRequest(http.MethodPost, base+"/deep/executions/"+execID+":cancel", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ex)
	if ex["state"] != "CANCELLED" {
		t.Fatalf("cancel execution=%#v", ex)
	}

	// Second execution for pageSize
	req = httptest.NewRequest(http.MethodPost, base+"/deep/executions", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create second execution status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/deep/executions?pageSize=1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list page status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listBody)
	execs, _ := listBody["executions"].([]any)
	if len(execs) != 1 {
		t.Fatalf("pageSize=1 got %#v", listBody)
	}
	if listBody["nextPageToken"] == nil || listBody["nextPageToken"] == "" {
		t.Fatalf("expected nextPageToken: %#v", listBody)
	}
}

func TestWorkflowsFailClosedWithoutPrincipal(t *testing.T) {
	mux := http.NewServeMux()
	svc := &workflows.Service{}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false })
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/p/locations/us-central1/workflows", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestWorkflowsAuthzDenyNonRootWithoutBinding(t *testing.T) {
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
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/locations/us-central1/workflows", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
