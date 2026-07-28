package server_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestIAMWIFAndGenerateAccessToken(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	createPool := httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/locations/global/workloadIdentityPools?workloadIdentityPoolId=lab-pool",
		strings.NewReader(`{"displayName":"Lab Pool","description":"theatre"}`))
	createPool.Header.Set("Authorization", auth)
	createPool.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, createPool)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pool status=%d body=%s", rec.Code, rec.Body.String())
	}

	createProv := httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/locations/global/workloadIdentityPools/lab-pool/providers?workloadIdentityPoolProviderId=oidc",
		strings.NewReader(`{"displayName":"OIDC","oidc":{"issuerUri":"https://example.com"},"attributeMapping":{"google.subject":"assertion.sub"}}`))
	createProv.Header.Set("Authorization", auth)
	createProv.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, createProv)
	if rec.Code != http.StatusOK {
		t.Fatalf("create provider status=%d body=%s", rec.Code, rec.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet,
		"/v1/projects/"+project+"/locations/global/workloadIdentityPools/lab-pool/providers", nil)
	list.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, list)
	if rec.Code != http.StatusOK {
		t.Fatalf("list providers status=%d body=%s", rec.Code, rec.Body.String())
	}

	email := createLabServiceAccount(t, srv, cfg, "impersonatee", "Impersonatee")
	body := []byte(`{"scope":["https://www.googleapis.com/auth/cloud-platform"],"lifetime":"600s"}`)
	tokReq := httptest.NewRequest(http.MethodPost,
		"/v1/projects/-/serviceAccounts/"+email+":generateAccessToken", bytes.NewReader(body))
	tokReq.Header.Set("Authorization", auth)
	tokReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, tokReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("generateAccessToken status=%d body=%s", rec.Code, rec.Body.String())
	}
	var tokResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tokResp); err != nil {
		t.Fatal(err)
	}
	accessToken, _ := tokResp["accessToken"].(string)
	if accessToken == "" || tokResp["expireTime"] == nil {
		t.Fatalf("token resp = %#v", tokResp)
	}

	// Minted token authenticates as the target SA (not root).
	getSA := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/serviceAccounts/"+email, nil)
	getSA.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, getSA)
	// SA without project IAM may be denied; root owner SA should work if policy grants.
	// Lab root is owner; minted SA token is the impersonatee itself — may get 403.
	// Assert at least token is accepted (not 401).
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("minted token unauthenticated: %s", rec.Body.String())
	}
}

func TestTokenCreatorGenerateAccessToken(t *testing.T) {
	srv, cfg := testServer(t)
	rootAuth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	caller := createLabServiceAccount(t, srv, cfg, "tok-caller", "Token Caller")
	target := createLabServiceAccount(t, srv, cfg, "tok-target", "Token Target")
	callerBearer := mintLabSABearer(t, srv, cfg, caller)

	scopeBody := []byte(`{"scope":["https://www.googleapis.com/auth/cloud-platform"],"lifetime":"600s"}`)
	denyReq := httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/serviceAccounts/"+target+":generateAccessToken", bytes.NewReader(scopeBody))
	denyReq.Header.Set("Authorization", "Bearer "+callerBearer)
	denyReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, denyReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("without TokenCreator expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	var denyBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &denyBody); err != nil {
		t.Fatal(err)
	}
	errObj, _ := denyBody["error"].(map[string]any)
	if errObj["status"] != "PERMISSION_DENIED" {
		t.Fatalf("deny error = %#v", errObj)
	}

	pol := `{"policy":{"bindings":[{"role":"roles/iam.serviceAccountTokenCreator","members":["serviceAccount:` + caller + `"]}],"etag":"tc1"}}`
	setReq := httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/serviceAccounts/"+target+":setIamPolicy", strings.NewReader(pol))
	setReq.Header.Set("Authorization", rootAuth)
	setReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, setReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}

	allowReq := httptest.NewRequest(http.MethodPost,
		"/v1/projects/-/serviceAccounts/"+target+":generateAccessToken", bytes.NewReader(scopeBody))
	allowReq.Header.Set("Authorization", "Bearer "+callerBearer)
	allowReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, allowReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("with TokenCreator generateAccessToken status=%d body=%s", rec.Code, rec.Body.String())
	}
	var tokResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tokResp); err != nil {
		t.Fatal(err)
	}
	if tok, _ := tokResp["accessToken"].(string); tok == "" {
		t.Fatalf("token resp = %#v", tokResp)
	}
}

