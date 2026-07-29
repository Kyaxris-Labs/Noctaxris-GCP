package securitycenter_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/securitycenter"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func sccMux(t *testing.T, inject bool) (*http.ServeMux, *store.Store, string) {
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
	(&securitycenter.Service{
		Store: st, Authz: &authz.Evaluator{Policies: st, Parents: st},
		InjectEnabled: inject,
	}).Mount(mux, principalFrom)
	return mux, st, "noctaxris-gcp-local"
}

func TestSourcesAndFindingsCRUD(t *testing.T) {
	mux, _, project := sccMux(t, false)
	org := store.DefaultOrganizationID
	srcBase := "/v1/organizations/" + org + "/sources"

	req := httptest.NewRequest(http.MethodPost, srcBase+"?sourceId=lab-src", bytes.NewReader([]byte(
		`{"displayName":"Lab Source","description":"forensics"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create source status=%d body=%s", rec.Code, rec.Body.String())
	}
	var src map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &src)
	wantName := "organizations/" + org + "/sources/lab-src"
	if src["name"] != wantName {
		t.Fatalf("source=%#v", src)
	}

	findBase := srcBase + "/lab-src/findings"
	req = httptest.NewRequest(http.MethodPost, findBase+"?findingId=f1", bytes.NewReader([]byte(
		`{"category":"OPEN_FIREWALL","severity":"HIGH","resourceName":"//compute.googleapis.com/projects/`+project+`/global/networks/default","description":"open fw"}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create finding status=%d body=%s", rec.Code, rec.Body.String())
	}
	var finding map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &finding)
	if finding["category"] != "OPEN_FIREWALL" || finding["state"] != "ACTIVE" {
		t.Fatalf("finding=%#v", finding)
	}

	req = httptest.NewRequest(http.MethodGet, findBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list findings status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	results, _ := list["listFindingsResults"].([]any)
	if len(results) != 1 {
		t.Fatalf("list=%#v", list)
	}

	req = httptest.NewRequest(http.MethodPost, findBase+"/f1:setState", bytes.NewReader([]byte(`{"state":"INACTIVE"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setState status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &finding)
	if finding["state"] != "INACTIVE" {
		t.Fatalf("expected INACTIVE: %#v", finding)
	}

	// Project-scoped mirror path
	projSrc := "/v1/projects/" + project + "/sources"
	req = httptest.NewRequest(http.MethodPost, projSrc+"?sourceId=proj-src", bytes.NewReader([]byte(`{"displayName":"P"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("project create source status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthzFailClosed(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&securitycenter.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Parents: st}}).Mount(mux,
		func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
		})
	req := httptest.NewRequest(http.MethodGet, "/v1/organizations/"+store.DefaultOrganizationID+"/sources", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthzUnauthenticated(t *testing.T) {
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
	(&securitycenter.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux,
		func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{}, false
		})
	req := httptest.NewRequest(http.MethodGet, "/v1/organizations/noctaxris-gcp-org/sources", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestInjectFindingsEnvGated(t *testing.T) {
	muxOff, _, _ := sccMux(t, false)
	body := `{"parent":"organizations/` + store.DefaultOrganizationID + `","sourceId":"inj","findings":[{"category":"MALWARE","severity":"CRITICAL","findingId":"x1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/securitycenter:injectFindings", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	muxOff.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("inject disabled expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	muxOn, _, _ := sccMux(t, true)
	req = httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/securitycenter:injectFindings", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	muxOn.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("inject enabled status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	names, _ := out["findingNames"].([]any)
	if len(names) != 1 {
		t.Fatalf("inject response=%#v", out)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/organizations/"+store.DefaultOrganizationID+"/sources/inj/findings", nil)
	rec = httptest.NewRecorder()
	muxOn.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list after inject status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	results, _ := list["listFindingsResults"].([]any)
	if len(results) != 1 {
		t.Fatalf("list after inject=%#v", list)
	}
}
