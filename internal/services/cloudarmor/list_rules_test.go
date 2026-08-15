package cloudarmor_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloudArmorListAndRulesWithoutDefault(t *testing.T) {
	mux, project := armorMux(t)
	base := "/compute/v1/projects/" + project + "/global/securityPolicies"

	body := `{"name":"rules-only","rules":[{"priority":100,"action":"deny(403)","match":{"versionedExpr":"SRC_IPS_V1","config":{"srcIpRanges":["1.2.3.4/32"]}}}]}`
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert with rules: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	items, _ := list["items"].([]any)
	if len(items) < 1 {
		t.Fatalf("list=%#v", list)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/rules-only", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var pol map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &pol)
	rules, _ := pol["rules"].([]any)
	if len(rules) < 2 {
		t.Fatalf("expected appended default rule: %#v", pol)
	}
}
