package kms_test

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/kms"
)

func TestKMSNotFoundAndBadEncryptPaths(t *testing.T) {
	mux, project := setupKMS(t)
	loc := kms.DefaultLocation
	ring := "/v1/projects/" + project + "/locations/" + loc + "/keyRings"

	req := httptest.NewRequest(http.MethodGet, ring+"/missing-ring/cryptoKeys/missing-key", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing key status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, ring+"/missing-ring/cryptoKeys/missing-key/cryptoKeyVersions", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("list versions missing status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, ring+"/missing-ring/cryptoKeys/missing-key:encrypt",
		bytes.NewReader([]byte(`{"plaintext":"`+base64.StdEncoding.EncodeToString([]byte("x"))+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("encrypt missing should fail, got %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, ring+"?keyRingId=err-ring", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create ring status=%d body=%s", rec.Code, rec.Body.String())
	}
	keyURL := ring + "/err-ring/cryptoKeys?cryptoKeyId=err-key"
	req = httptest.NewRequest(http.MethodPost, keyURL, bytes.NewReader([]byte(`{"purpose":"ENCRYPT_DECRYPT"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create key status=%d body=%s", rec.Code, rec.Body.String())
	}

	encURL := ring + "/err-ring/cryptoKeys/err-key:encrypt"
	req = httptest.NewRequest(http.MethodPost, encURL, bytes.NewReader([]byte(`{"plaintext":"!!!not-base64!!!"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("bad plaintext should fail, got %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, ring+"/err-ring/cryptoKeys/err-key/cryptoKeyVersions/99:destroy",
		bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("destroy missing status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, ring+"/err-ring/cryptoKeys/err-key/cryptoKeyVersions/99:restore",
		bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("restore missing status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, ring+"/err-ring/cryptoKeys/err-key/cryptoKeyVersions/1/publicKey", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("symmetric key publicKey should fail, got %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, ring+"/err-ring/cryptoKeys/err-key", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get key status=%d body=%s", rec.Code, rec.Body.String())
	}
}
