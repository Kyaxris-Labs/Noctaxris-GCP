package cloudrun_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
)

func TestCloudRunJobsPatchListRevisionsIAM(t *testing.T) {
	mux := mountCloudRun(t, nil)
	loc := cloudrun.DefaultLocation
	project := "noctaxris-gcp-local"
	svcBase := "/v2/projects/" + project + "/locations/" + loc + "/services"
	jobBase := "/v2/projects/" + project + "/locations/" + loc + "/jobs"

	body := `{"template":{"containers":[{"image":"demo"}],"labResponseBody":"{\"ok\":true}"}}`
	req := httptest.NewRequest(http.MethodPost, svcBase+"?serviceId=covsvc", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create svc: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, svcBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list svc: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, svcBase+"/covsvc", bytes.NewReader([]byte(`{"template":{"containers":[{"image":"v2"}],"labResponseBody":"{\"v\":2}"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch svc: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, svcBase+"/covsvc/revisions", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list revisions: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, svcBase+"/covsvc:getIamPolicy", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, svcBase+"/covsvc:setIamPolicy",
		bytes.NewReader([]byte(`{"policy":{"etag":"ACAB","bindings":[{"role":"roles/run.invoker","members":["user:a@b.c"]}]}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy: %d %s", rec.Code, rec.Body.String())
	}

	jobBody := `{"template":{"template":{"containers":[{"image":"job"}]}}}`
	req = httptest.NewRequest(http.MethodPost, jobBase+"?jobId=covjob", bytes.NewReader([]byte(jobBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create job: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, jobBase+"/covjob", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get job: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, jobBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list jobs: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, jobBase+"/covjob", bytes.NewReader([]byte(`{"template":{"template":{"containers":[{"image":"job2"}]}}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch job: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, jobBase+"/covjob", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete job: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, svcBase+"/covsvc", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete svc: %d %s", rec.Code, rec.Body.String())
	}
}
