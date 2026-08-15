package securitycenter_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestSCCGetListDeleteAndInject(t *testing.T) {
	mux, _, project := sccMux(t, true)
	org := store.DefaultOrganizationID
	srcBase := "/v1/organizations/" + org + "/sources"

	req := httptest.NewRequest(http.MethodPost, srcBase+"?sourceId=deep-src", bytes.NewReader([]byte(
		`{"displayName":"Deep","description":"d"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create source: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, srcBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sources: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, srcBase+"/deep-src", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get source: %d %s", rec.Code, rec.Body.String())
	}

	findBase := srcBase + "/deep-src/findings"
	req = httptest.NewRequest(http.MethodPost, findBase+"?findingId=f-deep", bytes.NewReader([]byte(
		`{"category":"MALWARE","severity":"CRITICAL","resourceName":"//cloudresourcemanager.googleapis.com/projects/`+project+`"}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create finding: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, findBase+"/f-deep", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get finding: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, findBase+"/f-deep", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete finding: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, srcBase+"/deep-src", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete source: %d %s", rec.Code, rec.Body.String())
	}

	projSrc := "/v1/projects/" + project + "/sources"
	req = httptest.NewRequest(http.MethodPost, projSrc+"?sourceId=p-src", bytes.NewReader([]byte(`{"displayName":"P"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proj create: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, projSrc, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proj list: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, projSrc+"/p-src", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proj get: %d", rec.Code)
	}
	pFind := projSrc + "/p-src/findings"
	req = httptest.NewRequest(http.MethodPost, pFind+"?findingId=pf1", bytes.NewReader([]byte(
		`{"category":"XSS","severity":"LOW","resourceName":"//storage.googleapis.com/projects/_/buckets/b"}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proj finding: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, pFind, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proj list findings: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, pFind+"/pf1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proj get finding: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, pFind+"/pf1:setState", bytes.NewReader([]byte(`{"state":"INACTIVE"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proj setState: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, pFind+"/pf1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proj delete finding: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, projSrc+"/p-src", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proj delete source: %d", rec.Code)
	}

	injectBody := `{"parent":"organizations/` + org + `","sourceId":"inject-src","findings":[{"findingId":"inj1","category":"INJECTED","severity":"MEDIUM","resourceName":"//compute.googleapis.com/projects/` + project + `/zones/us-central1-a/instances/i1"}]}`
	req = httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/securitycenter:injectFindings", bytes.NewReader([]byte(injectBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("inject: %d %s", rec.Code, rec.Body.String())
	}
	var inj map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inj)
	if inj["createdCount"] == nil && inj["findings"] == nil && inj["name"] == nil {
		// Accept any non-empty success JSON shape the handler returns.
		if len(rec.Body.Bytes()) < 2 {
			t.Fatalf("inject empty body")
		}
	}
}

func TestSCCInjectDisabled(t *testing.T) {
	mux, _, _ := sccMux(t, false)
	req := httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/securitycenter:injectFindings",
		bytes.NewReader([]byte(`{"parent":"organizations/`+store.DefaultOrganizationID+`"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d %s", rec.Code, rec.Body.String())
	}
}
