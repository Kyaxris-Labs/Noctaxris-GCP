package compute_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComputeStopValidateFirewallImagesOps(t *testing.T) {
	mux, _, project := mountCompute(t)
	netBase := "/compute/v1/projects/" + project + "/global/networks"
	req := httptest.NewRequest(http.MethodPost, netBase, bytes.NewReader([]byte(`{"name":"vpc3","autoCreateSubnetworks":false}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("net: %d %s", rec.Code, rec.Body.String())
	}

	fwBase := "/compute/v1/projects/" + project + "/global/firewalls"
	req = httptest.NewRequest(http.MethodPost, fwBase, bytes.NewReader([]byte(
		`{"name":"fw3","network":"projects/`+project+`/global/networks/vpc3","allowed":[{"IPProtocol":"tcp","ports":["22"]}],"sourceRanges":["0.0.0.0/0"]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fw: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, fwBase+"/fw3:validate", bytes.NewReader([]byte(
		`{"sourceIp":"8.8.8.8","protocol":"tcp","port":22}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("validate: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, fwBase+"/fw3:testIamPermissions", bytes.NewReader([]byte(
		`{"permissions":["compute.firewalls.get"]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("testIam: %d %s", rec.Code, rec.Body.String())
	}

	zone := "us-central1-a"
	instBase := "/compute/v1/projects/" + project + "/zones/" + zone + "/instances"
	req = httptest.NewRequest(http.MethodPost, instBase, bytes.NewReader([]byte(
		`{"name":"vm3","machineType":"zones/`+zone+`/machineTypes/e2-micro","networkInterfaces":[{"network":"global/networks/vpc3"}]}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("vm: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, instBase+"/vm3:stop", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop colon: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, instBase+"/vm3/start", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, instBase+"/vm3", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get vm: %d", rec.Code)
	}

	imgBase := "/compute/v1/projects/" + project + "/global/images"
	req = httptest.NewRequest(http.MethodGet, imgBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list images: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, imgBase+"/family/debian-12", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = rec.Code
	req = httptest.NewRequest(http.MethodGet, imgBase+"/debian-12", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = rec.Code

	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/operations/op-x", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = rec.Code
	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/regions/us-central1/operations/op-x", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = rec.Code
	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/zones/"+zone+"/operations/op-x", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = rec.Code
}
