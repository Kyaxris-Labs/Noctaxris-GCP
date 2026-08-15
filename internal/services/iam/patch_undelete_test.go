package iam_test

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIAMPatchUndeleteKeysAndPolicy(t *testing.T) {
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

	rec := do(http.MethodPost, base, `{"accountId":"cov-sa","serviceAccount":{"displayName":"Cov","description":"d"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	email := "cov-sa@" + project + ".iam.gserviceaccount.com"

	rec = do(http.MethodPatch, base+"/"+email+"?updateMask=displayName,description",
		`{"displayName":"Cov2","description":"d2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, base+"/"+email+":getIamPolicy", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":setIamPolicy",
		`{"policy":{"etag":"ACAB","bindings":[{"role":"roles/iam.serviceAccountUser","members":["user:a@b.c"]}]}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":testIamPermissions",
		`{"permissions":["iam.serviceAccounts.get","iam.serviceAccounts.actAs"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("testIamPermissions: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, base+"/"+email+"/keys", `{"privateKeyType":"TYPE_GOOGLE_CREDENTIALS_FILE"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, base+"/"+email+"/keys", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list keys: %d %s", rec.Code, rec.Body.String())
	}

	payload := base64.StdEncoding.EncodeToString([]byte("hello-blob"))
	rec = do(http.MethodPost, base+"/"+email+":signBlob", `{"bytesToSign":"`+payload+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("signBlob: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodDelete, base+"/"+email, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":undelete", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("undelete: %d %s", rec.Code, rec.Body.String())
	}

	h.setWho("nobody@example.com", false)
	rec = do(http.MethodGet, base+"/"+email, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("authz deny get: %d", rec.Code)
	}
}
