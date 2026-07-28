package certificatemanager_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/certificatemanager"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func cmMux(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureRoot("noctaxris-gcp-local", root); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	principalFrom := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	}
	(&certificatemanager.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, principalFrom)
	return mux, "noctaxris-gcp-local"
}

func TestCertificatesAndMapsCRUD(t *testing.T) {
	mux, project := cmMux(t)
	loc := certificatemanager.DefaultLocation
	certBase := "/v1/projects/" + project + "/locations/" + loc + "/certificates"

	req := httptest.NewRequest(http.MethodPost, certBase+"?certificateId=lab-cert", bytes.NewReader([]byte(
		`{"description":"lab","managed":{"domains":["example.com"]},"scope":"DEFAULT"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create cert status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cert map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &cert)
	if cert["name"] != "projects/"+project+"/locations/"+loc+"/certificates/lab-cert" {
		t.Fatalf("cert=%#v", cert)
	}

	req = httptest.NewRequest(http.MethodGet, certBase+"/lab-cert", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get cert status=%d body=%s", rec.Code, rec.Body.String())
	}

	mapBase := "/v1/projects/" + project + "/locations/" + loc + "/certificateMaps"
	req = httptest.NewRequest(http.MethodPost, mapBase+"?certificateMapId=lab-map", bytes.NewReader([]byte(
		`{"description":"lab map"}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create map status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, mapBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list maps status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	maps, _ := list["certificateMaps"].([]any)
	if len(maps) != 1 {
		t.Fatalf("maps=%#v", list)
	}

	req = httptest.NewRequest(http.MethodDelete, certBase+"/lab-cert", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete cert status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, mapBase+"/lab-map", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete map status=%d", rec.Code)
	}
}

func TestCertificateManagerAuthzUnauthenticated(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mux := http.NewServeMux()
	principalFrom := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	}
	(&certificatemanager.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, principalFrom)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/p/locations/global/certificates", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}
