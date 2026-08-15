package secretmanager_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

func TestSecretManagerAuthzDenyAndErrors(t *testing.T) {
	mux, project := setupSecretManagerWithPrincipal(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	base := "/v1/projects/" + project + "/secrets"
	req := httptest.NewRequest(http.MethodPost, base+"?secretId=x", bytes.NewReader([]byte(`{"replication":{"automatic":{}}}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("create deny: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("list deny: %d", rec.Code)
	}

	root, _ := setupSecretManager(t)
	req = httptest.NewRequest(http.MethodGet, base+"/missing", nil)
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, base+"/missing:addVersion", bytes.NewReader([]byte(`{"payload":{"data":"YQ=="}}`)))
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("addVersion missing")
	}
	req = httptest.NewRequest(http.MethodPost, base+"?secretId=", bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad create")
	}
}
