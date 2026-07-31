package sdk_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestListComputeInstancesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/compute/v1/projects/" + project + "/zones/us-central1-a/instances"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list compute instances status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode compute instances: %v body=%s", err, body)
	}
	if kind, _ := parsed["kind"].(string); kind != "compute#instanceList" {
		t.Fatalf("kind=%v want compute#instanceList body=%s", parsed["kind"], body)
	}
	if _, ok := parsed["items"]; !ok {
		t.Fatalf("missing items field body=%s", body)
	}
}

func TestListCloudRunServicesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v2/projects/" + project + "/locations/us-central1/services"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list run services status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode run services: %v body=%s", err, body)
	}
	if _, ok := parsed["services"]; !ok {
		t.Fatalf("missing services field body=%s", body)
	}
}

func TestListArtifactRegistryRepositoriesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/us-central1/repositories"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list repositories status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode repositories: %v body=%s", err, body)
	}
	if _, ok := parsed["repositories"]; !ok {
		t.Fatalf("missing repositories field body=%s", body)
	}
}

func TestListWorkflowsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/us-central1/workflows"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list workflows status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode workflows: %v body=%s", err, body)
	}
	if _, ok := parsed["workflows"]; !ok {
		t.Fatalf("missing workflows field body=%s", body)
	}
}

func TestGetAppEngineAppSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	status, body := doJSON(t, http.MethodGet, ep+"/v1/apps/"+project, token, nil)
	if status == http.StatusNotFound {
		t.Skip("App Engine app not created; soft-skip get app smoke")
	}
	if status != http.StatusOK {
		t.Fatalf("get app status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode app: %v body=%s", err, body)
	}
	if got, _ := parsed["id"].(string); got != "" && got != project {
		t.Fatalf("app id=%v want %s body=%s", parsed["id"], project, body)
	}
}

func TestListCloudBuildBuildsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/builds"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list Cloud Build builds status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode Cloud Build builds: %v body=%s", err, body)
	}
	if _, ok := parsed["builds"]; !ok {
		t.Fatalf("missing builds field body=%s", body)
	}
}

func TestListCloudFunctionsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v2/projects/" + project + "/locations/us-central1/functions"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list Cloud Functions status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode Cloud Functions: %v body=%s", err, body)
	}
	if _, ok := parsed["functions"]; !ok {
		t.Fatalf("missing functions field body=%s", body)
	}
}

func TestListCloudSchedulerJobsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/us-central1/jobs"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list Scheduler jobs status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode Scheduler jobs: %v body=%s", err, body)
	}
	if _, ok := parsed["jobs"]; !ok {
		t.Fatalf("missing jobs field body=%s", body)
	}
}

func TestListCloudTasksQueuesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v2/projects/" + project + "/locations/us-central1/queues"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list Cloud Tasks queues status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode Cloud Tasks queues: %v body=%s", err, body)
	}
	if _, ok := parsed["queues"]; !ok {
		t.Fatalf("missing queues field body=%s", body)
	}
}

func TestListEventarcChannelsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/us-central1/channels"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list Eventarc channels status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode Eventarc channels: %v body=%s", err, body)
	}
	if _, ok := parsed["channels"]; !ok {
		t.Fatalf("missing channels field body=%s", body)
	}
}
