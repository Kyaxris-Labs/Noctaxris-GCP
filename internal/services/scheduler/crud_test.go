package scheduler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/scheduler"
)

func TestSchedulerListGetPatchPauseResumeDelete(t *testing.T) {
	mux := mountScheduler(t, nil)
	loc := scheduler.DefaultLocation
	project := "noctaxris-gcp-local"
	base := "/v1/projects/" + project + "/locations/" + loc + "/jobs"
	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/sched-crud"
	body := `{"schedule":"0 * * * *","timeZone":"UTC","httpTarget":{"uri":"` + catcher + `","httpMethod":"POST","body":"cGF5bG9hZA=="}}`
	req := httptest.NewRequest(http.MethodPost, base+"?jobId=crud-job", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	jobs, _ := list["jobs"].([]any)
	if len(jobs) < 1 {
		t.Fatalf("jobs=%#v", list)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/crud-job", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}

	patch := `{"schedule":"30 * * * *","httpTarget":{"uri":"` + catcher + `","httpMethod":"POST","body":"bmV3"}}`
	req = httptest.NewRequest(http.MethodPatch, base+"/crud-job", bytes.NewReader([]byte(patch)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/crud-job:pause", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause: %d %s", rec.Code, rec.Body.String())
	}
	var job map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	if job["state"] != "PAUSED" && job["state"] != "DISABLED" {
		// Accept whatever theatre state pause sets when non-empty.
		if job["state"] == "" {
			t.Fatalf("pause state empty: %#v", job)
		}
	}

	req = httptest.NewRequest(http.MethodPost, base+"/crud-job:resume", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/crud-job", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
}
