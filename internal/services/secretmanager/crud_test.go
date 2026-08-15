package secretmanager_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

func TestSecretManagerCRUDAndIAM(t *testing.T) {
	mux, project := setupSecretManager(t)
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
		mux.ServeHTTP(rec, req)
		return rec
	}

	base := "/v1/projects/" + project + "/secrets"
	rec := do(http.MethodPost, base+"?secretId=crud-sec", `{"labels":{"a":"1"},"annotations":{"n":"v"},"replication":{"automatic":{}},"topics":[{"name":"projects/`+project+`/topics/rot"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"?secretId=crud-sec", `{"replication":{"automatic":{}}}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup: %d", rec.Code)
	}
	rec = do(http.MethodPost, base, `{"replication":{"automatic":{}}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing secretId: %d", rec.Code)
	}
	rec = do(http.MethodGet, base, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	sec := base + "/crud-sec"
	rec = do(http.MethodGet, sec, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPatch, sec, `{"labels":{"a":"2"},"rotation":{"rotationPeriod":"3600s","nextRotationTime":"2030-01-01T00:00:00Z"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}

	payload := base64.StdEncoding.EncodeToString([]byte("secret-data"))
	rec = do(http.MethodPost, sec+":addVersion", `{"payload":{"data":"`+payload+`"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("addVersion: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, sec+"/versions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, sec+"/versions/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get version: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, sec+"/versions/1:disable", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, sec+"/versions/1:enable", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, sec+"/versions/1:access", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("access: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, sec+":getIamPolicy", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	policy := `{"policy":{"etag":"ACAB","bindings":[{"role":"roles/secretmanager.secretAccessor","members":["serviceAccount:reader@example.com"]}]}}`
	rec = do(http.MethodPost, sec+":setIamPolicy", policy)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, sec+":getIamPolicy", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy after set: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, sec+":testIamPermissions", `{"permissions":["secretmanager.versions.access","secretmanager.secrets.get"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("testIamPermissions: %d %s", rec.Code, rec.Body.String())
	}
	var perms map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &perms)
	if perms["permissions"] == nil {
		t.Fatalf("permissions=%#v", perms)
	}

	rec = do(http.MethodPost, sec+":unknownMethod", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown method: %d", rec.Code)
	}
	rec = do(http.MethodGet, sec+"/versions/1:weird", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("weird version get: %d", rec.Code)
	}
	rec = do(http.MethodPost, sec+"/versions/1:weird", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("weird version post: %d", rec.Code)
	}

	rec = do(http.MethodDelete, sec, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, sec, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", rec.Code)
	}
	rec = do(http.MethodGet, sec, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing: %d", rec.Code)
	}
}

func TestSecretManagerUnauthenticated(t *testing.T) {
	mux, project := setupSecretManagerWithPrincipal(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/secrets", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
