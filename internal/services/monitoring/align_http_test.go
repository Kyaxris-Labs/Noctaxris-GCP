package monitoring_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

func TestMonitoringAlignAndAuthz(t *testing.T) {
	mux := mountMonitoring(t, nil)
	project := "noctaxris-gcp-local"
	base := "/v3/projects/" + project
	descType := "custom.googleapis.com/lab/align"

	req := httptest.NewRequest(http.MethodPost, base+"/metricDescriptors",
		bytes.NewReader([]byte(`{"type":"`+descType+`","metricKind":"GAUGE","valueType":"DOUBLE"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("desc: %d %s", rec.Code, rec.Body.String())
	}
	for i, v := range []string{"1.0", "5.0", "3.0"} {
		end := "2026-01-01T00:00:0" + string(rune('0'+i)) + "Z"
		ts := `{"timeSeries":[{"metric":{"type":"` + descType + `"},"points":[{"value":{"doubleValue":` + v + `},"interval":{"endTime":"` + end + `"}}]}]}`
		req = httptest.NewRequest(http.MethodPost, base+"/timeSeries", bytes.NewReader([]byte(ts)))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("ts: %d %s", rec.Code, rec.Body.String())
		}
	}
	q := url.Values{}
	q.Set("filter", `metric.type="`+descType+`"`)
	q.Set("aggregation.perSeriesAligner", "ALIGN_SUM")
	req = httptest.NewRequest(http.MethodGet, base+"/timeSeries?"+q.Encode(), nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("align list: %d %s", rec.Code, rec.Body.String())
	}

	deny := mountMonitoring(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req = httptest.NewRequest(http.MethodGet, base+"/metricDescriptors", nil)
	rec = httptest.NewRecorder()
	deny.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("authz deny: %d %s", rec.Code, rec.Body.String())
	}
}
