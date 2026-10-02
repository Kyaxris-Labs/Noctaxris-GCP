package logging_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingExclusionsCRUD(t *testing.T) {
	mux := setupLogging(t)
	const project = "noctaxris-gcp-local"
	base := "/v2/projects/" + project + "/exclusions"

	req := httptest.NewRequest(http.MethodPost, base+"?exclusionId=skip-debug",
		bytes.NewReader([]byte(`{"filter":"severity>=DEBUG","description":"drop debug","disabled":false}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["name"] != "projects/"+project+"/exclusions/skip-debug" {
		t.Fatalf("created=%#v", created)
	}

	req = httptest.NewRequest(http.MethodPost, base+"?exclusionId=skip-debug",
		bytes.NewReader([]byte(`{"filter":"severity>=INFO"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(`{"filter":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing id status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	exclusions, _ := list["exclusions"].([]any)
	if len(exclusions) < 1 {
		t.Fatalf("list=%#v", list)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/skip-debug", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/skip-debug", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/skip-debug", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLoggingBucketsAndViews(t *testing.T) {
	mux := setupLogging(t)
	const project = "noctaxris-gcp-local"
	loc := "global"
	bucketsBase := "/v2/projects/" + project + "/locations/" + loc + "/buckets"

	req := httptest.NewRequest(http.MethodGet, bucketsBase, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list buckets status=%d body=%s", rec.Code, rec.Body.String())
	}
	var bucketList map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &bucketList)
	buckets, _ := bucketList["buckets"].([]any)
	if len(buckets) < 2 {
		t.Fatalf("expected default buckets, got %#v", bucketList)
	}

	req = httptest.NewRequest(http.MethodGet, bucketsBase+"/_Default", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get bucket status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, bucketsBase+"/nope", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing bucket status=%d body=%s", rec.Code, rec.Body.String())
	}

	viewsBase := bucketsBase + "/_Default/views"
	req = httptest.NewRequest(http.MethodPost, viewsBase+"?viewId=lab-view",
		bytes.NewReader([]byte(`{"filter":"severity>=INFO"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create view status=%d body=%s", rec.Code, rec.Body.String())
	}
	var view map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	wantName := "projects/" + project + "/locations/" + loc + "/buckets/_Default/views/lab-view"
	if view["name"] != wantName {
		t.Fatalf("view=%#v", view)
	}

	req = httptest.NewRequest(http.MethodPost, viewsBase+"?viewId=lab-view",
		bytes.NewReader([]byte(`{"filter":"severity>=WARNING"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate view status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, viewsBase, bytes.NewReader([]byte(`{"filter":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing viewId status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, viewsBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list views status=%d body=%s", rec.Code, rec.Body.String())
	}
	var views map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &views)
	items, _ := views["views"].([]any)
	if len(items) < 1 {
		t.Fatalf("views=%#v", views)
	}

	req = httptest.NewRequest(http.MethodGet, viewsBase+"/lab-view", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get view status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, viewsBase+"/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing view status=%d body=%s", rec.Code, rec.Body.String())
	}
}
