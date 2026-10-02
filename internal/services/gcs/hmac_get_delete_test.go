package gcs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHMACKeyGetAndDelete(t *testing.T) {
	mux, _, project := openGCS(t)
	sa := "app@" + project + ".iam.gserviceaccount.com"
	create := httptest.NewRequest(http.MethodPost,
		"/storage/v1/projects/"+project+"/hmacKeys?serviceAccountEmail="+sa, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, create)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	meta, _ := created["metadata"].(map[string]any)
	accessID, _ := meta["accessId"].(string)
	if accessID == "" {
		t.Fatalf("created=%#v", created)
	}

	get := httptest.NewRequest(http.MethodGet, "/storage/v1/projects/"+project+"/hmacKeys/"+accessID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["accessId"] != accessID {
		t.Fatalf("got=%#v", got)
	}
	if _, hasSecret := got["secret"]; hasSecret {
		t.Fatalf("get must hide secret: %#v", got)
	}

	missing := httptest.NewRequest(http.MethodGet, "/storage/v1/projects/"+project+"/hmacKeys/does-not-exist", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, missing)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get status=%d body=%s", rec.Code, rec.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/storage/v1/projects/"+project+"/hmacKeys/"+accessID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, del)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	del = httptest.NewRequest(http.MethodDelete, "/storage/v1/projects/"+project+"/hmacKeys/"+accessID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, del)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	createMissingSA := httptest.NewRequest(http.MethodPost, "/storage/v1/projects/"+project+"/hmacKeys", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, createMissingSA)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing sa status=%d body=%s", rec.Code, rec.Body.String())
	}
}
