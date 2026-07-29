package iam_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/iam"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type iamTestHarness struct {
	mux    *http.ServeMux
	store  *store.Store
	setWho func(email string, isRoot bool)
}

func openIAM(t *testing.T) *iamTestHarness {
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
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}

	h := &iamTestHarness{store: st}
	mux := http.NewServeMux()
	var who authn.Principal
	h.setWho = func(email string, isRoot bool) {
		who = authn.Principal{Email: email, IsRoot: isRoot}
	}
	ih := &iam.Handler{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		Principal: func(r *http.Request) (authn.Principal, bool) {
			if who.Email == "" && !who.IsRoot {
				return authn.Principal{}, false
			}
			return who, true
		},
	}
	ih.Mount(mux)
	h.mux = mux
	return h
}

func seedServiceAccount(t *testing.T, st *store.Store, project, accountID string) string {
	t.Helper()
	email := accountID + "@" + project + ".iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID:   project,
		Email:       email,
		UniqueID:    accountID,
		DisplayName: accountID,
	}); err != nil {
		t.Fatal(err)
	}
	return email
}

func TestSTSTokenExchangeHappyAndFail(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	pool, err := h.store.CreateWIFPool(project, "global", "sts-pool", "STS", "", false)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := h.store.CreateWIFProvider(pool.Name, "oidc", "OIDC", "", "https://example.com", "", false)
	if err != nil {
		t.Fatal(err)
	}

	badGrant := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(
		"grant_type=client_credentials&audience="+url.QueryEscape(prov.Name)+"&subject_token=x"))
	badGrant.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, badGrant)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad grant_type status=%d body=%s", rec.Code, rec.Body.String())
	}

	missingSub := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(
		"grant_type="+url.QueryEscape(iam.GrantTypeTokenExchange)+"&audience="+url.QueryEscape(prov.Name)))
	missingSub.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, missingSub)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing subject_token status=%d body=%s", rec.Code, rec.Body.String())
	}

	unknown := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(
		"grant_type="+url.QueryEscape(iam.GrantTypeTokenExchange)+
			"&audience="+url.QueryEscape(pool.Name+"/providers/missing")+
			"&subject_token=lab"))
	unknown.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, unknown)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown provider status=%d body=%s", rec.Code, rec.Body.String())
	}

	jsonBody := `{"grant_type":"` + iam.GrantTypeTokenExchange + `","audience":"//iam.googleapis.com/` + prov.Name + `","subject_token":"json-sub"}`
	ok := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(jsonBody))
	ok.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, ok)
	if rec.Code != http.StatusOK {
		t.Fatalf("JSON exchange status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sts map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &sts); err != nil {
		t.Fatal(err)
	}
	if sts["access_token"] == "" || sts["token_type"] != "Bearer" {
		t.Fatalf("sts resp = %#v", sts)
	}
}

func TestGenerateAccessTokenTokenCreatorGrantDeny(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	projectRes := "projects/" + project
	caller := seedServiceAccount(t, h.store, project, "tok-caller")
	target := seedServiceAccount(t, h.store, project, "tok-target")
	scopeBody := []byte(`{"scope":["https://www.googleapis.com/auth/cloud-platform"],"lifetime":"600s"}`)

	h.setWho(caller, false)
	deny := httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/serviceAccounts/"+target+":generateAccessToken", bytes.NewReader(scopeBody))
	deny.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, deny)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("without TokenCreator expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	saRes := projectRes + "/serviceAccounts/" + target
	pol := authz.Policy{
		Etag: "tc1",
		Bindings: []authz.Binding{{
			Role:    "roles/iam.serviceAccountTokenCreator",
			Members: []string{"serviceAccount:" + caller},
		}},
	}
	if err := h.store.PutIAMPolicyJSON(saRes, pol); err != nil {
		t.Fatal(err)
	}

	allow := httptest.NewRequest(http.MethodPost,
		"/v1/projects/-/serviceAccounts/"+target+":generateAccessToken", bytes.NewReader(scopeBody))
	allow.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, allow)
	if rec.Code != http.StatusOK {
		t.Fatalf("with TokenCreator expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var tok map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil {
		t.Fatal(err)
	}
	if tok["accessToken"] == "" {
		t.Fatalf("token resp = %#v", tok)
	}
}

func TestGenerateAccessTokenViewerDenied(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	projectRes := "projects/" + project
	viewer := seedServiceAccount(t, h.store, project, "proj-viewer")
	target := seedServiceAccount(t, h.store, project, "imp-target")

	if err := h.store.PutIAMPolicyJSON(projectRes, authz.Policy{
		Etag: "v1",
		Bindings: []authz.Binding{{
			Role:    "roles/viewer",
			Members: []string{"serviceAccount:" + viewer},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	h.setWho(viewer, false)
	body := []byte(`{"scope":["https://www.googleapis.com/auth/cloud-platform"]}`)
	req := httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/serviceAccounts/"+target+":generateAccessToken", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer generateAccessToken expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
