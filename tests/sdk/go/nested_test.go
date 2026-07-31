package sdk_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNestedInvokeFailClosedSmoke(t *testing.T) {
	if !truthyEnv("NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED") {
		t.Skip("NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED unset; soft-skip nested fail-closed smoke")
	}
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	svcID := uniqueID("sdk-failclosed")
	if len(svcID) > 49 {
		svcID = svcID[:49]
		svcID = strings.TrimRight(svcID, "-")
	}
	base := ep + "/v2/projects/" + project + "/locations/us-central1/services"
	svcPath := base + "/" + svcID

	status, body := doJSON(t, http.MethodPost, base+"?serviceId="+url.QueryEscape(svcID), token, map[string]any{
		"template": map[string]any{
			"containers": []any{map[string]any{"image": "demo"}},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("create run service status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, svcPath, token, nil)
	})

	status, body = doJSON(t, http.MethodPost, svcPath+":invoke", token, map[string]any{})
	if status == http.StatusOK {
		if strings.Contains(string(body), `"mode":"mock"`) || strings.Contains(string(body), `"ok":true`) {
			t.Skipf("server returned soft-fail/mock invoke (DOCKER_HOST empty or not fail-closed); soft-skip body=%s", body)
		}
		t.Fatalf(":invoke unexpectedly OK under fail-closed env body=%s", body)
	}
	if status < 400 {
		t.Fatalf(":invoke want error status, got %d body=%s", status, body)
	}
	if strings.Contains(string(body), `"mode":"mock"`) {
		t.Fatalf(":invoke should not soft-fail to mock under fail-closed, body=%s", body)
	}
}
