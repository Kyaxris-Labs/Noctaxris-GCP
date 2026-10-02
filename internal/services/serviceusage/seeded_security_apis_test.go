package serviceusage_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServiceUsageSeededSecurityAPIsEnabled(t *testing.T) {
	mux, project := setupServiceUsage(t)
	for _, svc := range []string{
		"containeranalysis.googleapis.com",
		"binaryauthorization.googleapis.com",
		"cloudasset.googleapis.com",
		"securitycenter.googleapis.com",
		"orgpolicy.googleapis.com",
		"accesscontextmanager.googleapis.com",
	} {
		req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/services/"+svc, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", svc, rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["state"] != "ENABLED" {
			t.Fatalf("%s state=%v", svc, body["state"])
		}
	}
}
