package resourcemanager_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCRMV1ProjectsListBoring200(t *testing.T) {
	mux, _ := openCRM(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("v1 list: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	projects, _ := body["projects"].([]any)
	if len(projects) == 0 {
		t.Fatalf("projects=%#v", body)
	}
	p, _ := projects[0].(map[string]any)
	if p["projectId"] != "noctaxris-gcp-local" {
		t.Fatalf("projectId=%#v", p["projectId"])
	}
	if p["lifecycleState"] != "ACTIVE" {
		t.Fatalf("lifecycleState=%#v", p["lifecycleState"])
	}
	if _, ok := p["projectNumber"].(string); !ok {
		t.Fatalf("projectNumber=%#v", p["projectNumber"])
	}
	parent, _ := p["parent"].(map[string]any)
	if parent["type"] != "organization" || parent["id"] != store.DefaultOrganizationID {
		t.Fatalf("parent=%#v", parent)
	}
}
