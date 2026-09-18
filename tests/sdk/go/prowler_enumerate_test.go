package sdk_test

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"testing"
)

func TestProwlerGCPEnumerateSmoke(t *testing.T) {
	if _, err := exec.LookPath("prowler"); err != nil {
		t.Skip("prowler not installed; soft-skip Prowler enumerate")
	}
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	type row struct {
		method, path string
		wantKey      string
	}
	rows := []row{
		{http.MethodGet, "/v1/projects", "projects"},
		{http.MethodGet, "/v1/projects/" + project + "/services", "services"},
		{http.MethodGet, "/compute/v1/projects/" + project + "/regions", "items"},
		{http.MethodGet, "/storage/v1/b?project=" + project, "items"},
		{http.MethodGet, "/v1/projects/" + project + "/serviceAccounts", "accounts"},
	}
	for _, r := range rows {
		status, raw := doJSON(t, r.method, ep+r.path, token, nil)
		if status != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", r.method, r.path, status, raw)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("%s %s json: %v body=%s", r.method, r.path, err, raw)
		}
		if _, ok := body[r.wantKey]; !ok {
			t.Fatalf("%s %s missing %q in %s", r.method, r.path, r.wantKey, raw)
		}
	}
}
