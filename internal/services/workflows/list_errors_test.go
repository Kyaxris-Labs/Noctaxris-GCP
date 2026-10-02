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

func TestWorkflowsListPaginationAndNotFound(t *testing.T) {
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
	for _, id := range []string{"wf-a", "wf-b", "wf-c"} {
		req := httptest.NewRequest(http.MethodPost, base+"?workflowId="+id,
			bytes.NewReader([]byte(`{"sourceContents":"main: return 1"}`)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s status=%d body=%s", id, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, base+"?pageSize=1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var page1 map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &page1)
	items, _ := page1["workflows"].([]any)
	if len(items) != 1 {
		t.Fatalf("page1=%#v", page1)
	}
	token, _ := page1["nextPageToken"].(string)
	if token == "" {
		t.Fatalf("expected nextPageToken: %#v", page1)
	}

	req = httptest.NewRequest(http.MethodGet, base+"?pageSize=2&pageToken="+token, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list page2 status=%d body=%s", rec.Code, rec.Body.String())
	}
	var page2 map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &page2)
	items, _ = page2["workflows"].([]any)
	if len(items) < 1 {
		t.Fatalf("page2=%#v", page2)
	}

	req = httptest.NewRequest(http.MethodGet, base+"?pageToken=not-a-valid-token", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad pageToken status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/missing-wf", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/missing-wf", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/wf-a/executions/does-not-exist", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get execution missing status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/wf-a/executions/does-not-exist:cancel", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cancel missing status=%d body=%s", rec.Code, rec.Body.String())
	}
}
