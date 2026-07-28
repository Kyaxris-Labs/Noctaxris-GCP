package sdk_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

func requireToken(t *testing.T) string {
	t.Helper()
	token := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN"))
	if token == "" {
		t.Skip("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN unset; soft-skip authenticated smoke")
	}
	return token
}

func projectID() string {
	project := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_PROJECT"))
	if project == "" {
		return "noctaxris-gcp-local"
	}
	return project
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func doJSON(t *testing.T, method, rawURL, token string, body any) (int, []byte) {
	t.Helper()
	status, raw, err := doJSONErr(method, rawURL, token, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawURL, err)
	}
	return status, raw
}

func doJSONErr(method, rawURL, token string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, rawURL, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, nil
}

func TestReadyAndGetProjectSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	status, body := doJSON(t, http.MethodGet, ep+"/v3/projects/"+project, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get project status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode project: %v body=%s", err, body)
	}
	if got, _ := parsed["projectId"].(string); got != project {
		t.Fatalf("projectId=%v want %s body=%s", parsed["projectId"], project, body)
	}
}

func TestGetOrganizationSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)

	status, body := doJSON(t, http.MethodGet, ep+"/v3/organizations/noctaxris-gcp-org", token, nil)
	if status != http.StatusOK {
		t.Fatalf("get organization status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode organization: %v body=%s", err, body)
	}
	if got, _ := parsed["name"].(string); got != "organizations/noctaxris-gcp-org" {
		t.Fatalf("name=%v want organizations/noctaxris-gcp-org body=%s", parsed["name"], body)
	}
}

func TestListFoldersSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)

	q := url.Values{"parent": {"organizations/noctaxris-gcp-org"}}
	status, body := doJSON(t, http.MethodGet, ep+"/v3/folders?"+q.Encode(), token, nil)
	if status != http.StatusOK {
		t.Fatalf("list folders status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode folders: %v body=%s", err, body)
	}
	if _, ok := parsed["folders"]; !ok {
		t.Fatalf("missing folders field body=%s", body)
	}
}

func TestListBucketsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	status, body := doJSON(t, http.MethodGet, ep+"/storage/v1/b?project="+url.QueryEscape(project), token, nil)
	if status != http.StatusOK {
		t.Fatalf("list buckets status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode buckets: %v body=%s", err, body)
	}
	if kind, _ := parsed["kind"].(string); kind != "storage#buckets" {
		t.Fatalf("kind=%v want storage#buckets body=%s", parsed["kind"], body)
	}
}

func TestPubSubCreateAndListTopicsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	topicID := uniqueID("sdk-topic")
	topicPath := ep + "/v1/projects/" + project + "/topics/" + topicID

	status, body := doJSON(t, http.MethodPut, topicPath, token, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("create topic status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, topicPath, token, nil)
	})

	status, body = doJSON(t, http.MethodGet, ep+"/v1/projects/"+project+"/topics", token, nil)
	if status != http.StatusOK {
		t.Fatalf("list topics status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode topics: %v body=%s", err, body)
	}
	if _, ok := parsed["topics"]; !ok {
		t.Fatalf("missing topics field body=%s", body)
	}
}

func TestSecretManagerCreateAccessSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	secretID := uniqueID("sdk-secret")
	base := ep + "/v1/projects/" + project + "/secrets/" + secretID

	status, body := doJSON(t, http.MethodPost, ep+"/v1/projects/"+project+"/secrets?secretId="+url.QueryEscape(secretID), token, map[string]any{
		"replication": map[string]any{"automatic": map[string]any{}},
	})
	if status != http.StatusOK {
		t.Fatalf("create secret status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, base, token, nil)
	})

	payload := base64.StdEncoding.EncodeToString([]byte("sdk-smoke"))
	status, body = doJSON(t, http.MethodPost, base+":addVersion", token, map[string]any{
		"payload": map[string]any{"data": payload},
	})
	if status != http.StatusOK {
		t.Fatalf("addVersion status=%d body=%s", status, body)
	}

	status, body = doJSON(t, http.MethodGet, base+"/versions/latest:access", token, nil)
	if status != http.StatusOK {
		t.Fatalf("access secret status=%d body=%s", status, body)
	}
	var accessed map[string]any
	if err := json.Unmarshal(body, &accessed); err != nil {
		t.Fatalf("decode access: %v body=%s", err, body)
	}
	payloadObj, _ := accessed["payload"].(map[string]any)
	gotB64, _ := payloadObj["data"].(string)
	got, err := base64.StdEncoding.DecodeString(gotB64)
	if err != nil {
		t.Fatalf("decode payload data: %v body=%s", err, body)
	}
	if string(got) != "sdk-smoke" {
		t.Fatalf("payload=%q want sdk-smoke body=%s", got, body)
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

func TestListDNSManagedZonesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/dns/v1/projects/" + project + "/managedZones"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list managed zones status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode managed zones: %v body=%s", err, body)
	}
	if _, ok := parsed["managedZones"]; !ok {
		t.Fatalf("missing managedZones field body=%s", body)
	}
}

func TestListBigtableInstancesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v2/projects/" + project + "/instances"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list bigtable instances status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode bigtable instances: %v body=%s", err, body)
	}
	if _, ok := parsed["instances"]; !ok {
		t.Fatalf("missing instances field body=%s", body)
	}
}

func TestListMemorystoreInstancesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/us-central1/instances"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list memorystore instances status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode memorystore instances: %v body=%s", err, body)
	}
	if _, ok := parsed["instances"]; !ok {
		t.Fatalf("missing instances field body=%s", body)
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
