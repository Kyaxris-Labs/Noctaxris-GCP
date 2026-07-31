package compute_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

func TestInstanceMetadataAndFirewallValidate(t *testing.T) {
	mux, _, project := mountCompute(t)
	zone := "us-central1-a"
	base := "/compute/v1/projects/" + project + "/zones/" + zone + "/instances"
	body := `{"name":"meta-vm","metadata":{"startup-script":"echo hi","env":"lab"}}`
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/meta-vm", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	meta, _ := inst["metadata"].(map[string]any)
	items, _ := meta["items"].([]any)
	if len(items) < 2 {
		t.Fatalf("metadata items=%#v", meta)
	}

	fwBase := "/compute/v1/projects/" + project + "/global/firewalls"
	req = httptest.NewRequest(http.MethodPost, fwBase, bytes.NewReader([]byte(
		`{"name":"allow-http","network":"global/networks/default","sourceRanges":["10.0.0.0/8"],"allowed":[{"IPProtocol":"tcp","ports":["80","443"]}],"denied":[{"IPProtocol":"tcp","ports":["22"]}]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fw insert status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, fwBase+"/allow-http:validate",
		bytes.NewReader([]byte(`{"sourceIp":"10.1.2.3","protocol":"tcp","port":80}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("validate allow status=%d body=%s", rec.Code, rec.Body.String())
	}
	var allow map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &allow)
	if allow["allowed"] != true || allow["action"] != "ALLOW" {
		t.Fatalf("allow result=%#v", allow)
	}

	req = httptest.NewRequest(http.MethodPost, fwBase+"/allow-http:validate",
		bytes.NewReader([]byte(`{"sourceIp":"10.1.2.3","protocol":"tcp","port":22}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var deny map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &deny)
	if deny["allowed"] != false || deny["action"] != "DENY" {
		t.Fatalf("deny result=%#v", deny)
	}

	req = httptest.NewRequest(http.MethodPost, fwBase+"/allow-http:validate",
		bytes.NewReader([]byte(`{"sourceIp":"203.0.113.1","protocol":"tcp","port":80}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var miss map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &miss)
	if miss["matched"] != false {
		t.Fatalf("miss result=%#v", miss)
	}

	req = httptest.NewRequest(http.MethodPost, fwBase+"/allow-http:testIamPermissions",
		bytes.NewReader([]byte(`{"permissions":["compute.firewalls.get","compute.firewalls.delete"]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("testIam status=%d body=%s", rec.Code, rec.Body.String())
	}
	var iam map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &iam)
	perms, _ := iam["permissions"].([]any)
	if len(perms) != 2 {
		t.Fatalf("permissions=%#v", iam)
	}
}

func TestComputeImagesListGetFamily(t *testing.T) {
	mux, _, project := mountCompute(t)
	base := "/compute/v1/projects/" + project + "/global/images"

	req := httptest.NewRequest(http.MethodGet, base, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list images status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	items, _ := list["items"].([]any)
	if len(items) < 3 {
		t.Fatalf("expected theatre images, got %#v", list)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/family/debian-12", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get family status=%d body=%s", rec.Code, rec.Body.String())
	}
	var img map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &img)
	if img["status"] != "READY" || img["family"] != "debian-12" {
		t.Fatalf("family image=%#v", img)
	}
	name, _ := img["name"].(string)
	if name == "" {
		t.Fatalf("missing name: %#v", img)
	}
	selfLink, _ := img["selfLink"].(string)
	if !strings.Contains(selfLink, "/global/images/"+name) {
		t.Fatalf("selfLink=%q", selfLink)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/"+name, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get image status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Public image project path (Terraform ResolveImage often uses debian-cloud).
	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/debian-cloud/global/images/family/ubuntu-2204-lts", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-project family status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &img)
	if img["family"] != "ubuntu-2204-lts" || img["status"] != "READY" {
		t.Fatalf("ubuntu family=%#v", img)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/family/no-such-family", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing family status=%d", rec.Code)
	}
}

func TestInstanceDisksAndBootDiskEcho(t *testing.T) {
	mux, _, project := mountCompute(t)
	zone := "us-central1-a"
	base := "/compute/v1/projects/" + project + "/zones/" + zone + "/instances"
	image := "projects/debian-cloud/global/images/family/debian-12"
	body := `{
		"name":"disk-vm",
		"machineType":"zones/us-central1-a/machineTypes/e2-micro",
		"disks":[{
			"boot":true,
			"autoDelete":true,
			"initializeParams":{"image":"` + image + `","size":20}
		}]
	}`
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/disk-vm", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	disks, ok := inst["disks"].([]any)
	if !ok || len(disks) != 1 {
		t.Fatalf("disks=%#v", inst["disks"])
	}
	d0, _ := disks[0].(map[string]any)
	if d0["boot"] != true {
		t.Fatalf("boot flag=%#v", d0)
	}
	ip, _ := d0["initializeParams"].(map[string]any)
	if ip == nil {
		t.Fatalf("initializeParams missing: %#v", d0)
	}
	if ip["image"] != image {
		t.Fatalf("image echo=%#v", ip["image"])
	}
	if ip["sourceImage"] != image {
		t.Fatalf("sourceImage=%#v", ip["sourceImage"])
	}
	if ip["size"] != float64(20) {
		t.Fatalf("size echo=%#v", ip["size"])
	}
}

func TestInstanceBootDiskFieldEcho(t *testing.T) {
	mux, _, project := mountCompute(t)
	zone := "us-central1-a"
	base := "/compute/v1/projects/" + project + "/zones/" + zone + "/instances"
	image := "projects/debian-cloud/global/images/debian-12-bookworm-v20240701"
	body := `{
		"name":"bootdisk-vm",
		"bootDisk":{
			"autoDelete":true,
			"initializeParams":{"sourceImage":"` + image + `","diskSizeGb":"15"}
		}
	}`
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/bootdisk-vm", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	disks, _ := inst["disks"].([]any)
	if len(disks) != 1 {
		t.Fatalf("expected canonical disks, got %#v", inst["disks"])
	}
	bootDisk, _ := inst["bootDisk"].(map[string]any)
	if bootDisk == nil {
		t.Fatalf("bootDisk missing: %#v", inst)
	}
	ip, _ := bootDisk["initializeParams"].(map[string]any)
	if ip["sourceImage"] != image || ip["diskSizeGb"] != "15" {
		t.Fatalf("bootDisk initializeParams=%#v", ip)
	}
}

func TestInstanceStableNumericID(t *testing.T) {
	mux, _, project := mountCompute(t)
	zone := "us-central1-a"
	base := "/compute/v1/projects/" + project + "/zones/" + zone + "/instances"
	body := `{"name":"stable-id-vm"}`
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert status=%d", rec.Code)
	}

	var id1 string
	for i := 0; i < 2; i++ {
		req = httptest.NewRequest(http.MethodGet, base+"/stable-id-vm", nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var inst map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &inst)
		id, _ := inst["id"].(string)
		if id == "" {
			t.Fatalf("missing id: %#v", inst)
		}
		if i == 0 {
			id1 = id
			continue
		}
		if id != id1 {
			t.Fatalf("id changed between reads: %q vs %q", id1, id)
		}
	}
}

func TestZoneOperationGETDone(t *testing.T) {
	mux, _, project := mountCompute(t)
	zone := "us-central1-a"
	base := "/compute/v1/projects/" + project + "/zones/" + zone + "/instances"
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(`{"name":"op-vm"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	opName, _ := op["name"].(string)
	if opName == "" {
		t.Fatalf("insert op missing name: %#v", op)
	}

	opURL := "/compute/v1/projects/" + project + "/zones/" + zone + "/operations/" + opName
	req = httptest.NewRequest(http.MethodGet, opURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("operation get status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	if op["status"] != "DONE" {
		t.Fatalf("operation status=%#v", op["status"])
	}
	if op["name"] != opName {
		t.Fatalf("operation name=%#v want %q", op["name"], opName)
	}
	if op["kind"] != "compute#operation" || op["operationType"] == nil || op["id"] == nil {
		t.Fatalf("poll shape missing doneOperation fields: %#v", op)
	}
}

func TestRegionAndGlobalOperationGETDone(t *testing.T) {
	mux, _, project := mountCompute(t)

	netReq := httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/networks",
		bytes.NewReader([]byte(`{"name":"op-vpc","autoCreateSubnetworks":false}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, netReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("network insert status=%d body=%s", rec.Code, rec.Body.String())
	}
	var netOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &netOp)
	netOpName, _ := netOp["name"].(string)
	if netOpName == "" {
		t.Fatalf("network op=%#v", netOp)
	}
	gURL := "/compute/v1/projects/" + project + "/global/operations/" + netOpName
	req := httptest.NewRequest(http.MethodGet, gURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("global op get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var gOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &gOp)
	if gOp["status"] != "DONE" || gOp["name"] != netOpName {
		t.Fatalf("global poll=%#v", gOp)
	}

	region := "us-central1"
	subReq := httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/regions/"+region+"/subnetworks",
		bytes.NewReader([]byte(`{"name":"op-subnet","network":"global/networks/op-vpc","ipCidrRange":"10.9.0.0/24"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, subReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("subnet insert status=%d body=%s", rec.Code, rec.Body.String())
	}
	var subOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &subOp)
	subOpName, _ := subOp["name"].(string)
	if subOpName == "" {
		t.Fatalf("subnet op=%#v", subOp)
	}
	rURL := "/compute/v1/projects/" + project + "/regions/" + region + "/operations/" + subOpName
	req = httptest.NewRequest(http.MethodGet, rURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("region op get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rOp)
	if rOp["status"] != "DONE" || rOp["name"] != subOpName {
		t.Fatalf("region poll=%#v", rOp)
	}
}

