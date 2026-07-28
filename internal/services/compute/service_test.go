package compute_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountCompute(t *testing.T) (*http.ServeMux, *store.Store, string) {
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
	(&compute.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, principalFrom)
	return mux, st, "noctaxris-gcp-local"
}

func TestInstanceCRUDAndStatusTheatre(t *testing.T) {
	mux, _, project := mountCompute(t)
	zone := "us-central1-a"
	base := "/compute/v1/projects/" + project + "/zones/" + zone + "/instances"
	body := `{"name":"lab-vm","machineType":"zones/us-central1-a/machineTypes/e2-micro","networkInterfaces":[{"network":"global/networks/default"}]}`
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	if op["status"] != "DONE" || op["operationType"] != "insert" {
		t.Fatalf("insert op=%#v", op)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lab-vm", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if inst["status"] != "RUNNING" || inst["name"] != "lab-vm" {
		t.Fatalf("instance=%#v", inst)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/lab-vm/stop", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/lab-vm", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if inst["status"] != "TERMINATED" {
		t.Fatalf("after stop: %#v", inst)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/lab-vm/start", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/lab-vm", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if inst["status"] != "RUNNING" {
		t.Fatalf("after start: %#v", inst)
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("list=%#v", list)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/lab-vm", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestNetworkSubnetFirewallCRUD(t *testing.T) {
	mux, _, project := mountCompute(t)
	netBase := "/compute/v1/projects/" + project + "/global/networks"
	req := httptest.NewRequest(http.MethodPost, netBase, bytes.NewReader([]byte(
		`{"name":"lab-vpc","autoCreateSubnetworks":false,"description":"lab"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("network insert status=%d body=%s", rec.Code, rec.Body.String())
	}

	subBase := "/compute/v1/projects/" + project + "/regions/us-central1/subnetworks"
	req = httptest.NewRequest(http.MethodPost, subBase, bytes.NewReader([]byte(
		`{"name":"lab-subnet","network":"projects/`+project+`/global/networks/lab-vpc","ipCidrRange":"10.8.0.0/24"}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subnet insert status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, subBase+"/lab-subnet", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("10.8.0.0/24")) {
		t.Fatalf("get subnet status=%d body=%s", rec.Code, rec.Body.String())
	}

	fwBase := "/compute/v1/projects/" + project + "/global/firewalls"
	req = httptest.NewRequest(http.MethodPost, fwBase, bytes.NewReader([]byte(
		`{"name":"allow-internal","network":"projects/`+project+`/global/networks/lab-vpc","allowed":[{"IPProtocol":"tcp","ports":["22"]}]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("firewall insert status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, fwBase+"/allow-internal", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get firewall status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, fwBase+"/allow-internal", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete firewall status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, subBase+"/lab-subnet", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete subnet status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, netBase+"/lab-vpc", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete network status=%d", rec.Code)
	}
}

func TestComputeAuthzFailClosed(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@x"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	principalFrom := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	}
	(&compute.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, principalFrom)
	req := httptest.NewRequest(http.MethodGet, "/compute/v1/projects/noctaxris-gcp-local/zones/us-central1-a/instances", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
