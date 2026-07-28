package server_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/server"
)

func TestCRMGetProject(t *testing.T) {
	srv, cfg := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v3/projects/"+cfg.ProjectID, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["projectId"] != cfg.ProjectID {
		t.Fatalf("projectId = %#v", body["projectId"])
	}
	if body["name"] != "projects/"+cfg.ProjectID {
		t.Fatalf("name = %#v", body["name"])
	}
	if body["state"] != "ACTIVE" {
		t.Fatalf("state = %#v", body["state"])
	}
}

func TestCRMPatchProjectDisplayName(t *testing.T) {
	srv, cfg := testServer(t)
	payload := []byte(`{"displayName":"Noctaxris Lab"}`)
	req := httptest.NewRequest(http.MethodPatch, "/v3/projects/"+cfg.ProjectID+"?updateMask=displayName", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["displayName"] != "Noctaxris Lab" {
		t.Fatalf("displayName = %#v", body["displayName"])
	}
}

func TestIAMCreateServiceAccount(t *testing.T) {
	srv, cfg := testServer(t)
	email := createLabServiceAccount(t, srv, cfg, "lab-runner", "Lab Runner")
	if email != "lab-runner@"+cfg.ProjectID+".iam.gserviceaccount.com" {
		t.Fatalf("email = %s", email)
	}
}

func TestIAMServiceAccountEnableDisablePatchAndIAM(t *testing.T) {
	srv, cfg := testServer(t)
	email := createLabServiceAccount(t, srv, cfg, "lab-toggle", "Toggle")

	disableReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":disable", nil)
	disableReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	disableRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable status = %d body=%s", disableRec.Code, disableRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email, nil)
	getReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	getRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getRec.Code, getRec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["disabled"] != true {
		t.Fatalf("disabled = %#v", got["disabled"])
	}

	enableReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":enable", nil)
	enableReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	enableRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(enableRec, enableReq)
	if enableRec.Code != http.StatusOK {
		t.Fatalf("enable status = %d body=%s", enableRec.Code, enableRec.Body.String())
	}

	patchPayload := []byte(`{"serviceAccount":{"displayName":"Toggle Renamed"},"updateMask":"displayName"}`)
	patchReq := httptest.NewRequest(http.MethodPatch, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email, bytes.NewReader(patchPayload))
	patchReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	patchReq.Header.Set("Content-Type", "application/json")
	patchRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", patchRec.Code, patchRec.Body.String())
	}
	var patched map[string]any
	if err := json.Unmarshal(patchRec.Body.Bytes(), &patched); err != nil {
		t.Fatal(err)
	}
	if patched["displayName"] != "Toggle Renamed" || patched["disabled"] != false {
		t.Fatalf("patched = %#v", patched)
	}

	setPayload := []byte(`{"policy":{"bindings":[{"role":"roles/viewer","members":["serviceAccount:viewer@` + cfg.ProjectID + `.iam.gserviceaccount.com"]}],"etag":"sa1"}}`)
	setReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":setIamPolicy", bytes.NewReader(setPayload))
	setReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	setReq.Header.Set("Content-Type", "application/json")
	setRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(setRec, setReq)
	if setRec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy status = %d body=%s", setRec.Code, setRec.Body.String())
	}

	getPolReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":getIamPolicy", bytes.NewReader([]byte("{}")))
	getPolReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	getPolReq.Header.Set("Content-Type", "application/json")
	getPolRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getPolRec, getPolReq)
	if getPolRec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy status = %d body=%s", getPolRec.Code, getPolRec.Body.String())
	}

	testPayload := []byte(`{"permissions":["iam.serviceAccounts.get","storage.buckets.create"]}`)
	testReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":testIamPermissions", bytes.NewReader(testPayload))
	testReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	testReq.Header.Set("Content-Type", "application/json")
	testRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("testIamPermissions status = %d body=%s", testRec.Code, testRec.Body.String())
	}
	var testBody map[string]any
	if err := json.Unmarshal(testRec.Body.Bytes(), &testBody); err != nil {
		t.Fatal(err)
	}
	perms, _ := testBody["permissions"].([]any)
	if len(perms) != 2 {
		t.Fatalf("root testIamPermissions = %#v", testBody)
	}
}

