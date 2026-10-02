package cloudrun_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
)

func TestCloudRunJobsIAMPatchAndMissing(t *testing.T) {
	mux := mountCloudRun(t, nil)
	loc := cloudrun.DefaultLocation
	project := "noctaxris-gcp-local"
	svcBase := "/v2/projects/" + project + "/locations/" + loc + "/services"
	jobBase := "/v2/projects/" + project + "/locations/" + loc + "/jobs"

	body := `{"template":{"containers":[{"image":"gcr.io/demo/app:1"}],"labResponseBody":"{\"ok\":true}"}}`
	req := httptest.NewRequest(http.MethodPost, svcBase+"?serviceId=iam-svc", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create service: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, svcBase+"/iam-svc:getIamPolicy", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIam empty: %d %s", rec.Code, rec.Body.String())
	}

	pol := `{"policy":{"bindings":[{"role":"roles/run.invoker","members":["allUsers"]}],"etag":"ACAB"}}`
	req = httptest.NewRequest(http.MethodPost, svcBase+"/iam-svc:setIamPolicy", bytes.NewReader([]byte(pol)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIam: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, svcBase+"/iam-svc:getIamPolicy", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("run.invoker")) {
		t.Fatalf("getIam: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, svcBase+"/missing:setIamPolicy", bytes.NewReader([]byte(pol)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("setIam missing: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, svcBase+"/missing:getIamPolicy", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("getIam missing: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, svcBase+"/iam-svc:setIamPolicy", bytes.NewReader([]byte(`{`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("setIam bad json: %d", rec.Code)
	}

	patch := `{"template":{"containers":[{"image":"gcr.io/demo/app:2"}],"labResponseBody":"{\"v\":2}"},"traffic":[{"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST","percent":100}]}`
	req = httptest.NewRequest(http.MethodPatch, svcBase+"/iam-svc", bytes.NewReader([]byte(patch)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch service: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, svcBase+"/iam-svc", bytes.NewReader([]byte(`{"traffic":[{"percent":100,"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"}]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("traffic-only patch: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, svcBase+"/missing", bytes.NewReader([]byte(patch)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch missing: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, jobBase+"?jobId=job1",
		bytes.NewReader([]byte(`{"template":{"containers":[{"image":"gcr.io/demo/job:1"}]}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create job: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, jobBase, bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("job without id: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, jobBase+"/job1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get job: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, jobBase+"/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing job: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, jobBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list jobs: %d", rec.Code)
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	jobs, _ := list["jobs"].([]any)
	if len(jobs) == 0 {
		t.Fatalf("expected jobs: %#v", list)
	}

	req = httptest.NewRequest(http.MethodPatch, jobBase+"/job1",
		bytes.NewReader([]byte(`{"template":{"containers":[{"image":"gcr.io/demo/job:2"}]}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch job: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, jobBase+"/missing",
		bytes.NewReader([]byte(`{"template":{"containers":[{"image":"x"}]}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch missing job: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, jobBase+"/job1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete job: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, jobBase+"/job1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete job: %d", rec.Code)
	}

	host := cloudrun.DefaultLocation + "-run.googleapis.com"
	knBase := "/apis/serving.knative.dev/v1/namespaces/" + project
	req = httptest.NewRequest(http.MethodGet, knBase+"/services/missing-kn", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("knative get missing: %d", rec.Code)
	}
}
