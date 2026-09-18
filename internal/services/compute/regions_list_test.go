package compute_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComputeRegionsListBoring200(t *testing.T) {
	mux, _, project := mountCompute(t)
	req := httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/regions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("regions list: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["kind"] != "compute#regionList" {
		t.Fatalf("kind=%#v", body["kind"])
	}
	items, _ := body["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("items=%#v", body)
	}
	first, _ := items[0].(map[string]any)
	if first["name"] != "us-central1" || first["status"] != "UP" {
		t.Fatalf("first region=%#v", first)
	}
}
