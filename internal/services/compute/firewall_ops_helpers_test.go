package compute_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFirewallPatchDeleteResetAndOps(t *testing.T) {
	mux, _, project := mountCompute(t)
	netBase := "/compute/v1/projects/" + project + "/global/networks"
	req := httptest.NewRequest(http.MethodPost, netBase, bytes.NewReader([]byte(`{"name":"vpc-fw","autoCreateSubnetworks":false}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("net: %d %s", rec.Code, rec.Body.String())
	}

	fwBase := "/compute/v1/projects/" + project + "/global/firewalls"
	req = httptest.NewRequest(http.MethodPost, fwBase, bytes.NewReader([]byte(
		`{"name":"fw-ops","network":"projects/`+project+`/global/networks/vpc-fw","allowed":[{"IPProtocol":"tcp","ports":["80-90","443"]}],"denied":[{"IPProtocol":"udp","ports":["53"]}],"sourceRanges":["10.0.0.0/8"]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert fw: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, fwBase+"/fw-ops:validate", bytes.NewReader([]byte(
		`{"sourceIp":"10.1.2.3","protocol":"tcp","port":"85"}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("validate allow: %d %s", rec.Code, rec.Body.String())
	}
	var result map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if result["allowed"] != true {
		t.Fatalf("expected allow: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, fwBase+"/fw-ops:validate", bytes.NewReader([]byte(
		`{"sourceIp":"10.1.2.3","protocol":"udp","port":53}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if result["allowed"] != false || result["action"] != "DENY" {
		t.Fatalf("expected deny: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, fwBase+"/fw-ops:validate", bytes.NewReader([]byte(`{`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("validate bad json: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, fwBase+"/missing:validate", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("validate missing: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, fwBase+"/fw-ops:testIamPermissions", bytes.NewReader([]byte(
		`{"permissions":["compute.firewalls.get","compute.firewalls.delete",""]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("testIam: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, fwBase+"/fw-ops", bytes.NewReader([]byte(
		`{"description":"patched","allowed":[{"IPProtocol":"tcp","ports":["8080"]}]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch fw: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, fwBase+"/missing", bytes.NewReader([]byte(`{"description":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch missing: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, fwBase+"/fw-ops", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete fw: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, fwBase+"/fw-ops", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete: %d", rec.Code)
	}

	zone := "us-central1-a"
	instBase := "/compute/v1/projects/" + project + "/zones/" + zone + "/instances"
	req = httptest.NewRequest(http.MethodPost, instBase, bytes.NewReader([]byte(
		`{"name":"vm-reset","machineType":"zones/`+zone+`/machineTypes/e2-micro","networkInterfaces":[{"network":"global/networks/vpc-fw"}],"disks":[{"boot":true,"initializeParams":{"sourceImage":"projects/debian-cloud/global/images/family/debian-12"}}]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert vm: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, instBase+"/vm-reset/reset", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, instBase+"/missing/stop", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stop missing: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, instBase+"/vm-reset/unknown", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown action: %d", rec.Code)
	}

	netPatch := "/compute/v1/projects/" + project + "/global/networks/vpc-fw"
	req = httptest.NewRequest(http.MethodPatch, netPatch, bytes.NewReader([]byte(`{"description":"n"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch network: %d %s", rec.Code, rec.Body.String())
	}

	subBase := "/compute/v1/projects/" + project + "/regions/us-central1/subnetworks"
	req = httptest.NewRequest(http.MethodPost, subBase, bytes.NewReader([]byte(
		`{"name":"subnet-a","network":"projects/`+project+`/global/networks/vpc-fw","ipCidrRange":"10.9.0.0/24"}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert subnet: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, subBase+"/subnet-a", bytes.NewReader([]byte(`{"description":"s"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch subnet: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/operations/op-done", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("global op: %d %s", rec.Code, rec.Body.String())
	}
}
