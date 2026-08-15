package iam_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIAMGenerateAccessTokenLifetimeAndErrors(t *testing.T) {
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

	rec := do(http.MethodPost, base, `{"accountId":"tok-sa","serviceAccount":{"displayName":"Tok"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	email := "tok-sa@" + project + ".iam.gserviceaccount.com"

	rec = do(http.MethodPost, base+"/"+email+":generateAccessToken", `{"scope":["https://www.googleapis.com/auth/cloud-platform"],"lifetime":"3600s"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("token lifetime: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":generateAccessToken", `{"scope":["https://www.googleapis.com/auth/cloud-platform"],"lifetime":"bad"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad lifetime: %d", rec.Code)
	}
	rec = do(http.MethodPost, base+"/"+email+":generateAccessToken", `{"scope":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty scope: %d", rec.Code)
	}
	rec = do(http.MethodPost, base+"/"+email+":generateAccessToken", `{not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", rec.Code)
	}

	rec = do(http.MethodPost, base+"/"+email+":disable", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d", rec.Code)
	}
	rec = do(http.MethodPost, base+"/"+email+":generateAccessToken", `{"scope":["https://www.googleapis.com/auth/cloud-platform"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disabled token: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":enable", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: %d", rec.Code)
	}

	exp := time.Now().Add(time.Hour).Unix()
	rec = do(http.MethodPost, base+"/"+email+":signJwt", fmt.Sprintf(`{"payload":"{\"iss\":\"lab\",\"exp\":%d}"}`, exp))
	if rec.Code != http.StatusOK {
		t.Fatalf("signJwt: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/"+email+":signBlob", `{"bytesToSign":"!!!"}`)
	if rec.Code == http.StatusOK {
		t.Fatal("bad base64 signBlob should fail")
	}
	rec = do(http.MethodGet, base+"/missing@example.com", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing SA: %d", rec.Code)
	}
}
