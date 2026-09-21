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

func TestGCSXMLHMACObjectGetDeniedWithoutIAM(t *testing.T) {
	mux, st, project := openGCS(t)
	host := "127.0.0.1:4588"

	create := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project, strings.NewReader(`{"name":"hmac-deny","location":"US"}`))
	create.Header.Set("Content-Type", "application/json")
	create.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, create)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bucket: %d %s", rec.Code, rec.Body.String())
	}

	sa := "hmac-no-get@" + project + ".iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: project, Email: sa, UniqueID: "hmac-no-get", DisplayName: "hmac-no-get",
	}); err != nil {
		t.Fatal(err)
	}
	hmacReq := httptest.NewRequest(http.MethodPost, "/storage/v1/projects/"+project+"/hmacKeys?serviceAccountEmail="+sa, nil)
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

	putPath := "/storage/xml/hmac-deny/secret.txt"
	auth, date := store.SignGOOG4HMACHeader(http.MethodPut, host, putPath, accessID, secret, time.Now().UTC())
	put := httptest.NewRequest(http.MethodPut, putPath, strings.NewReader("nope"))
	put.Host = host
	put.Header.Set("Authorization", auth)
	put.Header.Set("x-goog-date", date)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, put)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("xml put without create IAM status=%d body=%s", rec.Code, rec.Body.String())
	}

	up := httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/hmac-deny/o?uploadType=media&name=secret.txt", strings.NewReader("held"))
	up.Header.Set("Content-Type", "text/plain")
	up.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, up)
	if rec.Code != http.StatusOK {
		t.Fatalf("root json upload: %d %s", rec.Code, rec.Body.String())
	}

	getPath := putPath
	auth, date = store.SignGOOG4HMACHeader(http.MethodGet, host, getPath, accessID, secret, time.Now().UTC())
	get := httptest.NewRequest(http.MethodGet, getPath, nil)
	get.Host = host
	get.Header.Set("Authorization", auth)
	get.Header.Set("x-goog-date", date)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, get)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("xml get without object get IAM status=%d body=%s", rec.Code, rec.Body.String())
	}
}
