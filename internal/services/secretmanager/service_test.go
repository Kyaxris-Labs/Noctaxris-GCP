package secretmanager_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/secretmanager"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func setupSecretManager(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	return setupSecretManagerWithPrincipal(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})
}

func setupSecretManagerWithPrincipal(t *testing.T, principal func(*http.Request) (authn.Principal, bool)) (*http.ServeMux, string) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project := "noctaxris-gcp-local"
	rootSA := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootSA); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &secretmanager.Service{
		Store:          st,
		Authz:          &authz.Evaluator{Policies: st},
		DefaultProject: project,
		HTTPPrincipal:  principal,
	}
	svc.RegisterREST(mux)
	return mux, project
}

func TestSecretManagerAuthzDenyNonRoot(t *testing.T) {
	mux, project := setupSecretManagerWithPrincipal(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets?secretId=deny-secret",
		bytes.NewReader([]byte(`{"replication":{"automatic":{}}}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSecretRotateAccessAndVersionStates(t *testing.T) {
	mux, project := setupSecretManager(t)
	next := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	cmek := "projects/" + project + "/locations/global/keyRings/lab/cryptoKeys/cmek"

	createBody := `{"replication":{"automatic":{}},"customerManagedEncryption":{"kmsKeyName":"` + cmek + `"},"rotation":{"rotationPeriod":"86400s","nextRotationTime":"` + next + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets?secretId=wp1-lab", bytes.NewReader([]byte(createBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create secret status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sec map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sec)
	cmekObj, _ := sec["customerManagedEncryption"].(map[string]any)
	if cmekObj["kmsKeyName"] != cmek {
		t.Fatalf("cmek stored = %#v", sec["customerManagedEncryption"])
	}

	v1Data := base64.StdEncoding.EncodeToString([]byte("version-one"))
	addURL := "/v1/projects/" + project + "/secrets/wp1-lab:addVersion"
	req = httptest.NewRequest(http.MethodPost, addURL, bytes.NewReader([]byte(`{"payload":{"data":"`+v1Data+`"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("addVersion status=%d body=%s", rec.Code, rec.Body.String())
	}

	accessURL := "/v1/projects/" + project + "/secrets/wp1-lab/versions/1:access"
	req = httptest.NewRequest(http.MethodGet, accessURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("access v1 status=%d body=%s", rec.Code, rec.Body.String())
	}
	var access map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &access)
	payload, _ := access["payload"].(map[string]any)
	gotB64, _ := payload["data"].(string)
	got, _ := base64.StdEncoding.DecodeString(gotB64)
	if string(got) != "version-one" {
		t.Fatalf("access v1 = %q", got)
	}

	rotURL := "/v1/projects/" + project + "/secrets/wp1-lab:rotateSecret"
	req = httptest.NewRequest(http.MethodPost, rotURL, bytes.NewReader([]byte(`{"payload":{"data":"`+base64.StdEncoding.EncodeToString([]byte("rotated-payload"))+`"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotateSecret status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rotVer map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rotVer)
	if !strings.HasSuffix(rotVer["name"].(string), "/versions/2") {
		t.Fatalf("rotate version = %#v", rotVer)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/secrets/wp1-lab/versions/latest:access", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("access latest status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &access)
	payload, _ = access["payload"].(map[string]any)
	gotB64, _ = payload["data"].(string)
	got, _ = base64.StdEncoding.DecodeString(gotB64)
	if string(got) != "rotated-payload" {
		t.Fatalf("access latest = %q", got)
	}

	disableURL := "/v1/projects/" + project + "/secrets/wp1-lab/versions/1:disable"
	req = httptest.NewRequest(http.MethodPost, disableURL, bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, accessURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("access disabled status=%d body=%s", rec.Code, rec.Body.String())
	}

	destroyURL := "/v1/projects/" + project + "/secrets/wp1-lab/versions/1:destroy"
	req = httptest.NewRequest(http.MethodPost, destroyURL, bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("destroy status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, accessURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("access destroyed status=%d body=%s", rec.Code, rec.Body.String())
	}

	listURL := "/v1/projects/" + project + "/secrets/wp1-lab/versions?filter=" + url.QueryEscape("state:ENABLED")
	req = httptest.NewRequest(http.MethodGet, listURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list enabled status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listBody struct {
		Versions []map[string]any `json:"versions"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listBody)
	if len(listBody.Versions) != 1 || listBody.Versions[0]["state"] != "ENABLED" {
		t.Fatalf("enabled versions = %#v", listBody.Versions)
	}
}