func TestServiceUsageList(t *testing.T) {
	srv, cfg := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+cfg.ProjectID+"/services", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	services, _ := body["services"].([]any)
	if len(services) < 3 {
		t.Fatalf("services len = %d body=%s", len(services), rec.Body.String())
	}
}

func TestServiceUsageBatchEnableAndFilter(t *testing.T) {
	srv, cfg := testServer(t)

	disableReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/services/storage.googleapis.com:disable", nil)
	disableReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	disableRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable status = %d body=%s", disableRec.Code, disableRec.Body.String())
	}

	batchPayload := []byte(`{"serviceIds":["storage.googleapis.com","pubsub.googleapis.com"]}`)
	batchReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/services:batchEnable", bytes.NewReader(batchPayload))
	batchReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	batchReq.Header.Set("Content-Type", "application/json")
	batchRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(batchRec, batchReq)
	if batchRec.Code != http.StatusOK {
		t.Fatalf("batchEnable status = %d body=%s", batchRec.Code, batchRec.Body.String())
	}
	var batchBody map[string]any
	if err := json.Unmarshal(batchRec.Body.Bytes(), &batchBody); err != nil {
		t.Fatal(err)
	}
	if batchBody["done"] != true {
		t.Fatalf("batchEnable = %#v", batchBody)
	}

	filterReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+cfg.ProjectID+"/services?filter=state:ENABLED", nil)
	filterReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	filterRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(filterRec, filterReq)
	if filterRec.Code != http.StatusOK {
		t.Fatalf("filter status = %d body=%s", filterRec.Code, filterRec.Body.String())
	}
	var filterBody map[string]any
	if err := json.Unmarshal(filterRec.Body.Bytes(), &filterBody); err != nil {
		t.Fatal(err)
	}
	services, _ := filterBody["services"].([]any)
	if len(services) < 1 {
		t.Fatalf("enabled services empty: %s", filterRec.Body.String())
	}
	for _, raw := range services {
		svc, _ := raw.(map[string]any)
		if svc["state"] != "ENABLED" {
			t.Fatalf("non-enabled in filter: %#v", svc)
		}
	}
}

func TestCRMListSearchAndAncestry(t *testing.T) {
	srv, cfg := testServer(t)

	listReq := httptest.NewRequest(http.MethodGet, "/v3/projects", nil)
	listReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listBody map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	projects, _ := listBody["projects"].([]any)
	if len(projects) < 1 {
		t.Fatalf("expected projects, got %s", listRec.Body.String())
	}

	searchPayload := []byte(`{"query":"noctaxris"}`)
	searchReq := httptest.NewRequest(http.MethodPost, "/v3/projects:search", bytes.NewReader(searchPayload))
	searchReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	searchReq.Header.Set("Content-Type", "application/json")
	searchRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(searchRec, searchReq)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("search status = %d body=%s", searchRec.Code, searchRec.Body.String())
	}

	ancReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+":getAncestry", bytes.NewReader([]byte("{}")))
	ancReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	ancReq.Header.Set("Content-Type", "application/json")
	ancRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(ancRec, ancReq)
	if ancRec.Code != http.StatusOK {
		t.Fatalf("ancestry status = %d body=%s", ancRec.Code, ancRec.Body.String())
	}
	var ancBody map[string]any
	if err := json.Unmarshal(ancRec.Body.Bytes(), &ancBody); err != nil {
		t.Fatal(err)
	}
	ancestors, _ := ancBody["ancestor"].([]any)
	if len(ancestors) != 2 {
		t.Fatalf("ancestor = %#v", ancBody)
	}
	first, _ := ancestors[0].(map[string]any)
	firstID, _ := first["resourceId"].(map[string]any)
	if firstID["type"] != "project" || firstID["id"] != cfg.ProjectID {
		t.Fatalf("first ancestor = %#v", first)
	}
	second, _ := ancestors[1].(map[string]any)
	secondID, _ := second["resourceId"].(map[string]any)
	if secondID["type"] != "organization" || secondID["id"] != "noctaxris-gcp-org" {
		t.Fatalf("second ancestor = %#v", second)
	}
}

