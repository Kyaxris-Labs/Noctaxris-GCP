package kms_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/kms"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func setupKMS(t *testing.T) (*http.ServeMux, string) {
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
	svc := &kms.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: rootSA, IsRoot: true}, true
	})
	return mux, project
}

func TestEncryptDecryptAndDestroyRefuses(t *testing.T) {
	mux, project := setupKMS(t)
	loc := kms.DefaultLocation
	ringURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings?keyRingId=lab-ring"
	req := httptest.NewRequest(http.MethodPost, ringURL, bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create ring status=%d body=%s", rec.Code, rec.Body.String())
	}

	keyURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/lab-ring/cryptoKeys?cryptoKeyId=lab-key"
	req = httptest.NewRequest(http.MethodPost, keyURL, bytes.NewReader([]byte(`{"purpose":"ENCRYPT_DECRYPT"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create key status=%d body=%s", rec.Code, rec.Body.String())
	}

	plain := base64.StdEncoding.EncodeToString([]byte("hello-kms"))
	encURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/lab-ring/cryptoKeys/lab-key:encrypt"
	req = httptest.NewRequest(http.MethodPost, encURL, bytes.NewReader([]byte(`{"plaintext":"`+plain+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("encrypt status=%d body=%s", rec.Code, rec.Body.String())
	}
	var encResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &encResp); err != nil {
		t.Fatal(err)
	}
	ct, _ := encResp["ciphertext"].(string)
	if ct == "" {
		t.Fatal("missing ciphertext")
	}

	decURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/lab-ring/cryptoKeys/lab-key:decrypt"
	req = httptest.NewRequest(http.MethodPost, decURL, bytes.NewReader([]byte(`{"ciphertext":"`+ct+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("decrypt status=%d body=%s", rec.Code, rec.Body.String())
	}
	var decResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decResp); err != nil {
		t.Fatal(err)
	}
	gotB64, _ := decResp["plaintext"].(string)
	got, err := base64.StdEncoding.DecodeString(gotB64)
	if err != nil || string(got) != "hello-kms" {
		t.Fatalf("plaintext = %q err=%v", got, err)
	}

	desURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/lab-ring/cryptoKeys/lab-key/cryptoKeyVersions/1:destroy"
	req = httptest.NewRequest(http.MethodPost, desURL, bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("destroy status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, encURL, bytes.NewReader([]byte(`{"plaintext":"`+plain+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("encrypt after destroy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	errObj, _ := errBody["error"].(map[string]any)
	if errObj["status"] != "FAILED_PRECONDITION" {
		t.Fatalf("error = %#v", errObj)
	}
}
