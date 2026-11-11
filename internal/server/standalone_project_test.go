package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/server"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

const standaloneSeedPath = "/_noctaxris-gcp/lab/projects:seedStandalone"

func openStandaloneServer(t *testing.T, enabled bool) (*server.Server, config.Config, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.Config{
		ListenAddr:         "127.0.0.1:0",
		DataRoot:           filepath.Join(dir, "data"),
		RootServiceAccount: "root@noctaxris-gcp-local.iam.gserviceaccount.com",
		RootAccessToken:    "test-root-token",
		ProjectID:          "noctaxris-gcp-local",
		LabStandalone:      enabled,
	}
	if err := st.EnsureRoot(cfg.ProjectID, cfg.RootServiceAccount); err != nil {
		t.Fatal(err)
	}
	return server.New(cfg, st, nil), cfg, st
}

func postSeed(srv *server.Server, token, name string) *httptest.ResponseRecorder {
	body := []byte(`{}`)
	if name != "" {
		raw, _ := json.Marshal(map[string]string{"name": name})
		body = raw
	}
	req := httptest.NewRequest(http.MethodPost, standaloneSeedPath, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestStandaloneSeedDisabledIs404(t *testing.T) {
	srv, cfg, _ := openStandaloneServer(t, false)
	if authn.IsPublicPath(standaloneSeedPath, false) {
		t.Fatal("seed path must not be public")
	}
	rec := postSeed(srv, "", "seed-disabled")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = postSeed(srv, cfg.RootAccessToken, "seed-disabled")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "PERMISSION_DENIED") {
		t.Fatalf("disabled seed must not be PermissionDenied: %s", rec.Body.String())
	}
}

func TestStandaloneSeedRootGateAndReach(t *testing.T) {
	srv, cfg, st := openStandaloneServer(t, true)
	if authn.IsPublicPath(standaloneSeedPath, false) || authn.IsPublicPath(standaloneSeedPath, true) {
		t.Fatal("seed path must not be public")
	}

	rec := postSeed(srv, "", "unsigned-name")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "accessToken") {
		t.Fatalf("unsigned body = %s", rec.Body.String())
	}

	ownerTok, _, err := labtoken.Mint(st, cfg.RootServiceAccount, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	rec = postSeed(srv, ownerTok, "owner-not-root")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("owner token status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), cfg.RootAccessToken) {
		t.Fatal("denied response included the root token")
	}

	rec = postSeed(srv, cfg.RootAccessToken, "solo-http")
	if rec.Code != http.StatusOK {
		t.Fatalf("seed status=%d body=%s", rec.Code, rec.Body.String())
	}
	var seeded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &seeded); err != nil {
		t.Fatal(err)
	}
	token, _ := seeded["accessToken"].(string)
	email, _ := seeded["serviceAccountEmail"].(string)
	projectID, _ := seeded["projectId"].(string)
	if token == "" || token == cfg.RootAccessToken {
		t.Fatalf("access token = %q", token)
	}
	if email == "" || email == cfg.RootServiceAccount || strings.Contains(email, "root@") {
		t.Fatalf("email = %q", email)
	}
	if strings.Contains(rec.Body.String(), cfg.RootAccessToken) {
		t.Fatal("response included the root token")
	}
	principal, err := srvAuth(cfg, st).AuthenticateToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.IsRoot || principal.Email != email || principal.Email == cfg.RootServiceAccount {
		t.Fatalf("principal = %#v", principal)
	}

	rec = postSeed(srv, cfg.RootAccessToken, "solo-http")
	if rec.Code != http.StatusOK {
		t.Fatalf("reseed status=%d body=%s", rec.Code, rec.Body.String())
	}
	var again map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &again); err != nil {
		t.Fatal(err)
	}
	if again["accessToken"] != token || again["serviceAccountEmail"] != email {
		t.Fatalf("reseed = %#v", again)
	}

	proj := getJSON(t, srv, token, http.MethodGet, "/v3/projects/"+projectID, nil)
	if _, exists := proj["parent"]; exists {
		t.Fatalf("v3 parent = %#v", proj["parent"])
	}
	ancestry := getJSON(t, srv, token, http.MethodPost, "/v1/projects/"+projectID+":getAncestry", []byte(`{}`))
	anc, _ := ancestry["ancestor"].([]any)
	if len(anc) != 1 {
		t.Fatalf("ancestry = %#v", ancestry["ancestor"])
	}

	list := getJSON(t, srv, cfg.RootAccessToken, http.MethodGet, "/v1/projects", nil)
	projects, _ := list["projects"].([]any)
	sawStandalone := false
	sawDefault := false
	for _, item := range projects {
		row, _ := item.(map[string]any)
		switch row["projectId"] {
		case projectID:
			sawStandalone = true
			if _, exists := row["parent"]; exists {
				t.Fatalf("v1 parent = %#v", row["parent"])
			}
		case cfg.ProjectID:
			sawDefault = true
			parent, _ := row["parent"].(map[string]any)
			if parent["type"] != "organization" || parent["id"] != store.DefaultOrganizationID {
				t.Fatalf("default v1 parent = %#v", row["parent"])
			}
		}
	}
	if !sawStandalone || !sawDefault {
		t.Fatalf("v1 list missing projects: %#v", list)
	}

	defAnc := getJSON(t, srv, cfg.RootAccessToken, http.MethodPost, "/v1/projects/"+cfg.ProjectID+":getAncestry", []byte(`{}`))
	defList, _ := defAnc["ancestor"].([]any)
	if len(defList) < 2 {
		t.Fatalf("default ancestry = %#v", defAnc["ancestor"])
	}
	orgNode, _ := defList[1].(map[string]any)
	orgID, _ := orgNode["resourceId"].(map[string]any)
	if orgID["type"] != "organization" || orgID["id"] != store.DefaultOrganizationID {
		t.Fatalf("default ancestor = %#v", defList[1])
	}

	child := getJSON(t, srv, cfg.RootAccessToken, http.MethodPost, "/v3/projects", []byte(`{"projectId":"lab-child","parent":"organizations/noctaxris-gcp-org"}`))
	if child["parent"] != store.DefaultOrganizationName {
		t.Fatalf("created parent = %#v", child["parent"])
	}

	orgEmail := "orgwide@example.com"
	policy := []byte(`{"policy":{"etag":"ACAB","bindings":[{"role":"roles/owner","members":["serviceAccount:` + orgEmail + `"]}]}}`)
	orgSet := call(t, srv, cfg.RootAccessToken, http.MethodPost, "/v3/organizations/noctaxris-gcp-org:setIamPolicy", policy)
	if orgSet.Code != http.StatusOK {
		t.Fatalf("set org policy status=%d body=%s", orgSet.Code, orgSet.Body.String())
	}
	orgTok, _, err := labtoken.Mint(st, orgEmail, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if call(t, srv, orgTok, http.MethodGet, "/v3/projects/"+cfg.ProjectID, nil).Code != http.StatusOK {
		t.Fatal("org IAM should grant on the default project")
	}
	if call(t, srv, orgTok, http.MethodGet, "/v3/projects/lab-child", nil).Code != http.StatusOK {
		t.Fatal("org IAM should grant on a lab-org parent project")
	}
	if call(t, srv, orgTok, http.MethodGet, "/v3/projects/"+projectID, nil).Code != http.StatusForbidden {
		t.Fatal("org IAM must not grant on the empty-parent project")
	}
	if call(t, srv, token, http.MethodGet, "/v3/projects/"+cfg.ProjectID, nil).Code != http.StatusForbidden {
		t.Fatal("standalone owner must not get the default project")
	}
	if call(t, srv, token, http.MethodGet, "/v3/organizations/noctaxris-gcp-org", nil).Code != http.StatusForbidden {
		t.Fatal("standalone owner must not get the organization")
	}
	denied := call(t, srv, token, http.MethodPost, "/v3/organizations/noctaxris-gcp-org:setIamPolicy", policy)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("setIamPolicy status=%d body=%s", denied.Code, denied.Body.String())
	}
	if strings.Contains(denied.Body.String(), cfg.RootAccessToken) || strings.Contains(denied.Body.String(), token) {
		t.Fatal("denied setIamPolicy echoed a token")
	}
}

