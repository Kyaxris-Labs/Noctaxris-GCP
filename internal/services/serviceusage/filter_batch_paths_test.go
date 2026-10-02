package serviceusage_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/serviceusage"
)

func TestServiceUsageFilterBatchGetAndUnknownMethods(t *testing.T) {
	mux, project := setupServiceUsage(t)
	base := "/v1/projects/" + project + "/services"

	req := httptest.NewRequest(http.MethodGet, base+"?filter=not-a-filter", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad filter status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/never.googleapis.com", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("never-enabled get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var svc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &svc)
	if svc["state"] != "DISABLED" {
		t.Fatalf("never-enabled=%#v", svc)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/storage.googleapis.com:enable", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("get with action status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/storage.googleapis.com", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("post without action status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/storage.googleapis.com:purge", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/services:batchGet?names=storage.googleapis.com&names=projects/"+project+"/services/pubsub.googleapis.com", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batchGet GET status=%d body=%s", rec.Code, rec.Body.String())
	}
	var batch map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &batch)
	services, _ := batch["services"].([]any)
	if len(services) < 2 {
		t.Fatalf("batchGet=%#v", batch)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/services:batchGet",
		bytes.NewReader([]byte(`{"names":["logging.googleapis.com"]}`)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batchGet POST status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/services:batchGet",
		bytes.NewReader([]byte(`{`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("batchGet bad json status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/widgets:batchGet", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad collection get status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/services:explode",
		bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown collection post status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/services:batchEnable",
		bytes.NewReader([]byte(`{`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("batchEnable bad json status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"?filter=state:ENABLED", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enabled filter status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServiceUsageNilPrincipalFunc(t *testing.T) {
	mux := http.NewServeMux()
	h := &serviceusage.Handler{}
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/services", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("nil principal status=%d", rec.Code)
	}
}
