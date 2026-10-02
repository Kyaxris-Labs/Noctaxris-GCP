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

func TestKMSAsymmetricSignAndEncryptEdges(t *testing.T) {
	mux, project := setupKMS(t)
	loc := kms.DefaultLocation
	ring := "/v1/projects/" + project + "/locations/" + loc + "/keyRings"

	req := httptest.NewRequest(http.MethodPost, ring+"?keyRingId=edge-ring", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ring: %d %s", rec.Code, rec.Body.String())
	}

	symURL := ring + "/edge-ring/cryptoKeys?cryptoKeyId=sym"
	req = httptest.NewRequest(http.MethodPost, symURL, bytes.NewReader([]byte(`{"purpose":"ENCRYPT_DECRYPT"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create sym: %d %s", rec.Code, rec.Body.String())
	}

	asymURL := ring + "/edge-ring/cryptoKeys?cryptoKeyId=asym"
	req = httptest.NewRequest(http.MethodPost, asymURL, bytes.NewReader([]byte(
		`{"purpose":"ASYMMETRIC_SIGN","versionTemplate":{"algorithm":"RSA_SIGN_PSS_2048_SHA256"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create asym: %d %s", rec.Code, rec.Body.String())
	}

	symPath := ring + "/edge-ring/cryptoKeys/sym"
	plain := base64.StdEncoding.EncodeToString([]byte("hello"))
	aad := base64.StdEncoding.EncodeToString([]byte("aad"))
	req = httptest.NewRequest(http.MethodPost, symPath+":encrypt",
		bytes.NewReader([]byte(`{"plaintext":"`+plain+`","additionalAuthenticatedData":"`+aad+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("encrypt aad: %d %s", rec.Code, rec.Body.String())
	}
	var enc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &enc)
	ct, _ := enc["ciphertext"].(string)
	if ct == "" {
		t.Fatalf("enc=%#v", enc)
	}
	req = httptest.NewRequest(http.MethodPost, symPath+":decrypt",
		bytes.NewReader([]byte(`{"ciphertext":"`+ct+`","additionalAuthenticatedData":"`+aad+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("decrypt aad: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, symPath+":encrypt", bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("encrypt bad json")
	}
	req = httptest.NewRequest(http.MethodPost, symPath+":encrypt",
		bytes.NewReader([]byte(`{"plaintext":"ok","additionalAuthenticatedData":"!!!"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("encrypt bad aad")
	}

	asymPath := ring + "/edge-ring/cryptoKeys/asym"
	ver := asymPath + "/cryptoKeyVersions/1"
	req = httptest.NewRequest(http.MethodPost, asymPath+":encrypt",
		bytes.NewReader([]byte(`{"plaintext":"`+plain+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("encrypt on asym key should fail")
	}

	data := base64.StdEncoding.EncodeToString([]byte("sign-me"))
	req = httptest.NewRequest(http.MethodPost, ver+":asymmetricSign",
		bytes.NewReader([]byte(`{"data":"`+data+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign data: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, ver+":asymmetricSign",
		bytes.NewReader([]byte(`{"digest":{"sha256":"`+base64.StdEncoding.EncodeToString([]byte("short"))+`"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("short digest should fail")
	}
	req = httptest.NewRequest(http.MethodPost, ver+":asymmetricSign",
		bytes.NewReader([]byte(`{"digest":{"sha256":"!!!"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad digest b64")
	}
	req = httptest.NewRequest(http.MethodPost, ver+":asymmetricSign",
		bytes.NewReader([]byte(`{"data":"!!!"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad data b64")
	}
	req = httptest.NewRequest(http.MethodPost, ver+":asymmetricSign", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("empty sign body")
	}
	req = httptest.NewRequest(http.MethodPost, ver+":asymmetricSign", bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("sign bad json")
	}

	req = httptest.NewRequest(http.MethodPost, symPath+"/cryptoKeyVersions/1:asymmetricSign",
		bytes.NewReader([]byte(`{"data":"`+data+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("sign on sym key")
	}

	req = httptest.NewRequest(http.MethodGet, ring+"/edge-ring/cryptoKeys", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list keys: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, ring, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list rings: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, ver+"/publicKey", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("publicKey: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, asymPath+"/cryptoKeyVersions/99/publicKey", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing version publicKey: %d", rec.Code)
	}
}
