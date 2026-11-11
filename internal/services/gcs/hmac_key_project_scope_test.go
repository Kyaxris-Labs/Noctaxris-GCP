package gcs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	gcshandler "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/gcs"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestHMACKeyGetDeleteRequiresURLProject(t *testing.T) {
	_, st, defaultProject := openGCS(t)
	const (
		projectQ      = "quartz-owned"
		projectP      = "pebble-other"
		projectPrefix = "quartz"
	)
	email := "hmac-admin@" + projectQ + ".iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: projectQ, Email: email, UniqueID: "hmac-admin-quartz", DisplayName: "hmac-admin",
	}); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{projectQ, projectP, projectPrefix} {
		if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
			Bindings: []authz.Binding{{
				Role:    "roles/storage.hmacKeyAdmin",
				Members: []string{"serviceAccount:" + email},
			}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	h := &gcshandler.Handler{
		Store:          st,
		Authz:          &authz.Evaluator{Policies: st},
		DefaultProject: defaultProject,
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: email, IsRoot: false}, true
		},
	}
	h.Register(mux)

	k, err := st.CreateHMACKey(projectQ, email)
	if err != nil {
		t.Fatal(err)
	}

	rec := serveHMACKey(mux, http.MethodGet, projectP, k.AccessID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get under %s status=%d body=%s", projectP, rec.Code, rec.Body.String())
	}
	rec = serveHMACKey(mux, http.MethodDelete, projectP, k.AccessID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete under %s status=%d body=%s", projectP, rec.Code, rec.Body.String())
	}
	requireStoredHMACKey(t, st, k.AccessID, projectQ)

	rec = serveHMACKey(mux, http.MethodGet, projectPrefix, k.AccessID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get under prefix %s status=%d body=%s", projectPrefix, rec.Code, rec.Body.String())
	}
	rec = serveHMACKey(mux, http.MethodDelete, projectPrefix, k.AccessID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete under prefix %s status=%d body=%s", projectPrefix, rec.Code, rec.Body.String())
	}
	requireStoredHMACKey(t, st, k.AccessID, projectQ)

	const missing = "GOOG1MISSINGACCESSID"
	rec = serveHMACKey(mux, http.MethodGet, projectQ, missing)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = serveHMACKey(mux, http.MethodDelete, projectQ, missing)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	lab, ok, err := st.GetHMACKey(store.LabGCSHMACAccessID)
	if err != nil || !ok || lab.ProjectID != "" {
		t.Fatalf("lab key ok=%v err=%v project=%q", ok, err, labProject(lab))
	}
	rec = serveHMACKey(mux, http.MethodGet, projectQ, store.LabGCSHMACAccessID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty project get status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = serveHMACKey(mux, http.MethodDelete, projectQ, store.LabGCSHMACAccessID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty project delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	lab, ok, err = st.GetHMACKey(store.LabGCSHMACAccessID)
	if err != nil || !ok || lab.ProjectID != "" || lab.Secret != store.LabGCSHMACSecret {
		t.Fatalf("lab key after delete ok=%v err=%v got=%#v", ok, err, lab)
	}
	requireStoredHMACKey(t, st, k.AccessID, projectQ)

	rec = serveHMACKey(mux, http.MethodGet, projectQ, k.AccessID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get under %s status=%d body=%s", projectQ, rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["accessId"] != k.AccessID || got["projectId"] != projectQ {
		t.Fatalf("got=%#v", got)
	}
	if _, hasSecret := got["secret"]; hasSecret {
		t.Fatalf("get must hide secret: %#v", got)
	}

	rec = serveHMACKey(mux, http.MethodDelete, projectQ, k.AccessID)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete under %s status=%d body=%s", projectQ, rec.Code, rec.Body.String())
	}
	rec = serveHMACKey(mux, http.MethodGet, projectQ, k.AccessID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	gone, ok, err := st.GetHMACKey(k.AccessID)
	if err != nil || ok {
		t.Fatalf("stored after delete ok=%v err=%v got=%#v", ok, err, gone)
	}
}

func serveHMACKey(mux http.Handler, method, project, accessID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/storage/v1/projects/"+project+"/hmacKeys/"+accessID, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func requireStoredHMACKey(t *testing.T, st *store.Store, accessID, project string) {
	t.Helper()
	got, ok, err := st.GetHMACKey(accessID)
	if err != nil || !ok {
		t.Fatalf("key %s present=%v err=%v", accessID, ok, err)
	}
	if got.ProjectID != project || got.AccessID != accessID {
		t.Fatalf("stored=%#v", got)
	}
}

func labProject(k *store.HMACKey) string {
	if k == nil {
		return ""
	}
	return k.ProjectID
}
