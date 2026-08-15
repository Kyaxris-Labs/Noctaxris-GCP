package kms_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/kms"
)

func TestKMSListGetRingsAndKeys(t *testing.T) {
	mux, project := setupKMS(t)
	loc := kms.DefaultLocation
	ringURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings?keyRingId=list-ring"
	req := httptest.NewRequest(http.MethodPost, ringURL, bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create ring: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/locations/"+loc+"/keyRings", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list rings: %d %s", rec.Code, rec.Body.String())
	}
	var rings struct {
		KeyRings []map[string]any `json:"keyRings"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &rings)
	if len(rings.KeyRings) < 1 {
		t.Fatalf("rings=%#v", rings)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/locations/"+loc+"/keyRings/list-ring", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get ring: %d %s", rec.Code, rec.Body.String())
	}

	keyURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/list-ring/cryptoKeys?cryptoKeyId=list-key"
	req = httptest.NewRequest(http.MethodPost, keyURL, bytes.NewReader([]byte(`{"purpose":"ENCRYPT_DECRYPT"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/locations/"+loc+"/keyRings/list-ring/cryptoKeys", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list keys: %d %s", rec.Code, rec.Body.String())
	}
	var keys struct {
		CryptoKeys []map[string]any `json:"cryptoKeys"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &keys)
	if len(keys.CryptoKeys) < 1 {
		t.Fatalf("keys=%#v", keys)
	}
}
