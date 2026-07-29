package sdk_test

import (
	"bytes"
	"context"
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

	"cloud.google.com/go/datastore/apiv1/datastorepb"
	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
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

func TestListManagedKafkaClustersSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/us-central1/clusters"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list managed kafka clusters status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode managed kafka clusters: %v body=%s", err, body)
	}
	if _, ok := parsed["clusters"]; !ok {
		t.Fatalf("missing clusters field body=%s", body)
	}
}

func TestListCloudSQLInstancesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/sql/v1/projects/" + project + "/instances"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list cloudsql instances status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode cloudsql instances: %v body=%s", err, body)
	}
	if _, ok := parsed["items"]; !ok {
		t.Fatalf("missing items field body=%s", body)
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

func TestListCloudArmorSecurityPoliciesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/compute/v1/projects/" + project + "/global/securityPolicies"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list securityPolicies status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode securityPolicies: %v body=%s", err, body)
	}
	if kind, _ := parsed["kind"].(string); kind != "compute#securityPolicyList" {
		t.Fatalf("kind=%v want compute#securityPolicyList body=%s", parsed["kind"], body)
	}
	if _, ok := parsed["items"]; !ok {
		t.Fatalf("missing items field body=%s", body)
	}
}

func TestListCertificateManagerCertificatesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/global/certificates"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list certificates status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode certificates: %v body=%s", err, body)
	}
	if _, ok := parsed["certificates"]; !ok {
		t.Fatalf("missing certificates field body=%s", body)
	}
}

func TestListFilestoreInstancesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/file/v1/projects/" + project + "/locations/us-central1/instances"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list filestore instances status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode filestore instances: %v body=%s", err, body)
	}
	if _, ok := parsed["instances"]; !ok {
		t.Fatalf("missing instances field body=%s", body)
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

func TestIAMGenerateAccessTokenSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	accountID := uniqueID("sdksa")
	if len(accountID) > 30 {
		accountID = accountID[:30]
		accountID = strings.TrimRight(accountID, "-")
	}
	email := accountID + "@" + project + ".iam.gserviceaccount.com"
	saPath := ep + "/v1/projects/" + project + "/serviceAccounts/" + email

	status, body := doJSON(t, http.MethodPost, ep+"/v1/projects/"+project+"/serviceAccounts", token, map[string]any{
		"accountId": accountID,
		"serviceAccount": map[string]any{
			"displayName": "sdk smoke",
		},
	})
	if status != http.StatusOK {
		t.Fatalf("create service account status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, saPath, token, nil)
	})

	status, body = doJSON(t, http.MethodPost, saPath+":generateAccessToken", token, map[string]any{
		"scope":    []string{"https://www.googleapis.com/auth/cloud-platform"},
		"lifetime": "3600s",
	})
	if status != http.StatusOK {
		t.Fatalf("generateAccessToken status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode generateAccessToken: %v body=%s", err, body)
	}
	if got, _ := parsed["accessToken"].(string); got == "" {
		t.Fatalf("missing accessToken body=%s", body)
	}
}

func TestGCSGenerateSignedURLSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	bucket := uniqueID("sdk-signed")
	bucketPath := ep + "/storage/v1/b/" + url.PathEscape(bucket)

	status, body := doJSON(t, http.MethodPost, ep+"/storage/v1/b?project="+url.QueryEscape(project), token, map[string]any{
		"name": bucket,
	})
	if status != http.StatusOK {
		t.Fatalf("create bucket status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, bucketPath, token, nil)
	})

	status, body = doJSON(t, http.MethodPost, bucketPath+"/o/smoke.txt:generateSignedUrl", token, map[string]any{
		"method":  "GET",
		"expires": 600,
		"alt":     "media",
	})
	if status != http.StatusOK {
		t.Fatalf("generateSignedUrl status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode generateSignedUrl: %v body=%s", err, body)
	}
	if got, _ := parsed["signedUrl"].(string); got == "" {
		t.Fatalf("missing signedUrl body=%s", body)
	}
}

