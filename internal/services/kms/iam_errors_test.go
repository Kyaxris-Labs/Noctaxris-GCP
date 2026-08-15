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

func TestKMSIAMAndEncryptErrorBranches(t *testing.T) {
	mux, project := setupKMS(t)
	loc := kms.DefaultLocation
	ringURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings?keyRingId=iam-ring"
	req := httptest.NewRequest(http.MethodPost, ringURL, bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ring: %d %s", rec.Code, rec.Body.String())
	}
	keyURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/iam-ring/cryptoKeys?cryptoKeyId=iam-key"
	req = httptest.NewRequest(http.MethodPost, keyURL, bytes.NewReader([]byte(`{"purpose":"ENCRYPT_DECRYPT"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("key: %d %s", rec.Code, rec.Body.String())
	}
	res := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/iam-ring/cryptoKeys/iam-key"

	req = httptest.NewRequest(http.MethodPost, res+":getIamPolicy", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, res+":setIamPolicy", bytes.NewReader([]byte(
		`{"policy":{"etag":"ACAB","bindings":[{"role":"roles/cloudkms.cryptoKeyEncrypterDecrypter","members":["serviceAccount:a@b.c"]}]}}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, res+":getIamPolicy", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy2: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, res+":encrypt", bytes.NewReader([]byte(`{"plaintext":"!!!"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad plaintext should fail")
	}
	req = httptest.NewRequest(http.MethodPost, res+":decrypt", bytes.NewReader([]byte(`{"ciphertext":"!!!"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad ciphertext should fail")
	}
	req = httptest.NewRequest(http.MethodPost, res+":encrypt", bytes.NewReader([]byte(`{"plaintext":"`+base64.StdEncoding.EncodeToString([]byte("ok"))+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("encrypt: %d %s", rec.Code, rec.Body.String())
	}
	var enc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &enc)

	req = httptest.NewRequest(http.MethodGet, res+"/cryptoKeyVersions/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing version: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/locations/"+loc+"/keyRings/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing ring: %d", rec.Code)
	}
}
