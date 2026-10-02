package accesscontextmanager_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestACMPatchPolicyListAndConflict(t *testing.T) {
	mux, _ := acmMux(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/accessPolicies?policyId=cov2", bytes.NewReader([]byte(
		`{"parent":"organizations/noctaxris-gcp-org","title":"Cov2","scopes":["projects/noctaxris-gcp-local"]}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/accessPolicies?policyId=cov2", bytes.NewReader([]byte(
		`{"parent":"organizations/noctaxris-gcp-org","title":"Dup"}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup policy: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/v1/accessPolicies/cov2?updateMask=title,scopes",
		bytes.NewReader([]byte(`{"title":"Cov2b","scopes":["projects/noctaxris-gcp-local"]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies/cov2", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}

	perim := `{"name":"accessPolicies/cov2/servicePerimeters/dry","title":"Dry","useExplicitDryRunSpec":true,"spec":{"resources":["projects/noctaxris-gcp-local"],"restrictedServices":["storage.googleapis.com"]}}`
	req = httptest.NewRequest(http.MethodPost, "/v1/accessPolicies/cov2/servicePerimeters",
		bytes.NewReader([]byte(perim)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create perimeter from name: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies/cov2/servicePerimeters", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list perimeters: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/accessPolicies/cov2/servicePerimeters?servicePerimeterId=dry",
		bytes.NewReader([]byte(`{"title":"again"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup perimeter: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch,
		"/v1/accessPolicies/cov2/servicePerimeters/dry?updateMask=description,status",
		bytes.NewReader([]byte(`{"description":"d","status":{"resources":["projects/noctaxris-gcp-local"],"restrictedServices":["pubsub.googleapis.com"]}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch perimeter status: %d %s", rec.Code, rec.Body.String())
	}
}