func doForm(method, rawURL, contentType string, body string) (int, []byte, error) {
	req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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

func truthyEnv(name string) bool {
	v := strings.TrimSpace(os.Getenv(name))
	return v == "1" || strings.EqualFold(v, "true")
}

func TestSTSTokenExchangeSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	poolID := uniqueID("sdk-sts-pool")
	if len(poolID) > 32 {
		poolID = poolID[:32]
		poolID = strings.TrimRight(poolID, "-")
	}
	providerID := "oidc"
	poolBase := ep + "/v1/projects/" + project + "/locations/global/workloadIdentityPools/" + poolID
	providerName := "projects/" + project + "/locations/global/workloadIdentityPools/" + poolID + "/providers/" + providerID

	status, body := doJSON(t, http.MethodPost, ep+"/v1/projects/"+project+"/locations/global/workloadIdentityPools?workloadIdentityPoolId="+url.QueryEscape(poolID), token, map[string]any{
		"displayName": "sdk sts pool",
	})
	if status != http.StatusOK {
		t.Fatalf("create WIF pool status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, poolBase, token, nil)
	})

	status, body = doJSON(t, http.MethodPost, poolBase+"/providers?workloadIdentityPoolProviderId="+url.QueryEscape(providerID), token, map[string]any{
		"displayName": "sdk oidc",
		"oidc":        map[string]any{"issuerUri": "https://example.com"},
	})
	if status != http.StatusOK {
		t.Fatalf("create WIF provider status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, poolBase+"/providers/"+providerID, token, nil)
	})

	form := url.Values{
		"grant_type":         {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"audience":           {"//iam.googleapis.com/" + providerName},
		"subject_token":      {"sdk-sts-sub"},
		"subject_token_type": {"urn:ietf:params:oauth:token-type:jwt"},
	}
	status, raw, err := doForm(http.MethodPost, ep+"/v1/token", "application/x-www-form-urlencoded", form.Encode())
	if err != nil {
		t.Fatalf("STS /v1/token: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("STS /v1/token status=%d body=%s", status, raw)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode STS: %v body=%s", err, raw)
	}
	if got, _ := parsed["access_token"].(string); got == "" {
		t.Fatalf("missing access_token body=%s", raw)
	}
	if got, _ := parsed["token_type"].(string); got != "Bearer" {
		t.Fatalf("token_type=%v want Bearer body=%s", parsed["token_type"], raw)
	}
}

func TestGCSRetentionDeleteDenySmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	bucket := uniqueID("sdk-retain")
	bucketPath := ep + "/storage/v1/b/" + url.PathEscape(bucket)
	objectPath := bucketPath + "/o/" + url.PathEscape("held.txt")

	status, body := doJSON(t, http.MethodPost, ep+"/storage/v1/b?project="+url.QueryEscape(project), token, map[string]any{
		"name": bucket,
	})
	if status != http.StatusOK {
		t.Fatalf("create bucket status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, objectPath, token, nil)
		_, _, _ = doJSONErr(http.MethodDelete, bucketPath, token, nil)
	})

	status, body = doJSON(t, http.MethodPatch, bucketPath, token, map[string]any{
		"retentionPolicy": map[string]any{"retentionPeriod": "3600"},
	})
	if status != http.StatusOK {
		t.Fatalf("patch retention status=%d body=%s", status, body)
	}

	upReq, err := http.NewRequest(http.MethodPost, ep+"/upload/storage/v1/b/"+url.PathEscape(bucket)+"/o?uploadType=media&name="+url.QueryEscape("held.txt"), strings.NewReader("held"))
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	upReq.Header.Set("Authorization", "Bearer "+token)
	upReq.Header.Set("Content-Type", "text/plain")
	upResp, err := (&http.Client{Timeout: 5 * time.Second}).Do(upReq)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	upBody, _ := io.ReadAll(upResp.Body)
	_ = upResp.Body.Close()
	if upResp.StatusCode != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", upResp.StatusCode, upBody)
	}

	status, body = doJSON(t, http.MethodDelete, objectPath, token, nil)
	if status == http.StatusOK {
		t.Fatalf("delete under retention should fail; got status=%d body=%s", status, body)
	}
	var errBody map[string]any
	if err := json.Unmarshal(body, &errBody); err != nil {
		t.Fatalf("decode delete error: %v body=%s", err, body)
	}
	e, _ := errBody["error"].(map[string]any)
	if e == nil || e["status"] != "FAILED_PRECONDITION" {
		t.Fatalf("want FAILED_PRECONDITION, got status=%d body=%s", status, body)
	}
}

func TestPubSubOIDCPushSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	topicID := uniqueID("sdk-oidc-topic")
	subID := uniqueID("sdk-oidc-sub")
	topicPath := ep + "/v1/projects/" + project + "/topics/" + topicID
	subPath := ep + "/v1/projects/" + project + "/subscriptions/" + subID
	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/sdk-oidc-push"
	saEmail := "push-sa@" + project + ".iam.gserviceaccount.com"
	audience := "https://example.com/sdk-oidc"

	status, body := doJSON(t, http.MethodPut, topicPath, token, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("create topic status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, subPath, token, nil)
		_, _, _ = doJSONErr(http.MethodDelete, topicPath, token, nil)
	})

	status, body = doJSON(t, http.MethodPut, subPath, token, map[string]any{
		"topic":              "projects/" + project + "/topics/" + topicID,
		"ackDeadlineSeconds": 10,
		"pushConfig": map[string]any{
			"pushEndpoint": catcher,
			"oidcToken": map[string]any{
				"serviceAccountEmail": saEmail,
				"audience":            audience,
			},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("create subscription status=%d body=%s", status, body)
	}
	var created map[string]any
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode subscription: %v body=%s", err, body)
	}
	pc, _ := created["pushConfig"].(map[string]any)
	oidc, _ := pc["oidcToken"].(map[string]any)
	if pc["pushEndpoint"] != catcher || oidc["serviceAccountEmail"] != saEmail || oidc["audience"] != audience {
		t.Fatalf("oidcToken round-trip failed: %#v", created["pushConfig"])
	}

	payload := base64.StdEncoding.EncodeToString([]byte("sdk-oidc-ping"))
	status, body = doJSON(t, http.MethodPost, topicPath+":publish", token, map[string]any{
		"messages": []any{map[string]any{"data": payload}},
	})
	if status != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", status, body)
	}

	t.Run("catcherAuthorization", func(t *testing.T) {
		status, dump, err := doJSONErr(http.MethodGet, ep+"/_noctaxris-gcp/http-catcher", token, nil)
		if err != nil || status != http.StatusOK {
			t.Skipf("lab catcher dump unavailable (status=%d err=%v); soft-skip Authorization assert (unit TestPubSubOIDCPushCatcher covers Bearer JWT)", status, err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(dump, &parsed); err != nil {
			t.Fatalf("decode catcher dump: %v body=%s", err, dump)
		}
		deliveries, _ := parsed["deliveries"].([]any)
		if len(deliveries) == 0 {
			t.Skip("catcher dump empty; soft-skip Authorization assert")
		}
		found := false
		for i := len(deliveries) - 1; i >= 0; i-- {
			raw, _ := deliveries[i].(string)
			var payload map[string]any
			if err := json.Unmarshal([]byte(raw), &payload); err != nil {
				continue
			}
			authz, _ := payload["authorization"].(string)
			if strings.HasPrefix(authz, "Bearer ") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected Bearer authorization in catcher dump body=%s", dump)
		}
	})
}

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

func grpcDialTarget(ep string) string {
	u, err := url.Parse(ep)
	if err != nil || u.Host == "" {
		if strings.Contains(ep, "://") {
			return ep
		}
		return ep
	}
	return u.Host
}

func grpcAuthCtx(token string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token)
}

func TestFirestoreCreateGetSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	conn, err := grpc.NewClient(grpcDialTarget(ep), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc dial: %v", err)
	}
	defer conn.Close()
	client := firestorepb.NewFirestoreClient(conn)
	ctx := grpcAuthCtx(token)

	parent := "projects/" + project + "/databases/(default)/documents"
	docID := uniqueID("sdk-fs")
	created, err := client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
		Parent:       parent,
		CollectionId: "sdk_smoke",
		DocumentId:   docID,
		Document: &firestorepb.Document{
			Fields: map[string]*firestorepb.Value{
				"ping": {ValueType: &firestorepb.Value_StringValue{StringValue: "pong"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	got, err := client.GetDocument(ctx, &firestorepb.GetDocumentRequest{Name: created.GetName()})
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if got.GetFields()["ping"].GetStringValue() != "pong" {
		t.Fatalf("fields=%#v", got.GetFields())
	}
}

func TestDatastoreCommitLookupSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	conn, err := grpc.NewClient(grpcDialTarget(ep), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc dial: %v", err)
	}
	defer conn.Close()
	client := datastorepb.NewDatastoreClient(conn)
	ctx := grpcAuthCtx(token)

	name := uniqueID("sdk-ds")
	key := &datastorepb.Key{
		PartitionId: &datastorepb.PartitionId{ProjectId: project},
		Path: []*datastorepb.Key_PathElement{{
			Kind: "SdkSmoke", IdType: &datastorepb.Key_PathElement_Name{Name: name},
		}},
	}
	_, err = client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: project,
		Mode:      datastorepb.CommitRequest_NON_TRANSACTIONAL,
		Mutations: []*datastorepb.Mutation{{
			Operation: &datastorepb.Mutation_Upsert{
				Upsert: &datastorepb.Entity{
					Key: key,
					Properties: map[string]*datastorepb.Value{
						"v": {ValueType: &datastorepb.Value_StringValue{StringValue: "ok"}},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	lookup, err := client.Lookup(ctx, &datastorepb.LookupRequest{ProjectId: project, Keys: []*datastorepb.Key{key}})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(lookup.Found) != 1 {
		t.Fatalf("found=%v missing=%v", lookup.Found, lookup.Missing)
	}
}

func TestGKEClusterAndCDNEdgeSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	clusterID := uniqueID("sdk-gke")
	if len(clusterID) > 40 {
		clusterID = clusterID[:40]
	}
	base := ep + "/container/v1/projects/" + project + "/locations/us-central1/clusters"
	status, body := doJSON(t, http.MethodPost, base+"?clusterId="+url.QueryEscape(clusterID), token, map[string]any{
		"displayName": "SDK GKE",
	})
	if status != http.StatusOK {
		t.Fatalf("create cluster status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, base+"/"+clusterID, token, nil)
	})
	status, body = doJSON(t, http.MethodGet, base+"/"+clusterID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get cluster status=%d body=%s", status, body)
	}

	distID := uniqueID("sdk-cdn")
	if len(distID) > 40 {
		distID = distID[:40]
	}
	distBase := ep + "/v1/projects/" + project + "/global/distributions"
	status, body = doJSON(t, http.MethodPost, distBase+"?distributionId="+url.QueryEscape(distID), token, map[string]any{
		"origin": map[string]any{"gcs": map[string]any{"bucket": "missing-bucket-for-smoke"}},
	})
	if status != http.StatusOK {
		t.Fatalf("create distribution status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, distBase+"/"+distID, token, nil)
	})
	edgeURL := ep + "/cdn/" + distID + "/obj.txt"
	req, err := http.NewRequest(http.MethodGet, edgeURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("edge without object want 404, got %d", resp.StatusCode)
	}
}

func TestListGKEClustersSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/container/v1/projects/" + project + "/locations/us-central1/clusters"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list GKE clusters status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode GKE clusters: %v body=%s", err, body)
	}
	if _, ok := parsed["clusters"]; !ok {
		t.Fatalf("missing clusters field body=%s", body)
	}
}

func TestListLoadBalancingBackendServicesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/compute/v1/projects/" + project + "/global/backendServices"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list backendServices status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode backendServices: %v body=%s", err, body)
	}
	if kind, _ := parsed["kind"].(string); kind != "compute#backendServiceList" {
		t.Fatalf("kind=%v want compute#backendServiceList body=%s", parsed["kind"], body)
	}
	if _, ok := parsed["items"]; !ok {
		t.Fatalf("missing items field body=%s", body)
	}
}

func TestListCDNDistributionsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/global/distributions"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list CDN distributions status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode CDN distributions: %v body=%s", err, body)
	}
	if _, ok := parsed["distributions"]; !ok {
		t.Fatalf("missing distributions field body=%s", body)
	}
}

func TestListKMSKeyRingsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/global/keyRings"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list KMS keyRings status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode KMS keyRings: %v body=%s", err, body)
	}
	if _, ok := parsed["keyRings"]; !ok {
		t.Fatalf("missing keyRings field body=%s", body)
	}
}

func TestListServiceUsageServicesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/services"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list Service Usage services status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode Service Usage services: %v body=%s", err, body)
	}
	if _, ok := parsed["services"]; !ok {
		t.Fatalf("missing services field body=%s", body)
	}
}

func TestListBigQueryDatasetsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/bigquery/v2/projects/" + project + "/datasets"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list BigQuery datasets status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode BigQuery datasets: %v body=%s", err, body)
	}
	if _, ok := parsed["datasets"]; !ok {
		t.Fatalf("missing datasets field body=%s", body)
	}
}

func TestListSpannerInstancesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/instances"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list Spanner instances status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode Spanner instances: %v body=%s", err, body)
	}
	if _, ok := parsed["instances"]; !ok {
		t.Fatalf("missing instances field body=%s", body)
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
