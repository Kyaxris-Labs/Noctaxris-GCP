package iam_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWIFProviderCreateEchoesAllowedAudiences(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	h.setWho("root@"+project+".iam.gserviceaccount.com", true)

	poolURL := "/v1/projects/" + project + "/locations/global/workloadIdentityPools?workloadIdentityPoolId=echo-pool"
	poolReq := httptest.NewRequest(http.MethodPost, poolURL, bytes.NewReader([]byte(`{"displayName":"Echo"}`)))
	poolReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, poolReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pool status=%d body=%s", rec.Code, rec.Body.String())
	}

	provURL := "/v1/projects/" + project + "/locations/global/workloadIdentityPools/echo-pool/providers?workloadIdentityPoolProviderId=echo-prov"
	body := []byte(`{"displayName":"EchoProv","oidc":{"issuerUri":"https://issuer.example","allowedAudiences":["https://custom-aud"]}}`)
	provReq := httptest.NewRequest(http.MethodPost, provURL, bytes.NewReader(body))
	provReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, provReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("create provider status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	oidc, ok := created["oidc"].(map[string]any)
	if !ok {
		t.Fatalf("oidc missing in %#v", created)
	}
	auds, ok := oidc["allowedAudiences"].([]any)
	if !ok || len(auds) != 1 || auds[0] != "https://custom-aud" {
		t.Fatalf("create echo audiences = %#v", oidc["allowedAudiences"])
	}

	getURL := "/v1/projects/" + project + "/locations/global/workloadIdentityPools/echo-pool/providers/echo-prov"
	getReq := httptest.NewRequest(http.MethodGet, getURL, nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, getReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("get provider status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	oidc2 := got["oidc"].(map[string]any)
	auds2 := oidc2["allowedAudiences"].([]any)
	if len(auds2) != 1 || auds2[0] != "https://custom-aud" {
		t.Fatalf("get audiences = %#v", oidc2["allowedAudiences"])
	}
}

func TestWIFProviderPATCHAllowedAudiences(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	h.setWho("root@"+project+".iam.gserviceaccount.com", true)

	poolURL := "/v1/projects/" + project + "/locations/global/workloadIdentityPools?workloadIdentityPoolId=patch-pool"
	poolReq := httptest.NewRequest(http.MethodPost, poolURL, bytes.NewReader([]byte(`{}`)))
	poolReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, poolReq)

	provURL := "/v1/projects/" + project + "/locations/global/workloadIdentityPools/patch-pool/providers?workloadIdentityPoolProviderId=patch-prov"
	createBody := []byte(`{"oidc":{"issuerUri":"https://issuer.example","allowedAudiences":["https://old"]}}`)
	provReq := httptest.NewRequest(http.MethodPost, provURL, bytes.NewReader(createBody))
	provReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, provReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	patchURL := "/v1/projects/" + project + "/locations/global/workloadIdentityPools/patch-pool/providers/patch-prov"
	patchBody := []byte(`{"oidc":{"allowedAudiences":["https://new-aud"]},"updateMask":"oidc.allowedAudiences"}`)
	patchReq := httptest.NewRequest(http.MethodPatch, patchURL, bytes.NewReader(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, patchReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}
	var patched map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil {
		t.Fatal(err)
	}
	oidc := patched["oidc"].(map[string]any)
	auds := oidc["allowedAudiences"].([]any)
	if len(auds) != 1 || auds[0] != "https://new-aud" {
		t.Fatalf("patched audiences = %#v", oidc["allowedAudiences"])
	}
	if oidc["issuerUri"] != "https://issuer.example" {
		t.Fatalf("issuer should remain: %#v", oidc)
	}

	// Mask without allowedAudiences body must not silently clear.
	badPatch := []byte(`{"updateMask":"oidc.allowedAudiences"}`)
	badReq := httptest.NewRequest(http.MethodPatch, patchURL, bytes.NewReader(badPatch))
	badReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, badReq)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mask without audiences status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Unrelated mask leaves audiences unchanged.
	dnPatch := []byte(`{"displayName":"Renamed","updateMask":"displayName"}`)
	dnReq := httptest.NewRequest(http.MethodPatch, patchURL, bytes.NewReader(dnPatch))
	dnReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, dnReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("displayName patch status=%d body=%s", rec.Code, rec.Body.String())
	}
	getReq := httptest.NewRequest(http.MethodGet, patchURL, nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, getReq)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	oidcGet := got["oidc"].(map[string]any)
	audsGet := oidcGet["allowedAudiences"].([]any)
	if len(audsGet) != 1 || audsGet[0] != "https://new-aud" {
		t.Fatalf("audiences after displayName patch = %#v", oidcGet["allowedAudiences"])
	}
}
