package iam_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIAMServiceAccountKeysAndWIFCRUD(t *testing.T) {
	h := openIAM(t)
	h.setWho("root@noctaxris-gcp-local.iam.gserviceaccount.com", true)
	project := "noctaxris-gcp-local"
	base := "/v1/projects/" + project + "/serviceAccounts"

	do := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		h.mux.ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodPost, base, `{"accountId":"lab-sa-one","serviceAccount":{"displayName":"Lab SA"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create SA: %d %s", rec.Code, rec.Body.String())
	}
	var sa map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sa)
	email, _ := sa["email"].(string)
	if email == "" {
		t.Fatalf("sa=%#v", sa)
	}
	rec = do(http.MethodPost, base, `{"accountId":"lab-sa-one","serviceAccount":{"displayName":"Lab SA"}}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup SA: %d", rec.Code)
	}
	rec = do(http.MethodPost, base, `{"accountId":"bad","serviceAccount":{}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short accountId: %d", rec.Code)
	}

	rec = do(http.MethodGet, base, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list SA: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, base+"/"+email, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get SA: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPatch, base+"/"+email, `{"displayName":"Renamed","updateMask":"displayName"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch SA: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, base+"/"+email+":disable", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":enable", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", rec.Code, rec.Body.String())
	}

	blob := base64.StdEncoding.EncodeToString([]byte("hello-blob"))
	rec = do(http.MethodPost, base+"/"+email+":signBlob", `{"bytesToSign":"`+blob+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("signBlob: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":signJwt", `{"payload":"{\"iss\":\"lab\",\"sub\":\"lab\",\"aud\":\"x\",\"exp\":`+fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())+`}"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("signJwt: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":generateAccessToken", `{"scope":["https://www.googleapis.com/auth/cloud-platform"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("generateAccessToken: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, base+"/"+email+":getIamPolicy", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":setIamPolicy", `{"policy":{"etag":"ACAB","bindings":[{"role":"roles/iam.serviceAccountUser","members":["user:a@b.c"]}]}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":testIamPermissions", `{"permissions":["iam.serviceAccounts.get","iam.serviceAccounts.actAs"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("testIamPermissions: %d %s", rec.Code, rec.Body.String())
	}

	keysPath := base + "/" + email + "/keys"
	rec = do(http.MethodPost, keysPath, `{"keyAlgorithm":"KEY_ALG_RSA_2048","privateKeyType":"TYPE_GOOGLE_CREDENTIALS_FILE"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
	}
	var key map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &key)
	keyName, _ := key["name"].(string)
	if keyName == "" {
		t.Fatalf("key=%#v", key)
	}
	keyID := keyName[stringsLastSlash(keyName)+1:]
	rec = do(http.MethodGet, keysPath, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list keys: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, keysPath+"/"+keyID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get key: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, keysPath+"/"+keyID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete key: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodDelete, base+"/"+email, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete SA: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":undelete", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("undelete: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, base+"/"+email, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete again: %d %s", rec.Code, rec.Body.String())
	}

	wifBase := "/v1/projects/" + project + "/locations/global/workloadIdentityPools"
	rec = do(http.MethodPost, wifBase+"?workloadIdentityPoolId=cov-pool", `{"displayName":"Cov Pool"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pool: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, wifBase, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list pools: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, wifBase+"/cov-pool", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get pool: %d %s", rec.Code, rec.Body.String())
	}
	provBase := wifBase + "/cov-pool/providers"
	rec = do(http.MethodPost, provBase+"?workloadIdentityPoolProviderId=oidc1", `{"displayName":"OIDC","oidc":{"issuerUri":"https://example.com"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create provider: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, provBase, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list providers: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, provBase+"/oidc1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get provider: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPatch, provBase+"/oidc1", `{"displayName":"OIDC2","oidc":{"issuerUri":"https://example.com"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch provider: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, provBase+"/oidc1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete provider: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, wifBase+"/cov-pool", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete pool: %d %s", rec.Code, rec.Body.String())
	}
}

func stringsLastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
