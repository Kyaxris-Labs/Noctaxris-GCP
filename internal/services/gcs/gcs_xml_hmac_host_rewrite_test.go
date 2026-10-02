package gcs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestGCSXMLHMACStorageGoogleapisHostRewrite(t *testing.T) {
	mux, st, project := openGCS(t)
	bucket := "hmac-host-rw"
	object := "finance/q1.csv"
	sa := "hmac-host@" + project + ".iam.gserviceaccount.com"
	const cloudHost = "storage.googleapis.com"
	loopback := "127.0.0.1:4588"
	wirePath := "/" + bucket + "/" + object
	xmlPath := "/storage/xml/" + bucket + "/" + object

	create := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project,
		strings.NewReader(`{"name":"`+bucket+`","location":"US"}`))
	create.Header.Set("Content-Type", "application/json")
	create.Host = loopback
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, create)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bucket: %d %s", rec.Code, rec.Body.String())
	}

	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: project, Email: sa, UniqueID: "hmac-host", DisplayName: "hmac-host",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/storage.objectAdmin",
			Members: []string{"serviceAccount:" + sa},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	hmacReq := httptest.NewRequest(http.MethodPost,
		"/storage/v1/projects/"+project+"/hmacKeys?serviceAccountEmail="+sa, nil)
	hmacReq.Host = loopback
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

	// 1) Host storage.googleapis.com, sign wire path; request path already rewritten
	// (as rewriteLabHostPath would leave it) must succeed.
	auth, date := store.SignGOOG4HMACHeader(http.MethodPut, cloudHost, wirePath, accessID, secret, time.Now().UTC(), nil)
	put := httptest.NewRequest(http.MethodPut, xmlPath, strings.NewReader("q1-bytes"))
	put.Host = cloudHost
	put.Header.Set("Authorization", auth)
	put.Header.Set("x-goog-date", date)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, put)
	if rec.Code != http.StatusOK {
		t.Fatalf("wire-path put after rewrite: %d %s", rec.Code, rec.Body.String())
	}

	auth, date = store.SignGOOG4HMACHeader(http.MethodGet, cloudHost, wirePath, accessID, secret, time.Now().UTC(), nil)
	get := httptest.NewRequest(http.MethodGet, xmlPath, nil)
	get.Host = cloudHost
	get.Header.Set("Authorization", auth)
	get.Header.Set("x-goog-date", date)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("wire-path get after rewrite: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "q1-bytes" {
		t.Fatalf("get body=%q", rec.Body.String())
	}

	// 2) Host storage.googleapis.com, sign /storage/xml/bucket/obj still works.
	auth, date = store.SignGOOG4HMACHeader(http.MethodGet, cloudHost, xmlPath, accessID, secret, time.Now().UTC(), nil)
	getXML := httptest.NewRequest(http.MethodGet, xmlPath, nil)
	getXML.Host = cloudHost
	getXML.Header.Set("Authorization", auth)
	getXML.Header.Set("x-goog-date", date)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, getXML)
	if rec.Code != http.StatusOK {
		t.Fatalf("signed xml-path get on cloud host: %d %s", rec.Code, rec.Body.String())
	}

	// 3) Host 127.0.0.1:4588 + /storage/xml/... still works.
	auth, date = store.SignGOOG4HMACHeader(http.MethodGet, loopback, xmlPath, accessID, secret, time.Now().UTC(), nil)
	getLoop := httptest.NewRequest(http.MethodGet, xmlPath, nil)
	getLoop.Host = loopback
	getLoop.Header.Set("Authorization", auth)
	getLoop.Header.Set("x-goog-date", date)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, getLoop)
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback xml-path get: %d %s", rec.Code, rec.Body.String())
	}

	// 4) Wrong signature still 401.
	auth, date = store.SignGOOG4HMACHeader(http.MethodGet, cloudHost, wirePath, accessID, secret, time.Now().UTC(), nil)
	bad := httptest.NewRequest(http.MethodGet, xmlPath, nil)
	bad.Host = cloudHost
	bad.Header.Set("Authorization", auth+"deadbeef")
	bad.Header.Set("x-goog-date", date)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, bad)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong signature status=%d body=%s", rec.Code, rec.Body.String())
	}
}
