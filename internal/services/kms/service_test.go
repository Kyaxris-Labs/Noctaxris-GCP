package kms_test

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

	getURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/lab-ring/cryptoKeys/lab-key"
	req = httptest.NewRequest(http.MethodGet, getURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get key status=%d body=%s", rec.Code, rec.Body.String())
	}
	var keyResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &keyResp); err != nil {
		t.Fatal(err)
	}
	primary, _ := keyResp["primary"].(map[string]any)
	if primary["state"] != "DESTROYED" {
		t.Fatalf("primary after destroy = %#v", primary)
	}

	listVerURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/lab-ring/cryptoKeys/lab-key/cryptoKeyVersions"
	req = httptest.NewRequest(http.MethodGet, listVerURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		CryptoKeyVersions []map[string]any `json:"cryptoKeyVersions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.CryptoKeyVersions) != 1 || listResp.CryptoKeyVersions[0]["state"] != "DESTROYED" {
		t.Fatalf("versions = %#v", listResp.CryptoKeyVersions)
	}

	restoreURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/lab-ring/cryptoKeys/lab-key/cryptoKeyVersions/1:restore"
	req = httptest.NewRequest(http.MethodPost, restoreURL, bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", rec.Code, rec.Body.String())
	}
	var restoreResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &restoreResp)
	if restoreResp["state"] != "ENABLED" {
		t.Fatalf("restore = %#v", restoreResp)
	}

	req = httptest.NewRequest(http.MethodGet, getURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &keyResp)
	primary, _ = keyResp["primary"].(map[string]any)
	if primary["state"] != "ENABLED" {
		t.Fatalf("primary after restore = %#v", primary)
	}

	req = httptest.NewRequest(http.MethodPost, encURL, bytes.NewReader([]byte(`{"plaintext":"`+plain+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("encrypt after restore status=%d body=%s", rec.Code, rec.Body.String())
	}

	getVerURL := listVerURL + "/1"
	req = httptest.NewRequest(http.MethodGet, getVerURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get version status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAsymmetricSignGetPublicKeyUpdateAndIAM(t *testing.T) {
	mux, project := setupKMS(t)
	loc := kms.DefaultLocation
	ringURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings?keyRingId=sign-ring"
	req := httptest.NewRequest(http.MethodPost, ringURL, bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create ring status=%d body=%s", rec.Code, rec.Body.String())
	}

	keyURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/sign-ring/cryptoKeys?cryptoKeyId=sign-key"
	body := `{"purpose":"ASYMMETRIC_SIGN","versionTemplate":{"algorithm":"RSA_SIGN_PSS_2048_SHA256","protectionLevel":"SOFTWARE"},"labels":{"env":"lab"}}`
	req = httptest.NewRequest(http.MethodPost, keyURL, bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create asym key status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Symmetric GetPublicKey must fail closed.
	symKeyURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/sign-ring/cryptoKeys?cryptoKeyId=sym-key"
	req = httptest.NewRequest(http.MethodPost, symKeyURL, bytes.NewReader([]byte(`{"purpose":"ENCRYPT_DECRYPT"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create sym key status=%d", rec.Code)
	}
	symPub := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/sign-ring/cryptoKeys/sym-key/cryptoKeyVersions/1/publicKey"
	req = httptest.NewRequest(http.MethodGet, symPub, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GetPublicKey on ENCRYPT_DECRYPT status=%d body=%s", rec.Code, rec.Body.String())
	}

	pubURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/sign-ring/cryptoKeys/sign-key/cryptoKeyVersions/1/publicKey"
	req = httptest.NewRequest(http.MethodGet, pubURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GetPublicKey status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pubResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &pubResp); err != nil {
		t.Fatal(err)
	}
	pemStr, _ := pubResp["pem"].(string)
	if !strings.Contains(pemStr, "BEGIN PUBLIC KEY") {
		t.Fatalf("pem=%q", pemStr)
	}

	sum := sha256.Sum256([]byte("lab-sign-payload"))
	digestB64 := base64.StdEncoding.EncodeToString(sum[:])
	signURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/sign-ring/cryptoKeys/sign-key/cryptoKeyVersions/1:asymmetricSign"
	req = httptest.NewRequest(http.MethodPost, signURL, bytes.NewReader([]byte(`{"digest":{"sha256":"`+digestB64+`"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("asymmetricSign status=%d body=%s", rec.Code, rec.Body.String())
	}
	var signResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &signResp)
	sigB64, _ := signResp["signature"].(string)
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil || len(sig) == 0 {
		t.Fatalf("signature decode: %v %q", err, sigB64)
	}
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		t.Fatal("pem decode failed")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pub := pubAny.(*rsa.PublicKey)
	if err := rsa.VerifyPSS(pub, crypto.SHA256, sum[:], sig, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}); err != nil {
		t.Fatalf("verify: %v", err)
	}

	patchURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/sign-ring/cryptoKeys/sign-key?updateMask=labels"
	req = httptest.NewRequest(http.MethodPatch, patchURL, bytes.NewReader([]byte(`{"labels":{"env":"updated"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("UpdateCryptoKey status=%d body=%s", rec.Code, rec.Body.String())
	}
	var patched map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &patched)
	labels, _ := patched["labels"].(map[string]any)
	if labels["env"] != "updated" {
		t.Fatalf("labels=%#v", labels)
	}

	setIAM := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/sign-ring/cryptoKeys/sign-key:setIamPolicy"
	req = httptest.NewRequest(http.MethodPost, setIAM, bytes.NewReader([]byte(`{"policy":{"etag":"ACAB","bindings":[{"role":"roles/cloudkms.admin","members":["allAuthenticatedUsers"]}]}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}
	getIAM := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/sign-ring/cryptoKeys/sign-key:getIamPolicy"
	req = httptest.NewRequest(http.MethodPost, getIAM, bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pol map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &pol)
	bindings, _ := pol["bindings"].([]any)
	if len(bindings) != 1 {
		t.Fatalf("policy=%#v", pol)
	}
}
