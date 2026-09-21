package iam_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/iam"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

const vpcscPerimeterDenied = "Request is denied because of VPC Service Controls"

type vpcscCredentialsFixture struct {
	mux     *http.ServeMux
	st      *store.Store
	who     authn.Principal
	project string
	target  string
}

func setupVPCSCCredentials(t *testing.T) *vpcscCredentialsFixture {
	t.Helper()
	t.Setenv("NOCTAXRIS_GCP_VPCSC_ENFORCE", "1")
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
	project := "noctaxris-gcp-local"
	rootSA := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootSA); err != nil {
		t.Fatal(err)
	}
	if err := st.MigrateAccessContextManager(); err != nil {
		t.Fatal(err)
	}
	status, _ := json.Marshal(map[string]any{
		"resources":          []string{"projects/" + project},
		"restrictedServices": []string{"iamcredentials.googleapis.com"},
	})
	polName := store.AccessPolicyResourceName("cred-pol")
	if ok, err := st.CreateAccessPolicy(store.AccessPolicy{
		Name: polName, PolicyID: "cred-pol", Parent: "organizations/noctaxris-gcp-org", Title: "Credentials",
	}); err != nil || !ok {
		t.Fatalf("policy ok=%v err=%v", ok, err)
	}
	if ok, err := st.CreateServicePerimeter(store.ServicePerimeter{
		Name: store.ServicePerimeterResourceName("cred-pol", "cred-edge"), PolicyName: polName,
		PerimeterID: "cred-edge", Title: "credentials", StatusJSON: string(status),
	}); err != nil || !ok {
		t.Fatalf("perimeter ok=%v err=%v", ok, err)
	}

	f := &vpcscCredentialsFixture{
		st:      st,
		who:     authn.Principal{Email: rootSA, IsRoot: true},
		project: project,
	}
	target := seedServiceAccount(t, st, project, "vpc-target")
	f.target = target

	mux := http.NewServeMux()
	h := &iam.Handler{
		Store:     st,
		Authz:     &authz.Evaluator{Policies: st, Roles: st},
		Principal: func(*http.Request) (authn.Principal, bool) { return f.who, true },
	}
	h.Mount(mux)
	f.mux = mux
	return f
}

func (f *vpcscCredentialsFixture) grantTokenCreator(t *testing.T, email string) {
	t.Helper()
	member := email
	if !strings.Contains(email, ":") {
		member = "serviceAccount:" + email
	}
	pol := authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/iam.serviceAccountTokenCreator",
			Members: []string{member},
		}},
	}
	if err := f.st.PutIAMPolicyJSON("projects/"+f.project, pol); err != nil {
		t.Fatal(err)
	}
	if err := f.st.PutIAMPolicyJSON("projects/"+f.project+"/serviceAccounts/"+f.target, pol); err != nil {
		t.Fatal(err)
	}
}

func (f *vpcscCredentialsFixture) post(path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func (f *vpcscCredentialsFixture) generateAccessToken(alias bool) *httptest.ResponseRecorder {
	path := "/v1/projects/-/serviceAccounts/" + f.target + ":generateAccessToken"
	if alias {
		path = "/iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/" + f.target + ":generateAccessToken"
	}
	return f.post(path, `{"scope":["https://www.googleapis.com/auth/cloud-platform"]}`)
}

func (f *vpcscCredentialsFixture) signBlob() *httptest.ResponseRecorder {
	payload := base64.StdEncoding.EncodeToString([]byte("vpcsc-blob"))
	return f.post("/v1/projects/-/serviceAccounts/"+f.target+":signBlob",
		`{"bytesToSign":"`+payload+`"}`)
}

func (f *vpcscCredentialsFixture) signJwt() *httptest.ResponseRecorder {
	exp := time.Now().Add(time.Hour).Unix()
	payload := fmt.Sprintf(`{"iss":"lab","sub":"lab","aud":"x","exp":%d}`, exp)
	body, _ := json.Marshal(map[string]string{"payload": payload})
	return f.post("/v1/projects/-/serviceAccounts/"+f.target+":signJwt", string(body))
}

func assertVPCSCDenied(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("%s status=%d body=%s", what, rec.Code, rec.Body.String())
	}
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s unmarshal: %v body=%s", what, err, rec.Body.String())
	}
	if env.Error.Message != vpcscPerimeterDenied {
		t.Fatalf("%s message=%q want %q", what, env.Error.Message, vpcscPerimeterDenied)
	}
}

func TestVPCSCCredentialsHostAliasUnresolvedWIFDenies(t *testing.T) {
	f := setupVPCSCCredentials(t)
	email := "wif:missing:alice"
	f.grantTokenCreator(t, email)
	f.who = authn.Principal{Email: email, IsRoot: false}
	assertVPCSCDenied(t, f.generateAccessToken(true), "unresolved WIF host alias generateAccessToken")
}

