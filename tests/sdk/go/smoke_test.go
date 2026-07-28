package sdk_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func endpoint(t *testing.T) string {
	t.Helper()
	ep := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_ENDPOINT"))
	if ep == "" {
		t.Skip("NOCTAXRIS_GCP_ENDPOINT unset; soft-skip live smoke")
	}
	return strings.TrimRight(ep, "/")
}

func requireReady(t *testing.T) string {
	t.Helper()
	ep := endpoint(t)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ep + "/_noctaxris-gcp/ready")
	if err != nil {
		t.Skipf("Noctaxris-GCP not reachable at %s: %v", ep, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("Noctaxris-GCP not ready at %s: status %d", ep, resp.StatusCode)
	}
	return ep
}

func TestReadyAndGetProjectSmoke(t *testing.T) {
	ep := requireReady(t)
	token := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN"))
	if token == "" {
		t.Skip("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN unset; soft-skip authenticated smoke")
	}
	project := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_PROJECT"))
	if project == "" {
		project = "noctaxris-gcp-local"
	}

	req, err := http.NewRequest(http.MethodGet, ep+"/v3/projects/"+project, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get project status=%d body=%s", resp.StatusCode, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode project: %v body=%s", err, body)
	}
	if got, _ := parsed["projectId"].(string); got != project {
		t.Fatalf("projectId=%v want %s body=%s", parsed["projectId"], project, body)
	}
}

func TestListBucketsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN"))
	if token == "" {
		t.Skip("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN unset; soft-skip authenticated smoke")
	}
	project := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_PROJECT"))
	if project == "" {
		project = "noctaxris-gcp-local"
	}

	req, err := http.NewRequest(http.MethodGet, ep+"/storage/v1/b?project="+project, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("list buckets: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list buckets status=%d body=%s", resp.StatusCode, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode buckets: %v body=%s", err, body)
	}
	if kind, _ := parsed["kind"].(string); kind != "storage#buckets" {
		t.Fatalf("kind=%v want storage#buckets body=%s", parsed["kind"], body)
	}
}

func TestListCloudRunServicesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN"))
	if token == "" {
		t.Skip("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN unset; soft-skip authenticated smoke")
	}
	project := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_PROJECT"))
	if project == "" {
		project = "noctaxris-gcp-local"
	}

	path := ep + "/v2/projects/" + project + "/locations/us-central1/services"
	req, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("list run services: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list run services status=%d body=%s", resp.StatusCode, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode run services: %v body=%s", err, body)
	}
	if _, ok := parsed["services"]; !ok {
		t.Fatalf("missing services field body=%s", body)
	}
}
