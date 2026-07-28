package dataflow_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/dataflow"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestDataflowJobsCreateGetListTheatre(t *testing.T) {
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
	svc := &dataflow.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	loc := dataflow.DefaultLocation
	base := "/v1b3/projects/noctaxris-gcp-local/locations/" + loc + "/jobs"
	body := `{"name":"lab-batch","type":"JOB_TYPE_BATCH"}`
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var job map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job["currentState"] != "JOB_STATE_RUNNING" {
		t.Fatalf("create should be RUNNING: %#v", job)
	}
	jobID, _ := job["id"].(string)
	if jobID == "" {
		t.Fatal("missing job id")
	}

	req = httptest.NewRequest(http.MethodGet, base+"/"+jobID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	if job["currentState"] != "JOB_STATE_DONE" {
		t.Fatalf("get should advance to DONE: %#v", job)
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list regional status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listBody)
	jobs, _ := listBody["jobs"].([]any)
	if len(jobs) != 1 {
		t.Fatalf("list regional=%#v", listBody)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1b3/projects/noctaxris-gcp-local/jobs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list project status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDataflowFailClosedWithoutPrincipal(t *testing.T) {
	mux := http.NewServeMux()
	svc := &dataflow.Service{}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false })
	req := httptest.NewRequest(http.MethodGet, "/v1b3/projects/p/locations/us-central1/jobs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}
