package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestStandaloneSeedTokenIsNotRootAndRepeats(t *testing.T) {
	srv, cfg, _ := openStandaloneServer(t, true)
	first := postSeed(srv, cfg.RootAccessToken, "repeat-home")
	token, email, projectID := decodeSeed(t, first)
	if token == cfg.RootAccessToken {
		t.Fatalf("access token = %q", token)
	}
	if email == "" || email == cfg.RootServiceAccount {
		t.Fatalf("email = %q", email)
	}
	if strings.Contains(first.Body.String(), cfg.RootAccessToken) {
		t.Fatal("response included the root token")
	}

	second := postSeed(srv, cfg.RootAccessToken, "repeat-home")
	againToken, _, againProject := decodeSeed(t, second)
	if againToken != token || againProject != projectID {
		t.Fatalf("reseed token=%q project=%q want token=%q project=%q", againToken, againProject, token, projectID)
	}
}

func TestStandaloneOwnerDeniedOnDefaultAndOrgPolicy(t *testing.T) {
	srv, cfg, _ := openStandaloneServer(t, true)
	seeded := postSeed(srv, cfg.RootAccessToken, "owner-home")
	token, _, projectID := decodeSeed(t, seeded)

	denied := call(t, srv, token, http.MethodGet, "/v3/projects/"+cfg.ProjectID, nil)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("default project status=%d body=%s", denied.Code, denied.Body.String())
	}
	if strings.Contains(denied.Body.String(), token) || strings.Contains(denied.Body.String(), cfg.RootAccessToken) {
		t.Fatal("denied get echoed a token")
	}

	home := call(t, srv, token, http.MethodGet, "/v3/projects/"+projectID, nil)
	if home.Code != http.StatusOK {
		t.Fatalf("home project status=%d body=%s", home.Code, home.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(home.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["projectId"] != projectID {
		t.Fatalf("home project = %#v", got)
	}

	policy := []byte(`{"policy":{"etag":"ACAB","bindings":[{"role":"roles/owner","members":["serviceAccount:outsider@example.com"]}]}}`)
	orgSet := call(t, srv, token, http.MethodPost, "/v3/"+store.DefaultOrganizationName+":setIamPolicy", policy)
	if orgSet.Code != http.StatusForbidden {
		t.Fatalf("setIamPolicy status=%d body=%s", orgSet.Code, orgSet.Body.String())
	}
	if strings.Contains(orgSet.Body.String(), token) || strings.Contains(orgSet.Body.String(), cfg.RootAccessToken) {
		t.Fatal("denied setIamPolicy echoed a token")
	}
}

func TestStandaloneSeedUnsignedIs401AndDisabledIs404(t *testing.T) {
	srv, cfg, _ := openStandaloneServer(t, false)
	unsigned := postSeed(srv, "", "seed-disabled-name")
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned status=%d body=%s", unsigned.Code, unsigned.Body.String())
	}
	if strings.Contains(unsigned.Body.String(), "accessToken") {
		t.Fatalf("unsigned body = %s", unsigned.Body.String())
	}
	rec := postSeed(srv, cfg.RootAccessToken, "seed-disabled-name")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "PERMISSION_DENIED") {
		t.Fatalf("disabled seed must not be PermissionDenied: %s", rec.Body.String())
	}
}

func TestStandaloneSeedInvalidNameLeavesNoProject(t *testing.T) {
	srv, cfg, st := openStandaloneServer(t, true)
	before, err := st.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	// validStandaloneProjectID rejects space, slash, colon, and "-". Uppercase is allowed.
	names := []string{"bad name", "has/slash", "colon:name", "-"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			rec := postSeed(srv, cfg.RootAccessToken, name)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "name is not a valid project id") {
				t.Fatalf("body=%s", rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "accessToken") || strings.Contains(rec.Body.String(), cfg.RootAccessToken) {
				t.Fatalf("body=%s", rec.Body.String())
			}
			_, ok, err := st.GetProject(strings.TrimSpace(name))
			if err != nil || ok {
				t.Fatalf("project row ok=%v err=%v", ok, err)
			}
		})
	}
	after, err := st.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("projects before=%d after=%d", len(before), len(after))
	}
	for _, project := range after {
		for _, name := range names {
			if project.ID == strings.TrimSpace(name) {
				t.Fatalf("invalid seed left project %q", project.ID)
			}
		}
	}
}

func decodeSeed(t *testing.T, rec *httptest.ResponseRecorder) (token, email, projectID string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("seed status=%d body=%s", rec.Code, rec.Body.String())
	}
	var seeded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &seeded); err != nil {
		t.Fatal(err)
	}
	token, _ = seeded["accessToken"].(string)
	email, _ = seeded["serviceAccountEmail"].(string)
	projectID, _ = seeded["projectId"].(string)
	if token == "" || projectID == "" {
		t.Fatalf("seed = %#v", seeded)
	}
	return token, email, projectID
}
