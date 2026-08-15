package compute_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComputeListGetPatchNetworkSubnetFirewallInstance(t *testing.T) {
	mux, _, project := mountCompute(t)
	netBase := "/compute/v1/projects/" + project + "/global/networks"
	req := httptest.NewRequest(http.MethodPost, netBase, bytes.NewReader([]byte(
		`{"name":"vpc2","autoCreateSubnetworks":false,"description":"d1"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert net: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, netBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list nets: %d %s", rec.Code, rec.Body.String())
	}
	var listed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	items, _ := listed["items"].([]any)
	if len(items) < 1 {
		t.Fatalf("nets=%#v", listed)
	}

	req = httptest.NewRequest(http.MethodGet, netBase+"/vpc2", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get net: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, netBase+"/vpc2", bytes.NewReader([]byte(`{"description":"d2","mtu":1460}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch net: %d %s", rec.Code, rec.Body.String())
	}

	subBase := "/compute/v1/projects/" + project + "/regions/us-central1/subnetworks"
	req = httptest.NewRequest(http.MethodPost, subBase, bytes.NewReader([]byte(
		`{"name":"sn2","network":"projects/`+project+`/global/networks/vpc2","ipCidrRange":"10.9.0.0/24"}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert subnet: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, subBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list subnets: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPatch, subBase+"/sn2", bytes.NewReader([]byte(`{"description":"subnet-patched"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch subnet: %d %s", rec.Code, rec.Body.String())
	}

	fwBase := "/compute/v1/projects/" + project + "/global/firewalls"
	req = httptest.NewRequest(http.MethodPost, fwBase, bytes.NewReader([]byte(
		`{"name":"fw2","network":"projects/`+project+`/global/networks/vpc2","allowed":[{"IPProtocol":"tcp","ports":["80"]}]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert fw: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, fwBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list fw: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPatch, fwBase+"/fw2", bytes.NewReader([]byte(
		`{"allowed":[{"IPProtocol":"tcp","ports":["443"]}],"description":"https"}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch fw: %d %s", rec.Code, rec.Body.String())
	}

	zone := "us-central1-a"
	instBase := "/compute/v1/projects/" + project + "/zones/" + zone + "/instances"
	req = httptest.NewRequest(http.MethodPost, instBase, bytes.NewReader([]byte(
		`{"name":"vm2","machineType":"zones/`+zone+`/machineTypes/e2-micro","networkInterfaces":[{"network":"global/networks/vpc2"}]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert vm: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, instBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list vms: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPatch, instBase+"/vm2", bytes.NewReader([]byte(
		`{"labels":{"env":"lab"},"metadata":{"items":[{"key":"a","value":"b"}]}}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch vm: %d %s", rec.Code, rec.Body.String())
	}
	var vm map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &vm)
	if vm["name"] != "vm2" {
		t.Fatalf("vm=%#v", vm)
	}

	req = httptest.NewRequest(http.MethodPost, instBase+"/vm2/reset", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, instBase+"/vm2/start", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, instBase+"/vm2", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete vm: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, fwBase+"/fw2", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = rec
	req = httptest.NewRequest(http.MethodDelete, subBase+"/sn2", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	req = httptest.NewRequest(http.MethodDelete, netBase+"/vpc2", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
}
