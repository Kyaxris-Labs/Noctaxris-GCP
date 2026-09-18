package firebaseauth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/firebaseauth"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func lookupPath() string {
	return "/identitytoolkit.googleapis.com/v1/accounts:lookup"
}

func postLookup(mux *http.ServeMux, body string, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, lookupPath(), bytes.NewReader([]byte(body)))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func mountLookup(t *testing.T, who func(*http.Request) (authn.Principal, bool), authenticator *authn.Authenticator) (*http.ServeMux, *store.Store) {
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
	project := "noctaxris-gcp-local"
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &firebaseauth.Service{
		Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}, Authn: authenticator, DefaultProject: project,
	}
	svc.Mount(mux, who)
	return mux, st
}

func denyHasNoUserDump(t *testing.T, rec *httptest.ResponseRecorder, localID, email string) {
	t.Helper()
	raw := rec.Body.Bytes()
	if bytes.Contains(raw, []byte(`"users"`)) {
		t.Fatalf("denied lookup must not dump users: %s", rec.Body.String())
	}
	if localID != "" && bytes.Contains(raw, []byte(localID)) {
		t.Fatalf("denied lookup leaked localId: %s", rec.Body.String())
	}
	if email != "" && bytes.Contains(raw, []byte(email)) {
		t.Fatalf("denied lookup leaked email: %s", rec.Body.String())
	}
}

func TestAccountsLookupIdTokenSelfAndAdminIdentifiers(t *testing.T) {
	anon := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	}
	const rootToken = "lab-root-lookup-token"
	auth := &authn.Authenticator{
		RootServiceAccount: "root@noctaxris-gcp-local.iam.gserviceaccount.com",
		RootAccessToken:    rootToken,
	}
	mux, _ := mountLookup(t, anon, auth)
	localID, idToken := signUpUser(t, mux, "lookup-self@example.com")
	otherID, _ := signUpUser(t, mux, "lookup-other@example.com")

	t.Run("idToken only returns that uid", func(t *testing.T) {
		rec := postLookup(mux, `{"idToken":"`+idToken+`"}`, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		users, _ := out["users"].([]any)
		if len(users) != 1 {
			t.Fatalf("users=%#v", out)
		}
		row, _ := users[0].(map[string]any)
		if row["localId"] != localID || row["email"] != "lookup-self@example.com" {
			t.Fatalf("row=%#v", row)
		}
	})

	t.Run("empty body missing idToken", func(t *testing.T) {
		rec := postLookup(mux, `{}`, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("MISSING_ID_TOKEN")) {
			t.Fatalf("body=%s", rec.Body.String())
		}
	})

	denies := []struct {
		name, body string
	}{
		{"email array", `{"email":["lookup-self@example.com"]}`},
		{"localId array", `{"localId":["` + localID + `"]}`},
		{"phone array", `{"phoneNumber":["+15555550100"]}`},
		{"federated array", `{"federatedUserId":[{"providerId":"google.com","rawId":"abc"}]}`},
		{"idToken plus other email", `{"idToken":"` + idToken + `","email":["lookup-other@example.com"]}`},
	}
	for _, tc := range denies {
		t.Run("unauthenticated "+tc.name, func(t *testing.T) {
			rec := postLookup(mux, tc.body, "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !bytes.Contains(rec.Body.Bytes(), []byte("MISSING_ID_TOKEN")) {
				t.Fatalf("body=%s", rec.Body.String())
			}
			denyHasNoUserDump(t, rec, localID, "lookup-self@example.com")
			denyHasNoUserDump(t, rec, otherID, "lookup-other@example.com")
		})
	}

	t.Run("authenticated without permission", func(t *testing.T) {
		who := func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
		}
		deniedMux, _ := mountLookup(t, who, nil)
		local, _ := signUpUser(t, deniedMux, "denied-lookup@example.com")
		rec := postLookup(deniedMux, `{"email":["denied-lookup@example.com"]}`, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		denyHasNoUserDump(t, rec, local, "denied-lookup@example.com")
		rec = postLookup(deniedMux, `{"localId":["`+local+`"]}`, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("localId status=%d body=%s", rec.Code, rec.Body.String())
		}
		denyHasNoUserDump(t, rec, local, "denied-lookup@example.com")
	})

	t.Run("root Bearer on public path", func(t *testing.T) {
		rec := postLookup(mux, `{"email":["lookup-self@example.com"]}`, rootToken)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		users, _ := out["users"].([]any)
		if len(users) != 1 {
			t.Fatalf("users=%#v", out)
		}
		row, _ := users[0].(map[string]any)
		if row["localId"] != localID {
			t.Fatalf("row=%#v", row)
		}
	})

	t.Run("IAM users.get", func(t *testing.T) {
		reader := "reader@noctaxris-gcp-local.iam.gserviceaccount.com"
		who := func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: reader, IsRoot: false}, true
		}
		grantedMux, grantedStore := mountLookup(t, who, nil)
		if _, err := grantedStore.CreateCustomRole("noctaxris-gcp-local", "lookupGet", "Lookup get", "", "GA", []string{"firebaseauth.users.get"}); err != nil {
			t.Fatal(err)
		}
		if err := grantedStore.PutIAMPolicyJSON("projects/noctaxris-gcp-local", authz.Policy{
			Bindings: []authz.Binding{{
				Role:    "projects/noctaxris-gcp-local/roles/lookupGet",
				Members: []string{"serviceAccount:" + reader},
			}},
		}); err != nil {
			t.Fatal(err)
		}
		id, _ := signUpUser(t, grantedMux, "iam-lookup@example.com")
		rec := postLookup(grantedMux, `{"localId":["`+id+`"]}`, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte(id)) {
			t.Fatalf("body=%s", rec.Body.String())
		}
	})
}