func TestIAMUndeleteSignBlobKeysPageAndDisabledGate(t *testing.T) {
	srv, cfg := testServer(t)
	email := createLabServiceAccount(t, srv, cfg, "lab-sign", "Sign")

	// Create two keys then page with pageSize=1.
	for i := 0; i < 2; i++ {
		keyReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+"/keys", bytes.NewReader([]byte("{}")))
		keyReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
		keyReq.Header.Set("Content-Type", "application/json")
		keyRec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(keyRec, keyReq)
		if keyRec.Code != http.StatusOK {
			t.Fatalf("create key status = %d body=%s", keyRec.Code, keyRec.Body.String())
		}
	}
	pageReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+"/keys?pageSize=1", nil)
	pageReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	pageRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("list keys status = %d body=%s", pageRec.Code, pageRec.Body.String())
	}
	var pageBody map[string]any
	if err := json.Unmarshal(pageRec.Body.Bytes(), &pageBody); err != nil {
		t.Fatal(err)
	}
	keys, _ := pageBody["keys"].([]any)
	if len(keys) != 1 || pageBody["nextPageToken"] == nil || pageBody["nextPageToken"] == "" {
		t.Fatalf("paged keys = %#v", pageBody)
	}

	payload := base64.StdEncoding.EncodeToString([]byte("hello-lab"))
	signPayload, _ := json.Marshal(map[string]string{"bytesToSign": payload})
	signReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":signBlob", bytes.NewReader(signPayload))
	signReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	signReq.Header.Set("Content-Type", "application/json")
	signRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(signRec, signReq)
	if signRec.Code != http.StatusOK {
		t.Fatalf("signBlob status = %d body=%s", signRec.Code, signRec.Body.String())
	}
	var signBody map[string]any
	if err := json.Unmarshal(signRec.Body.Bytes(), &signBody); err != nil {
		t.Fatal(err)
	}
	wantSum := sha256.Sum256([]byte("hello-lab"))
	wantSig := base64.StdEncoding.EncodeToString(wantSum[:])
	if signBody["signature"] != wantSig || signBody["keyId"] != "lab-sha256" {
		t.Fatalf("signBlob = %#v want %s", signBody, wantSig)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email, nil)
	delReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	delRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", delRec.Code, delRec.Body.String())
	}
	undReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":undelete", nil)
	undReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	undRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(undRec, undReq)
	if undRec.Code != http.StatusOK {
		t.Fatalf("undelete status = %d body=%s", undRec.Code, undRec.Body.String())
	}

	// Disable iam.googleapis.com then create must fail closed.
	disReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/services/iam.googleapis.com:disable", nil)
	disReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	disRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(disRec, disReq)
	if disRec.Code != http.StatusOK {
		t.Fatalf("disable iam status = %d", disRec.Code)
	}
	createPayload := []byte(`{"accountId":"blockedsa","serviceAccount":{"displayName":"Blocked"}}`)
	createReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts", bytes.NewReader(createPayload))
	createReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusBadRequest {
		t.Fatalf("create with disabled iam status = %d body=%s", createRec.Code, createRec.Body.String())
	}
}

