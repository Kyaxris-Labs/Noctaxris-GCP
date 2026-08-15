package monitoring_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestMonitoringFullCRUD(t *testing.T) {
	mux := mountMonitoring(t, nil)
	project := "noctaxris-gcp-local"
	base := "/v3/projects/" + project

	descType := "custom.googleapis.com/lab/cov"
	desc := `{"type":"` + descType + `","metricKind":"GAUGE","valueType":"DOUBLE","displayName":"cov"}`
	req := httptest.NewRequest(http.MethodPost, base+"/metricDescriptors", bytes.NewReader([]byte(desc)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create descriptor: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/metricDescriptors", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list descriptors: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/metricDescriptors/"+url.PathEscape(descType), nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get descriptor: %d %s", rec.Code, rec.Body.String())
	}

	ts := `{"timeSeries":[{"metric":{"type":"` + descType + `"},"points":[{"value":{"doubleValue":2.5},"interval":{"endTime":"2026-01-01T00:00:00Z"}}]}]}`
	req = httptest.NewRequest(http.MethodPost, base+"/timeSeries", bytes.NewReader([]byte(ts)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create ts: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/timeSeries?filter="+url.QueryEscape(`metric.type="`+descType+`"`), nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list ts: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, base+"/timeSeries:delete",
		bytes.NewReader([]byte(`{"filter":"metric.type=\"`+descType+`\""}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete ts: %d %s", rec.Code, rec.Body.String())
	}

	policy := `{"displayName":"cov-pol","enabled":true,"conditions":[{"displayName":"c1"}]}`
	req = httptest.NewRequest(http.MethodPost, base+"/alertPolicies?alertPolicyId=cov1", bytes.NewReader([]byte(policy)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create policy: %d %s", rec.Code, rec.Body.String())
	}
	polName := "projects/" + project + "/alertPolicies/cov1"
	req = httptest.NewRequest(http.MethodGet, base+"/alertPolicies/cov1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get policy: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/alertPolicies", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list policies: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, base+"/alertPolicies/cov1",
		bytes.NewReader([]byte(`{"displayName":"cov-pol-2","enabled":false,"conditions":[]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch policy: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/alertPolicies/cov1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete policy: %d %s", rec.Code, rec.Body.String())
	}
	_ = polName
}
