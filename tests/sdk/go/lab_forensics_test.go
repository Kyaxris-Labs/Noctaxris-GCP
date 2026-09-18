package sdk_test

import (
	"net/http"
	"testing"
)

func TestLabClockFreezeSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	requireLabForensics(t)

	status, body := doJSON(t, http.MethodPost, ep+"/_noctaxris-gcp/lab/clock:freeze", token, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("clock freeze status=%d body=%s", status, body)
	}
	status, body = doJSON(t, http.MethodPost, ep+"/_noctaxris-gcp/lab/clock:unfreeze", token, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("clock unfreeze status=%d body=%s", status, body)
	}
}

func TestLogsInjectDeniedOrWritesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	requireLogsInject(t)

	status, body := doJSON(t, http.MethodPost, ep+"/_noctaxris-gcp/lab/logs:inject", token, map[string]any{
		"projectId": projectID(),
		"entries": []any{
			map[string]any{
				"logName":     "projects/" + projectID() + "/logs/lab-sdk",
				"textPayload": "sdk-inject",
				"resource":    map[string]any{"type": "global"},
			},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("logs inject status=%d body=%s", status, body)
	}
}