func TestSTSTokenExchangeWIF(t *testing.T) {
	srv, cfg := testServer(t)
	rootAuth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID
	providerName := "projects/" + project + "/locations/global/workloadIdentityPools/sts-pool/providers/oidc"

	createPool := httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/locations/global/workloadIdentityPools?workloadIdentityPoolId=sts-pool",
		strings.NewReader(`{"displayName":"STS Pool"}`))
	createPool.Header.Set("Authorization", rootAuth)
	createPool.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, createPool)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pool status=%d body=%s", rec.Code, rec.Body.String())
	}

	createProv := httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/locations/global/workloadIdentityPools/sts-pool/providers?workloadIdentityPoolProviderId=oidc",
		strings.NewReader(`{"displayName":"OIDC","oidc":{"issuerUri":"https://example.com"}}`))
	createProv.Header.Set("Authorization", rootAuth)
	createProv.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, createProv)
	if rec.Code != http.StatusOK {
		t.Fatalf("create provider status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Missing audience / subject_token fail closed.
	for _, body := range []string{
		`grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Atoken-exchange&subject_token=lab-sub`,
		`grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Atoken-exchange&audience=` + url.QueryEscape(providerName),
	} {
		bad := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(body))
		bad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec = httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bad)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("missing field expected 400, got %d body=%s for %q", rec.Code, rec.Body.String(), body)
		}
	}

	// Unknown provider.
	unknown := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(
		"grant_type="+url.QueryEscape("urn:ietf:params:oauth:grant-type:token-exchange")+
			"&audience="+url.QueryEscape("projects/"+project+"/locations/global/workloadIdentityPools/sts-pool/providers/missing")+
			"&subject_token=lab-sub"))
	unknown.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, unknown)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown provider expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Disabled provider.
	createDisabled := httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/locations/global/workloadIdentityPools/sts-pool/providers?workloadIdentityPoolProviderId=disabled",
		strings.NewReader(`{"displayName":"Disabled","disabled":true,"oidc":{"issuerUri":"https://example.com"}}`))
	createDisabled.Header.Set("Authorization", rootAuth)
	createDisabled.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, createDisabled)
	if rec.Code != http.StatusOK {
		t.Fatalf("create disabled provider status=%d body=%s", rec.Code, rec.Body.String())
	}
	disabledAud := "projects/" + project + "/locations/global/workloadIdentityPools/sts-pool/providers/disabled"
	disReq := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(
		"grant_type="+url.QueryEscape("urn:ietf:params:oauth:grant-type:token-exchange")+
			"&audience="+url.QueryEscape("//iam.googleapis.com/"+disabledAud)+
			"&subject_token=lab-sub"))
	disReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, disReq)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("disabled provider expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Happy path: form exchange with //iam.googleapis.com/ audience prefix.
	exch := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(
		"grant_type="+url.QueryEscape("urn:ietf:params:oauth:grant-type:token-exchange")+
			"&audience="+url.QueryEscape("//iam.googleapis.com/"+providerName)+
			"&subject_token=lab-sub"+
			"&subject_token_type="+url.QueryEscape("urn:ietf:params:oauth:token-type:jwt")))
	exch.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, exch)
	if rec.Code != http.StatusOK {
		t.Fatalf("STS exchange status=%d body=%s", rec.Code, rec.Body.String())
	}
	var stsResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &stsResp); err != nil {
		t.Fatal(err)
	}
	accessToken, _ := stsResp["access_token"].(string)
	if accessToken == "" || stsResp["token_type"] != "Bearer" {
		t.Fatalf("sts resp = %#v", stsResp)
	}

	// Without project binding: authenticated but denied.
	getDenied := httptest.NewRequest(http.MethodGet, "/v3/projects/"+project, nil)
	getDenied.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, getDenied)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("WIF bearer unauthenticated: %s", rec.Body.String())
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("WIF without binding expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Bind viewer for wif:{providerId}:{subject} and retry.
	wifMember := "wif:oidc:lab-sub"
	pol := `{"policy":{"bindings":[{"role":"roles/viewer","members":["` + wifMember + `"]}],"etag":"wif1"}}`
	setPol := httptest.NewRequest(http.MethodPost, "/v3/projects/"+project+":setIamPolicy", strings.NewReader(pol))
	setPol.Header.Set("Authorization", rootAuth)
	setPol.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, setPol)
	if rec.Code != http.StatusOK {
		t.Fatalf("project setIamPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}

	getOK := httptest.NewRequest(http.MethodGet, "/v3/projects/"+project, nil)
	getOK.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, getOK)
	if rec.Code != http.StatusOK {
		t.Fatalf("WIF viewer GET project status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCRMTagKeysAndBindingsViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken

	createKey := httptest.NewRequest(http.MethodPost, "/v3/tagKeys",
		strings.NewReader(`{"parent":"organizations/noctaxris-gcp-org","shortName":"env","description":"lab"}`))
	createKey.Header.Set("Authorization", auth)
	createKey.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, createKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("create tagKey status=%d body=%s", rec.Code, rec.Body.String())
	}
	var key map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &key)
	keyName, _ := key["name"].(string)
	if !strings.HasPrefix(keyName, "tagKeys/") {
		t.Fatalf("key name = %#v", key)
	}

	listKeys := httptest.NewRequest(http.MethodGet, "/v3/tagKeys?parent=organizations/noctaxris-gcp-org", nil)
	listKeys.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, listKeys)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tagKeys status=%d body=%s", rec.Code, rec.Body.String())
	}

	bindBody := `{"parent":"projects/` + cfg.ProjectID + `","tagValueNamespacedName":"noctaxris-gcp-org/env/prod"}`
	createBind := httptest.NewRequest(http.MethodPost, "/v3/tagBindings", strings.NewReader(bindBody))
	createBind.Header.Set("Authorization", auth)
	createBind.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, createBind)
	if rec.Code != http.StatusOK {
		t.Fatalf("create tagBinding status=%d body=%s", rec.Code, rec.Body.String())
	}

	listBind := httptest.NewRequest(http.MethodGet, "/v3/tagBindings?parent="+url.QueryEscape("projects/"+cfg.ProjectID), nil)
	listBind.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, listBind)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tagBindings status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSecretManagerRotationAndRotateSecret(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID
	next := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)

	createBody := `{"replication":{"automatic":{}},"rotation":{"rotationPeriod":"86400s","nextRotationTime":"` + next + `"},"topics":[{"name":"projects/` + project + `/topics/rot"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets?secretId=rot-lab", strings.NewReader(createBody))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create secret status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sec map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sec)
	rot, _ := sec["rotation"].(map[string]any)
	if rot["rotationPeriod"] != "86400s" {
		t.Fatalf("rotation = %#v", sec)
	}

	add := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets/rot-lab:addVersion",
		strings.NewReader(`{"payload":{"data":"`+base64.StdEncoding.EncodeToString([]byte("one"))+`"}}`))
	add.Header.Set("Authorization", auth)
	add.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, add)
	if rec.Code != http.StatusOK {
		t.Fatalf("addVersion status=%d body=%s", rec.Code, rec.Body.String())
	}

	rotReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets/rot-lab:rotateSecret", strings.NewReader(`{}`))
	rotReq.Header.Set("Authorization", auth)
	rotReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, rotReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotateSecret status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ver map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &ver)
	if !strings.HasSuffix(ver["name"].(string), "/versions/2") {
		t.Fatalf("rotated version = %#v", ver)
	}
}

func TestGCSV4SignedURLGenerateAndGet(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID
	host := "127.0.0.1:4588"

	req := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project, strings.NewReader(`{"name":"signed-lab"}`))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Host = host
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bucket: %d %s", rec.Code, rec.Body.String())
	}

	up := httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/signed-lab/o?uploadType=media&name=hello.txt", strings.NewReader("signed-bytes"))
	up.Header.Set("Authorization", auth)
	up.Header.Set("Content-Type", "text/plain")
	up.Host = host
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, up)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	gen := httptest.NewRequest(http.MethodPost, "/storage/v1/b/signed-lab/o/hello.txt:generateSignedUrl",
		strings.NewReader(`{"method":"GET","expires":600,"alt":"media"}`))
	gen.Header.Set("Authorization", auth)
	gen.Header.Set("Content-Type", "application/json")
	gen.Host = host
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, gen)
	if rec.Code != http.StatusOK {
		t.Fatalf("generateSignedUrl: %d %s", rec.Code, rec.Body.String())
	}
	var genResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &genResp)
	signedURL, _ := genResp["signedUrl"].(string)
	if signedURL == "" {
		t.Fatalf("genResp = %#v", genResp)
	}
	u, err := url.Parse(signedURL)
	if err != nil {
		t.Fatal(err)
	}

	dl := httptest.NewRequest(http.MethodGet, u.RequestURI(), nil)
	dl.Host = host
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, dl)
	if rec.Code != http.StatusOK {
		t.Fatalf("signed GET: %d %s", rec.Code, rec.Body.String())
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "signed-bytes" {
		t.Fatalf("body = %q", body)
	}

	// PUT via signed URL
	putGen := httptest.NewRequest(http.MethodPost, "/storage/v1/b/signed-lab/o/via-put.txt:generateSignedUrl",
		strings.NewReader(`{"method":"PUT","expires":600}`))
	putGen.Header.Set("Authorization", auth)
	putGen.Header.Set("Content-Type", "application/json")
	putGen.Host = host
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, putGen)
	if rec.Code != http.StatusOK {
		t.Fatalf("generate PUT url: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &genResp)
	putURL, _ := url.Parse(genResp["signedUrl"].(string))
	put := httptest.NewRequest(http.MethodPut, putURL.RequestURI(), strings.NewReader("from-signed-put"))
	put.Header.Set("Content-Type", "text/plain")
	put.Host = host
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, put)
	if rec.Code != http.StatusOK {
		t.Fatalf("signed PUT: %d %s", rec.Code, rec.Body.String())
	}
}
