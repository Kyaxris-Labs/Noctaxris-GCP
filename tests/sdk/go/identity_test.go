package sdk_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

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
