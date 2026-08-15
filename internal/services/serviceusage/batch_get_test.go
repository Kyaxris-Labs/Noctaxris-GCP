package serviceusage_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServiceUsageBatchGet(t *testing.T) {
	mux, project := setupServiceUsage(t)
	n1 := "projects/" + project + "/services/storage.googleapis.com"
	n2 := "projects/" + project + "/services/pubsub.googleapis.com"

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/services:batchGet?names="+n1+"&names="+n2, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET batchGet: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	services, _ := body["services"].([]any)
	if len(services) < 2 {
		t.Fatalf("services=%#v", body)
	}

	post := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/services:batchGet",
		bytes.NewReader([]byte(`{"names":["storage.googleapis.com","pubsub.googleapis.com"]}`)))
	post.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, post)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST batchGet: %d %s", rec.Code, rec.Body.String())
	}
}
