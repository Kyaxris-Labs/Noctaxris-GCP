package kms_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/kms"
)

func TestKMSUpdateIAMAsymmetricAndVersions(t *testing.T) {
	mux, project := setupKMS(t)
	loc := kms.DefaultLocation
	ring := "/v1/projects/" + project + "/locations/" + loc + "/keyRings"
	req := httptest.NewRequest(http.MethodPost, ring+"?keyRingId=asym-ring", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ring: %d %s", rec.Code, rec.Body.String())
	}

	keyURL := ring + "/asym-ring/cryptoKeys?cryptoKeyId=asym-key"
	req = httptest.NewRequest(http.MethodPost, keyURL, bytes.NewReader([]byte(`{"purpose":"ASYMMETRIC_SIGN","versionTemplate":{"algorithm":"RSA_SIGN_PSS_2048_SHA256"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create asym: %d %s", rec.Code, rec.Body.String())
	}
	keyPath := ring + "/asym-ring/cryptoKeys/asym-key"

	req = httptest.NewRequest(http.MethodPatch, keyPath+"?updateMask=labels",
		bytes.NewReader([]byte(`{"labels":{"env":"lab"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, keyPath+":getIamPolicy", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIam: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, keyPath+":setIamPolicy",
		bytes.NewReader([]byte(`{"policy":{"etag":"ACAB","bindings":[{"role":"roles/cloudkms.cryptoKeyEncrypterDecrypter","members":["user:a@b.c"]}]}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIam: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, keyPath+"/cryptoKeyVersions", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions: %d %s", rec.Code, rec.Body.String())
	}
	var vers struct {
		CryptoKeyVersions []map[string]any `json:"cryptoKeyVersions"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &vers)
	if len(vers.CryptoKeyVersions) < 1 {
		t.Fatalf("versions=%#v", vers)
	}
	verPath := keyPath + "/cryptoKeyVersions/1"
	req = httptest.NewRequest(http.MethodGet, verPath, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get version: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, verPath+"/publicKey", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("publicKey: %d %s", rec.Code, rec.Body.String())
	}

	digest := base64.StdEncoding.EncodeToString(make([]byte, 32))
	req = httptest.NewRequest(http.MethodPost, verPath+":asymmetricSign",
		bytes.NewReader([]byte(`{"digest":{"sha256":"`+digest+`"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("asymmetricSign: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, verPath+":destroy", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	// may refuse destroy of primary — accept non-500
	if rec.Code >= 500 {
		t.Fatalf("destroy: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, verPath+":restore", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code >= 500 {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, ring+"/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing ring: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, keyPath+":setIamPolicy", bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad setIam json")
	}
}
