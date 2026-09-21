package resourcemanager_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCRMCreateSecondProject(t *testing.T) {
	mux, st := openCRM(t)

	body := []byte(`{"projectId":"cb-host","displayName":"Worker pool host"}`)
	req := httptest.NewRequest(http.MethodPost, "/v3/projects", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["projectId"] != "cb-host" {
		t.Fatalf("created %#v", created)
	}

	got, ok, err := st.GetProject("cb-host")
	if err != nil || !ok || got.ID != "cb-host" {
		t.Fatalf("persisted %#v ok=%v err=%v", got, ok, err)
	}

	req = httptest.NewRequest(http.MethodGet, "/v3/projects/cb-host", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get second project status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v3/projects", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v3/projects", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Projects []map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Projects) < 2 {
		t.Fatalf("want two projects, got %#v", listed.Projects)
	}
}
