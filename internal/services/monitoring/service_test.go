package monitoring_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/monitoring"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountMonitoring(t *testing.T, principal func(*http.Request) (authn.Principal, bool)) *http.ServeMux {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if principal == nil {
		principal = func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: root, IsRoot: true}, true
		}
	}
	mux := http.NewServeMux()
	svc := &monitoring.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, principal)
	return mux
}

func TestMonitoringDescriptorTimeSeriesAlertPolicy(t *testing.T) {
	mux := mountMonitoring(t, nil)
	project := "noctaxris-gcp-local"
	base := "/v3/projects/" + project

	desc := `{"type":"custom.googleapis.com/lab/wp2","metricKind":"GAUGE","valueType":"DOUBLE"}`
	req := httptest.NewRequest(http.MethodPost, base+"/metricDescriptors", bytes.NewReader([]byte(desc)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create descriptor status=%d body=%s", rec.Code, rec.Body.String())
	}

	ts := `{"timeSeries":[{"metric":{"type":"custom.googleapis.com/lab/wp2"},"points":[{"value":{"doubleValue":1.5}}]}]}`
	req = httptest.NewRequest(http.MethodPost, base+"/timeSeries", bytes.NewReader([]byte(ts)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create time series status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/timeSeries?filter=metric.type=\"custom.googleapis.com/lab/wp2\"", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list time series status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		TimeSeries []map[string]any `json:"timeSeries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.TimeSeries) != 1 {
		t.Fatalf("timeSeries=%#v", listResp.TimeSeries)
	}

	policy := `{"displayName":"lab","enabled":true,"conditions":[{"displayName":"c1"}]}`
	req = httptest.NewRequest(http.MethodPost, base+"/alertPolicies?alertPolicyId=pol1", bytes.NewReader([]byte(policy)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create alert policy status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMonitoringAuthzDenyNonRoot(t *testing.T) {
	mux := mountMonitoring(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodGet, "/v3/projects/noctaxris-gcp-local/metricDescriptors", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
