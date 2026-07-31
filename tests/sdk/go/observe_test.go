package sdk_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestListLoggingSinksSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v2/projects/" + project + "/sinks"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list Logging sinks status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode Logging sinks: %v body=%s", err, body)
	}
	if _, ok := parsed["sinks"]; !ok {
		t.Fatalf("missing sinks field body=%s", body)
	}
}

func TestListMonitoringAlertPoliciesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v3/projects/" + project + "/alertPolicies"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list Monitoring alertPolicies status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode Monitoring alertPolicies: %v body=%s", err, body)
	}
	if _, ok := parsed["alertPolicies"]; !ok {
		t.Fatalf("missing alertPolicies field body=%s", body)
	}
}

func TestListDataflowJobsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1b3/projects/" + project + "/locations/us-central1/jobs"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list dataflow jobs status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode dataflow jobs: %v body=%s", err, body)
	}
	if _, ok := parsed["jobs"]; !ok {
		t.Fatalf("missing jobs field body=%s", body)
	}
}

func TestVertexAIGenerateContentSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/us-central1/publishers/google/models/gemini-1.5-flash:generateContent"
	status, body := doJSON(t, http.MethodPost, path, token, map[string]any{
		"contents": []any{
			map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": "sdk-smoke"}},
			},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("generateContent status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode generateContent: %v body=%s", err, body)
	}
	if _, ok := parsed["candidates"]; !ok {
		t.Fatalf("missing candidates field body=%s", body)
	}
}
