package cloudrun_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
)

func TestKnativeCreateReplaceDeleteAndRelated(t *testing.T) {
	mux := mountCloudRun(t, nil)
	project := "noctaxris-gcp-local"
	base := "/apis/serving.knative.dev/v1/namespaces/" + project
	host := cloudrun.DefaultLocation + "-run.googleapis.com"

	createBody := `{
		"apiVersion":"serving.knative.dev/v1",
		"kind":"Service",
		"metadata":{"name":"kn-mutate"},
		"spec":{
			"template":{
				"metadata":{"annotations":{"labResponseBody":"{\"from\":\"knative\"}"}},
				"spec":{
					"serviceAccountName":"runtime@noctaxris-gcp-local.iam.gserviceaccount.com",
					"containers":[{"image":"gcr.io/demo/app:1"}]
				}
			},
			"traffic":[{"percent":100,"latestRevision":true}]
		}
	}`
	req := httptest.NewRequest(http.MethodPost, base+"/services", bytes.NewReader([]byte(createBody)))
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/services", bytes.NewReader([]byte(createBody)))
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create status=%d body=%s", rec.Code, rec.Body.String())
	}

	badName := `{"metadata":{},"spec":{"template":{"spec":{"containers":[{"image":"x"}]}}}}`
	req = httptest.NewRequest(http.MethodPost, base+"/services", bytes.NewReader([]byte(badName)))
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name status=%d body=%s", rec.Code, rec.Body.String())
	}

	replaceBody := `{
		"metadata":{"name":"kn-mutate"},
		"spec":{
			"template":{"spec":{"containers":[{"image":"gcr.io/demo/app:2"}]}},
			"traffic":[{"percent":100,"latestRevision":true}]
		}
	}`
	req = httptest.NewRequest(http.MethodPut, base+"/services/kn-mutate", bytes.NewReader([]byte(replaceBody)))
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace status=%d body=%s", rec.Code, rec.Body.String())
	}

	mismatch := `{"metadata":{"name":"other"},"spec":{"template":{"spec":{"containers":[{"image":"x"}]}}}}`
	req = httptest.NewRequest(http.MethodPut, base+"/services/kn-mutate", bytes.NewReader([]byte(mismatch)))
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("name mismatch status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/configurations", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list configurations status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cfgList map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &cfgList)
	if cfgList["kind"] != "ConfigurationList" {
		t.Fatalf("cfgList=%#v", cfgList)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/configurations/kn-mutate", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get configuration status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/routes", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list routes status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/routes/kn-mutate", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get route status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/revisions/kn-mutate-00001", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get revision status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/revisions/kn-mutate-00001", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("delete revision status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/services/kn-mutate", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete service status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/services/kn-mutate", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/configurations/missing", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing configuration status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/routes/missing", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing route status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/revisions/missing-00001", nil)
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing revision status=%d", rec.Code)
	}
}