func srvAuth(cfg config.Config, st *store.Store) *authn.Authenticator {
	return &authn.Authenticator{
		RootServiceAccount: cfg.RootServiceAccount,
		RootAccessToken:    cfg.RootAccessToken,
		Tokens:             st,
	}
}

func call(t *testing.T, srv *server.Server, token, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestStandaloneProjectListDoesNotCrossParents(t *testing.T) {
	srv, cfg, _ := openStandaloneServer(t, true)
	rec := postSeed(srv, cfg.RootAccessToken, "list-home")
	if rec.Code != http.StatusOK {
		t.Fatalf("seed status=%d body=%s", rec.Code, rec.Body.String())
	}
	var seeded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &seeded); err != nil {
		t.Fatal(err)
	}
	token, _ := seeded["accessToken"].(string)
	projectID, _ := seeded["projectId"].(string)
	own := call(t, srv, token, http.MethodGet, "/v3/projects?parent=projects/"+projectID, nil)
	if own.Code != http.StatusOK {
		t.Fatalf("own list status=%d body=%s", own.Code, own.Body.String())
	}
	if strings.Contains(own.Body.String(), cfg.ProjectID) {
		t.Fatalf("standalone list included the default project: %s", own.Body.String())
	}
	if !strings.Contains(own.Body.String(), projectID) {
		t.Fatalf("standalone list omitted the home project: %s", own.Body.String())
	}
	org := call(t, srv, cfg.RootAccessToken, http.MethodGet, "/v3/projects?parent=organizations/noctaxris-gcp-org", nil)
	if org.Code != http.StatusOK {
		t.Fatalf("org list status=%d body=%s", org.Code, org.Body.String())
	}
	if strings.Contains(org.Body.String(), projectID) {
		t.Fatalf("org list included the empty-parent project: %s", org.Body.String())
	}
	if !strings.Contains(org.Body.String(), cfg.ProjectID) {
		t.Fatalf("org list omitted the default project: %s", org.Body.String())
	}
}

func getJSON(t *testing.T, srv *server.Server, token, method, path string, body []byte) map[string]any {
	t.Helper()
	rec := call(t, srv, token, method, path, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s status=%d body=%s", method, path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
