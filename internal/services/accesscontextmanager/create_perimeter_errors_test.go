package accesscontextmanager_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestACMCreatePerimeterMissingID(t *testing.T) {
	mux, _ := acmMux(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/accessPolicies?policyId=cov1", bytes.NewReader([]byte(
		`{"parent":"organizations/noctaxris-gcp-org","title":"Cov"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("policy: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/accessPolicies/cov1/servicePerimeters",
		bytes.NewReader([]byte(`{"title":"no-id"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("missing perimeter id must fail")
	}
	req = httptest.NewRequest(http.MethodPatch, "/v1/accessPolicies/cov1", bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad patch json must fail")
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies/missing-policy/servicePerimeters", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("list missing policy perimeters: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, "/v1/accessPolicies/cov1/servicePerimeters/nope", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing perimeter: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/accessPolicies/missing-policy/servicePerimeters?servicePerimeterId=x",
		bytes.NewReader([]byte(`{"title":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("create under missing policy: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPatch, "/v1/accessPolicies/cov1/servicePerimeters/nope",
		bytes.NewReader([]byte(`{"title":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch missing perimeter: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list policies empty parent: %d", rec.Code)
	}
}