func TestIAMSignJwtTheatre(t *testing.T) {
	srv, cfg := testServer(t)
	email := createLabServiceAccount(t, srv, cfg, "lab-jwt", "JWT")

	nowUnix := time.Now().UTC().Unix()
	payloadBytes, err := json.Marshal(map[string]any{"sub": email, "iat": nowUnix, "exp": nowUnix + 3600})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"payload": string(payloadBytes)})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":signJwt", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signJwt status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["keyId"] != "lab-none" {
		t.Fatalf("keyId = %#v", resp["keyId"])
	}
	signed, _ := resp["signedJwt"].(string)
	parts := strings.Split(signed, ".")
	if len(parts) != 3 || parts[2] != "" {
		t.Fatalf("expected unsigned lab JWT (empty sig), got %q", signed)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["sub"] != email {
		t.Fatalf("claims = %#v", claims)
	}

	badExp, _ := json.Marshal(map[string]string{"payload": `{"sub":"x","exp":1}`})
	badReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":signJwt", bytes.NewReader(badExp))
	badReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	badReq.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("past exp status = %d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestServiceUsageBatchGetAndConfigTitle(t *testing.T) {
	srv, cfg := testServer(t)

	getReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+cfg.ProjectID+"/services/storage.googleapis.com", nil)
	getReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	getRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getRec.Code, getRec.Body.String())
	}
	var getBody map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &getBody); err != nil {
		t.Fatal(err)
	}
	cfgMap, _ := getBody["config"].(map[string]any)
	if cfgMap["name"] != "storage.googleapis.com" || cfgMap["title"] != "Cloud Storage API" {
		t.Fatalf("config = %#v", cfgMap)
	}
	apis, _ := cfgMap["apis"].([]any)
	if len(apis) != 1 {
		t.Fatalf("apis = %#v", cfgMap["apis"])
	}
	doc, _ := cfgMap["documentation"].(map[string]any)
	if doc["summary"] != "Cloud Storage API" {
		t.Fatalf("documentation = %#v", doc)
	}

	name := "projects/" + cfg.ProjectID + "/services/storage.googleapis.com"
	batchReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+cfg.ProjectID+"/services:batchGet?names="+name, nil)
	batchReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	batchRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(batchRec, batchReq)
	if batchRec.Code != http.StatusOK {
		t.Fatalf("batchGet status = %d body=%s", batchRec.Code, batchRec.Body.String())
	}
	var batchBody map[string]any
	if err := json.Unmarshal(batchRec.Body.Bytes(), &batchBody); err != nil {
		t.Fatal(err)
	}
	services, _ := batchBody["services"].([]any)
	if len(services) != 1 {
		t.Fatalf("batchGet services = %#v", batchBody)
	}

	disPayload := []byte(`{"serviceIds":["storage.googleapis.com"]}`)
	disReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/services:batchDisable", bytes.NewReader(disPayload))
	disReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	disReq.Header.Set("Content-Type", "application/json")
	disRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(disRec, disReq)
	if disRec.Code != http.StatusOK {
		t.Fatalf("batchDisable status = %d body=%s", disRec.Code, disRec.Body.String())
	}
	var disBody map[string]any
	if err := json.Unmarshal(disRec.Body.Bytes(), &disBody); err != nil {
		t.Fatal(err)
	}
	if disBody["done"] != true {
		t.Fatalf("batchDisable = %#v", disBody)
	}
	getAfter := httptest.NewRequest(http.MethodGet, "/v1/projects/"+cfg.ProjectID+"/services/storage.googleapis.com", nil)
	getAfter.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	getAfterRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getAfterRec, getAfter)
	var after map[string]any
	if err := json.Unmarshal(getAfterRec.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after["state"] != "DISABLED" {
		t.Fatalf("after batchDisable = %#v", after)
	}
}

func createLabServiceAccount(t *testing.T, srv *server.Server, cfg config.Config, accountID, displayName string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"accountId": accountID,
		"serviceAccount": map[string]any{
			"displayName": displayName,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create SA status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	email, _ := body["email"].(string)
	if email == "" {
		t.Fatalf("missing email in %#v", body)
	}
	if body["displayName"] != displayName {
		t.Fatalf("displayName = %#v", body["displayName"])
	}
	return email
}

// mintLabSABearer creates a user-managed key and returns the lab Bearer token from private_key.
func mintLabSABearer(t *testing.T, srv *server.Server, cfg config.Config, email string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+"/keys", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create key status = %d body=%s", rec.Code, rec.Body.String())
	}
	var keyBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &keyBody); err != nil {
		t.Fatal(err)
	}
	b64, _ := keyBody["privateKeyData"].(string)
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	var cred map[string]any
	if err := json.Unmarshal(raw, &cred); err != nil {
		t.Fatal(err)
	}
	token, _ := cred["private_key"].(string)
	if token == "" {
		t.Fatalf("missing private_key in %#v", cred)
	}
	return token
}