func TestVPCSCCredentialsGenerateAccessTokenSameProjectAllows(t *testing.T) {
	f := setupVPCSCCredentials(t)
	pool, err := f.st.CreateWIFPool(f.project, "global", "cred-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	email := "wif:oidc-lab:alice"
	f.grantTokenCreator(t, email)
	f.who = authn.Principal{Email: email, IsRoot: false}
	rec := f.generateAccessToken(false)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-project WIF generateAccessToken status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCCredentialsGenerateAccessTokenOtherProjectDenies(t *testing.T) {
	f := setupVPCSCCredentials(t)
	pool, err := f.st.CreateWIFPool("other-proj", "global", "cred-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	email := "wif:oidc-lab:alice"
	f.grantTokenCreator(t, email)
	f.who = authn.Principal{Email: email, IsRoot: false}
	assertVPCSCDenied(t, f.generateAccessToken(false), "other-project WIF generateAccessToken")
}

func TestVPCSCCredentialsGenerateAccessTokenEnforceOffAllows(t *testing.T) {
	f := setupVPCSCCredentials(t)
	t.Setenv("NOCTAXRIS_GCP_VPCSC_ENFORCE", "")
	email := "wif:missing:alice"
	f.grantTokenCreator(t, email)
	f.who = authn.Principal{Email: email, IsRoot: false}
	rec := f.generateAccessToken(false)
	if rec.Code != http.StatusOK {
		t.Fatalf("enforce-off generateAccessToken status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCCredentialsGenerateAccessTokenRootSkipsCallerCheck(t *testing.T) {
	f := setupVPCSCCredentials(t)
	pool, err := f.st.CreateWIFPool("other-proj", "global", "cred-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	f.who = authn.Principal{Email: "wif:oidc-lab:alice", IsRoot: true}
	rec := f.generateAccessToken(false)
	if rec.Code != http.StatusOK {
		t.Fatalf("root generateAccessToken status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCCredentialsSignBlobAndSignJwtEnforced(t *testing.T) {
	f := setupVPCSCCredentials(t)

	unresolved := "wif:missing:alice"
	f.grantTokenCreator(t, unresolved)
	f.who = authn.Principal{Email: unresolved, IsRoot: false}
	assertVPCSCDenied(t, f.signBlob(), "unresolved WIF signBlob")
	assertVPCSCDenied(t, f.signJwt(), "unresolved WIF signJwt")

	pool, err := f.st.CreateWIFPool("other-proj", "global", "cred-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	cross := "wif:oidc-lab:alice"
	f.grantTokenCreator(t, cross)
	f.who = authn.Principal{Email: cross, IsRoot: false}
	assertVPCSCDenied(t, f.signBlob(), "other-project WIF signBlob")
	assertVPCSCDenied(t, f.signJwt(), "other-project WIF signJwt")

	samePool, err := f.st.CreateWIFPool(f.project, "global", "cred-same", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(samePool.Name, "oidc-same", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	same := "wif:oidc-same:alice"
	f.grantTokenCreator(t, same)
	f.who = authn.Principal{Email: same, IsRoot: false}
	if rec := f.signBlob(); rec.Code != http.StatusOK {
		t.Fatalf("same-project WIF signBlob status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := f.signJwt(); rec.Code != http.StatusOK {
		t.Fatalf("same-project WIF signJwt status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCCredentialsHostUserSameProjectDenies(t *testing.T) {
	f := setupVPCSCCredentials(t)
	email := "player@example.com"
	f.grantTokenCreator(t, email)
	f.who = authn.Principal{Email: email, IsRoot: false}
	assertVPCSCDenied(t, f.generateAccessToken(false), "host user generateAccessToken")
	assertVPCSCDenied(t, f.signBlob(), "host user signBlob")
}

func TestVPCSCCredentialsSameProjectMemberSAAllows(t *testing.T) {
	f := setupVPCSCCredentials(t)
	caller := seedServiceAccount(t, f.st, f.project, "in-perim")
	f.grantTokenCreator(t, caller)
	f.who = authn.Principal{Email: caller, IsRoot: false}
	rec := f.generateAccessToken(false)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-project member SA generateAccessToken status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCCredentialsTokenCreatorTimeCELInPerimeter(t *testing.T) {
	f := setupVPCSCCredentials(t)
	caller := seedServiceAccount(t, f.st, f.project, "timed")
	pol := authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/iam.serviceAccountTokenCreator",
			Members: []string{"serviceAccount:" + caller},
			Condition: &authz.Expr{
				Expression: `request.time < timestamp("2021-01-01T00:00:00Z")`,
			},
		}},
	}
	if err := f.st.PutIAMPolicyJSON("projects/"+f.project+"/serviceAccounts/"+f.target, pol); err != nil {
		t.Fatal(err)
	}
	eval := &authz.Evaluator{Policies: f.st, Roles: f.st}
	eval.Now = func() time.Time { return time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC) }
	mux := http.NewServeMux()
	h := &iam.Handler{
		Store: f.st,
		Authz: eval,
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: caller, IsRoot: false}, true
		},
	}
	h.Mount(mux)
	body := `{"scope":["https://www.googleapis.com/auth/cloud-platform"]}`
	path := "/v1/projects/-/serviceAccounts/" + f.target + ":generateAccessToken"
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("in-window member Token Creator status=%d body=%s", rec.Code, rec.Body.String())
	}
	eval.Now = func() time.Time { return time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC) }
	req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expired request.time must deny status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), vpcscPerimeterDenied) {
		t.Fatalf("expired CEL should be IAM deny, not VPC-SC: %s", rec.Body.String())
	}
}
