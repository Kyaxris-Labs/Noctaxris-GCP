package server_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/kms"
)

func TestKMSEncryptDecryptViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	loc := kms.DefaultLocation
	ringPath := "/v1/projects/" + cfg.ProjectID + "/locations/" + loc + "/keyRings?keyRingId=srv-ring"
	req := httptest.NewRequest(http.MethodPost, ringPath, bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create ring status=%d body=%s", rec.Code, rec.Body.String())
	}

	keyPath := "/v1/projects/" + cfg.ProjectID + "/locations/" + loc + "/keyRings/srv-ring/cryptoKeys?cryptoKeyId=srv-key"
	req = httptest.NewRequest(http.MethodPost, keyPath, bytes.NewReader([]byte(`{"purpose":"ENCRYPT_DECRYPT"}`)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create key status=%d body=%s", rec.Code, rec.Body.String())
	}

	plain := base64.StdEncoding.EncodeToString([]byte("wired"))
	encPath := "/v1/projects/" + cfg.ProjectID + "/locations/" + loc + "/keyRings/srv-ring/cryptoKeys/srv-key:encrypt"
	req = httptest.NewRequest(http.MethodPost, encPath, bytes.NewReader([]byte(`{"plaintext":"`+plain+`"}`)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("encrypt status=%d body=%s", rec.Code, rec.Body.String())
	}
	var enc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &enc)
	ct, _ := enc["ciphertext"].(string)
	if ct == "" {
		t.Fatal("missing ciphertext")
	}

	decPath := "/v1/projects/" + cfg.ProjectID + "/locations/" + loc + "/keyRings/srv-ring/cryptoKeys/srv-key:decrypt"
	req = httptest.NewRequest(http.MethodPost, decPath, bytes.NewReader([]byte(`{"ciphertext":"`+ct+`"}`)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("decrypt status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLoggingWriteListViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	body := `{"logName":"projects/` + cfg.ProjectID + `/logs/wired","entries":[{"textPayload":"hello-wired"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v2/entries:write", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("write status=%d body=%s", rec.Code, rec.Body.String())
	}

	list := `{"resourceNames":["projects/` + cfg.ProjectID + `"],"filter":"logName=\"projects/` + cfg.ProjectID + `/logs/wired\""}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(list)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("entries=%#v", resp.Entries)
	}
}
