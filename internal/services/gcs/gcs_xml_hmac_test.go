package gcs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestGCSXMLHMACListGetPutAndGenerations(t *testing.T) {
	mux, _, project := openGCS(t)
	host := "127.0.0.1:4588"

	create := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project, strings.NewReader(`{"name":"hmac-xml","location":"US"}`))
	create.Header.Set("Content-Type", "application/json")
	create.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, create)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bucket: %d %s", rec.Code, rec.Body.String())
	}

	hmacReq := httptest.NewRequest(http.MethodPost, "/storage/v1/projects/"+project+"/hmacKeys?serviceAccountEmail=app@"+project+".iam.gserviceaccount.com", nil)
	hmacReq.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, hmacReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("create hmac: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	secret, _ := created["secret"].(string)
	meta, _ := created["metadata"].(map[string]any)
	accessID, _ := meta["accessId"].(string)
	if secret == "" || accessID == "" {
		t.Fatalf("hmac create %#v", created)
	}

	putPath := "/storage/xml/hmac-xml/finance/q1.csv"
	auth, date := store.SignGOOG4HMACHeader(http.MethodPut, host, putPath, accessID, secret, time.Now().UTC())
	put := httptest.NewRequest(http.MethodPut, putPath, strings.NewReader("q1-bytes"))
	put.Host = host
	put.Header.Set("Authorization", auth)
	put.Header.Set("x-goog-date", date)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, put)
	if rec.Code != http.StatusOK {
		t.Fatalf("xml put: %d %s", rec.Code, rec.Body.String())
	}

	getPath := putPath
	auth, date = store.SignGOOG4HMACHeader(http.MethodGet, host, getPath, accessID, secret, time.Now().UTC())
	get := httptest.NewRequest(http.MethodGet, getPath, nil)
	get.Host = host
	get.Header.Set("Authorization", auth)
	get.Header.Set("x-goog-date", date)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("xml get: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "q1-bytes" {
		t.Fatalf("get body=%q", rec.Body.String())
	}

	listPath := "/storage/xml/hmac-xml"
	auth, date = store.SignGOOG4HMACHeader(http.MethodGet, host, listPath, accessID, secret, time.Now().UTC())
	list := httptest.NewRequest(http.MethodGet, listPath+"?versions=true", nil)
	list.Host = host
	list.Header.Set("Authorization", auth)
	list.Header.Set("x-goog-date", date)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, list)
	if rec.Code != http.StatusOK {
		t.Fatalf("xml list: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "finance/q1.csv") {
		t.Fatalf("list xml=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<Generation>") {
		t.Fatalf("expected generation in versions list: %s", rec.Body.String())
	}

	listKeys := httptest.NewRequest(http.MethodGet, "/storage/v1/projects/"+project+"/hmacKeys", nil)
	listKeys.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, listKeys)
	if rec.Code != http.StatusOK {
		t.Fatalf("list hmac keys: %d %s", rec.Code, rec.Body.String())
	}
}
