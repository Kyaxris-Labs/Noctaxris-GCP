package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/dataflow"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/dns"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// dnsDataflowMux mounts Cloud DNS + Dataflow for focused httptest coverage.
func dnsDataflowMux(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureRoot("noctaxris-gcp-local", root); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	principalFrom := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	}
	eval := &authz.Evaluator{Policies: st}
	(&dns.Service{Store: st, Authz: eval}).Mount(mux, principalFrom)
	(&dataflow.Service{Store: st, Authz: eval}).Mount(mux, principalFrom)
	return mux, "noctaxris-gcp-local"
}

func TestCloudDNSViaServer(t *testing.T) {
	mux, project := dnsDataflowMux(t)
	base := "/dns/v1/projects/" + project + "/managedZones"
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(
		`{"name":"srv-zone","dnsName":"srv.example.","visibility":"private"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, base+"/srv-zone/rrsets", bytes.NewReader([]byte(
		`{"name":"a.srv.example.","type":"A","ttl":60,"rrdatas":["10.0.0.1"]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rrset status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDataflowViaServer(t *testing.T) {
	mux, project := dnsDataflowMux(t)
	base := "/v1b3/projects/" + project + "/locations/" + dataflow.DefaultLocation + "/jobs"
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(`{"name":"srv-job"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var job map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	if job["currentState"] != "JOB_STATE_RUNNING" {
		t.Fatalf("job=%#v", job)
	}
	id, _ := job["id"].(string)
	req = httptest.NewRequest(http.MethodGet, base+"/"+id, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	if job["currentState"] != "JOB_STATE_DONE" {
		t.Fatalf("expected DONE: %#v", job)
	}
}

func TestComputeEngineViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	project := cfg.ProjectID
	zone := "us-central1-a"
	base := "/compute/v1/projects/" + project + "/zones/" + zone + "/instances"
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(
		`{"name":"srv-vm","machineType":"zones/us-central1-a/machineTypes/e2-micro"}`,
	)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/srv-vm", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if inst["status"] != "RUNNING" {
		t.Fatalf("instance=%#v", inst)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/srv-vm/reset", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", rec.Code, rec.Body.String())
	}

	netBase := "/compute/v1/projects/" + project + "/global/networks"
	req = httptest.NewRequest(http.MethodPost, netBase, bytes.NewReader([]byte(`{"name":"srv-vpc","autoCreateSubnetworks":false}`)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("network status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, netBase+"/srv-vpc", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get network status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestComputeEngineAuthzUnauthenticated(t *testing.T) {
	srv, cfg := testServer(t)
	path := "/compute/v1/projects/" + cfg.ProjectID + "/zones/us-central1-a/instances"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBigtableAndMemorystore(t *testing.T) {
	srv, cfg := testServer(t)
	h := srv.Handler()

	auth := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	}

	btBody := `{"instanceId":"bt1","instance":{"displayName":"BT"},"clusters":{"c1":{"location":"us-central1-b","serveNodes":1}}}`
	req := httptest.NewRequest(http.MethodPost, "/v2/projects/"+cfg.ProjectID+"/instances", bytes.NewReader([]byte(btBody)))
	auth(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bigtable instance: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/projects/"+cfg.ProjectID+"/instances/bt1", nil)
	auth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get bigtable instance: %d %s", rec.Code, rec.Body.String())
	}

	msBody := `{"tier":"BASIC","memorySizeGb":1}`
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/locations/us-central1/instances?instanceId=redis1", bytes.NewReader([]byte(msBody)))
	auth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create memorystore: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+cfg.ProjectID+"/locations/us-central1/instances/redis1", nil)
	auth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get memorystore: %d %s", rec.Code, rec.Body.String())
	}
}
