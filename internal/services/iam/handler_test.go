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
		Authz: &authz.Evaluator{Policies: st, Roles: st},
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
	prov, err := h.store.CreateWIFProvider(pool.Name, "oidc", "OIDC", "", "https://example.com", "", "[]", false)
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

func TestCustomRolesCRUD(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	h.setWho("root@"+project+".iam.gserviceaccount.com", true)

	createBody := []byte(`{
		"roleId":"bucketLister",
		"role":{
			"title":"Bucket Lister",
			"description":"list only",
			"includedPermissions":["storage.buckets.list"],
			"stage":"GA"
		}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/roles", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["name"] != "projects/"+project+"/roles/bucketLister" {
		t.Fatalf("created=%#v", created)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/roles/bucketLister", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, getReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/roles", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, listReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	roles, _ := list["roles"].([]any)
	if len(roles) != 1 {
		t.Fatalf("list roles=%#v", list)
	}

	patchBody := []byte(`{"includedPermissions":["storage.buckets.list","storage.objects.get"],"updateMask":"includedPermissions"}`)
	patchReq := httptest.NewRequest(http.MethodPatch, "/v1/projects/"+project+"/roles/bucketLister", bytes.NewReader(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, patchReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Bind custom role and evaluate least-privilege via Authz wired to store Roles.
	email := seedServiceAccount(t, h.store, project, "custom-user")
	if err := h.store.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Etag: "c1",
		Bindings: []authz.Binding{{
			Role:    "projects/" + project + "/roles/bucketLister",
			Members: []string{"serviceAccount:" + email},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	eval := &authz.Evaluator{Policies: h.store, Roles: h.store}
	ok, err := eval.Evaluate(email, false, "storage.buckets.list", "projects/"+project)
	if err != nil || !ok {
		t.Fatalf("custom role allow: ok=%v err=%v", ok, err)
	}
	ok, err = eval.Evaluate(email, false, "storage.buckets.create", "projects/"+project)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("custom role must not over-grant create")
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/v1/projects/"+project+"/roles/bucketLister", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, delReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	ok, err = eval.Evaluate(email, false, "storage.buckets.list", "projects/"+project)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("deleted custom role must stop granting")
	}
}

func TestCustomRoleUndelete(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	h.setWho("root@"+project+".iam.gserviceaccount.com", true)

	createBody := []byte(`{
		"roleId":"roleUndelete",
		"role":{"title":"T","includedPermissions":["storage.buckets.list"]}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/roles", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/v1/projects/"+project+"/roles/roleUndelete", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, delReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/roles/roleUndelete", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, getReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("get deleted status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["deleted"] != true {
		t.Fatalf("get on soft-deleted role must return deleted:true, got %#v", got)
	}

	dupReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/roles", bytes.NewReader(createBody))
	dupReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, dupReq)
	if rec.Code != http.StatusConflict {
		t.Fatalf("recreate while deleted expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}

	undReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/roles/roleUndelete:undelete", bytes.NewReader([]byte("{}")))
	undReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, undReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("undelete status=%d body=%s", rec.Code, rec.Body.String())
	}
	var restored map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &restored); err != nil {
		t.Fatal(err)
	}
	if restored["deleted"] != false {
		t.Fatalf("undelete response: %#v", restored)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/roles", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, listReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	roles, _ := list["roles"].([]any)
	if len(roles) != 1 {
		t.Fatalf("list after undelete: %#v", list)
	}
}

func TestCustomRoleUndeleteAuthzDenied(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	h.setWho("root@"+project+".iam.gserviceaccount.com", true)
	createBody := []byte(`{"roleId":"noUndelete","role":{"title":"T","includedPermissions":["storage.buckets.list"]}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/roles", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	delReq := httptest.NewRequest(http.MethodDelete, "/v1/projects/"+project+"/roles/noUndelete", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, delReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d", rec.Code)
	}

	viewer := seedServiceAccount(t, h.store, project, "role-undelete-viewer")
	if err := h.store.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Etag: "v1",
		Bindings: []authz.Binding{{
			Role:    "roles/viewer",
			Members: []string{"serviceAccount:" + viewer},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	h.setWho(viewer, false)
	undReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/roles/noUndelete:undelete", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, undReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer undelete expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCustomRoleUndeleteAuthzAllowed(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	h.setWho("root@"+project+".iam.gserviceaccount.com", true)
	createBody := []byte(`{"roleId":"ownerUndelete","role":{"title":"T","includedPermissions":["storage.buckets.list"]}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/roles", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	delReq := httptest.NewRequest(http.MethodDelete, "/v1/projects/"+project+"/roles/ownerUndelete", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, delReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d", rec.Code)
	}

	owner := seedServiceAccount(t, h.store, project, "role-undelete-owner")
	if err := h.store.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Etag: "o1",
		Bindings: []authz.Binding{{
			Role:    "roles/owner",
			Members: []string{"serviceAccount:" + owner},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	h.setWho(owner, false)
	undReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/roles/ownerUndelete:undelete", bytes.NewReader([]byte("{}")))
	undReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, undReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner undelete expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var restored map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &restored); err != nil {
		t.Fatal(err)
	}
	if restored["deleted"] != false {
		t.Fatalf("undelete response: %#v", restored)
	}

	// Undelete on an active role is FailedPrecondition, not NotFound.
	again := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/roles/ownerUndelete:undelete", bytes.NewReader([]byte("{}")))
	again.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, again)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("undelete active expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "FAILED_PRECONDITION") && !strings.Contains(rec.Body.String(), "not deleted") {
		t.Fatalf("expected FAILED_PRECONDITION body=%s", rec.Body.String())
	}
}

func TestCustomRolesAuthzDenied(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	viewer := seedServiceAccount(t, h.store, project, "role-viewer")
	if err := h.store.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Etag: "v1",
		Bindings: []authz.Binding{{
			Role:    "roles/viewer",
			Members: []string{"serviceAccount:" + viewer},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	h.setWho(viewer, false)
	body := []byte(`{"roleId":"x","role":{"title":"x","includedPermissions":["storage.buckets.list"]}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/roles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create role expected 403, got %d", rec.Code)
	}
}

func TestCreateKeyDeniedByOrgPolicy(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	email := seedServiceAccount(t, h.store, project, "key-blocked")
	if _, err := h.store.SetOrgPolicy("projects/"+project, store.ConstraintDisableServiceAccountKeyCreation, `{"rules":[{"enforce":true}]}`); err != nil {
		t.Fatal(err)
	}
	h.setWho("root@"+project+".iam.gserviceaccount.com", true)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/serviceAccounts/"+url.PathEscape(email)+"/keys", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 FAILED_PRECONDITION, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "iam.disableServiceAccountKeyCreation") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
