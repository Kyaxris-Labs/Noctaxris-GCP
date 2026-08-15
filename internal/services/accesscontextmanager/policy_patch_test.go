package accesscontextmanager_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestACMPolicyPerimeterGetPatchDelete(t *testing.T) {
	mux, _ := acmMux(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/accessPolicies?policyId=pol2", bytes.NewReader([]byte(
		`{"parent":"organizations/noctaxris-gcp-org","title":"P2"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies/pol2", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get policy: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/v1/accessPolicies/pol2", bytes.NewReader([]byte(`{"title":"P2-renamed"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch policy: %d %s", rec.Code, rec.Body.String())
	}

	body := `{"title":"Perim","status":{"resources":["projects/noctaxris-gcp-local"],"restrictedServices":["storage.googleapis.com"]}}`
	req = httptest.NewRequest(http.MethodPost, "/v1/accessPolicies/pol2/servicePerimeters?servicePerimeterId=perim1", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create perimeter: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies/pol2/servicePerimeters", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list perimeters: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies/pol2/servicePerimeters/perim1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get perimeter: %d %s", rec.Code, rec.Body.String())
	}

	patch := `{"title":"Perim2","status":{"resources":["projects/noctaxris-gcp-local"],"restrictedServices":["pubsub.googleapis.com"]}}`
	req = httptest.NewRequest(http.MethodPatch, "/v1/accessPolicies/pol2/servicePerimeters/perim1", bytes.NewReader([]byte(patch)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch perimeter: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)

	req = httptest.NewRequest(http.MethodDelete, "/v1/accessPolicies/pol2/servicePerimeters/perim1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete perimeter: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/accessPolicies/pol2", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete policy: %d %s", rec.Code, rec.Body.String())
	}
}
