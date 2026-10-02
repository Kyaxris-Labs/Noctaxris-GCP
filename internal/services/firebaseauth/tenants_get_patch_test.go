package firebaseauth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIdentityToolkitGetAndPatchTenant(t *testing.T) {
	mux := testFirebaseMux(t)
	project := "noctaxris-gcp-local"
	base := "/identitytoolkit.googleapis.com/v2/projects/" + project + "/tenants"

	req := httptest.NewRequest(http.MethodPost, base+"?tenantId=lab-tenant",
		bytes.NewReader([]byte(`{"displayName":"Lab","allowPasswordSignup":true}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lab-tenant", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["displayName"] != "Lab" {
		t.Fatalf("get=%#v", got)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/missing-tenant", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get status=%d body=%s", rec.Code, rec.Body.String())
	}

	patch := `{"displayName":"Lab Patched","allowPasswordSignup":false}`
	req = httptest.NewRequest(http.MethodPatch, base+"/lab-tenant", bytes.NewReader([]byte(patch)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["displayName"] != "Lab Patched" || got["allowPasswordSignup"] != false {
		t.Fatalf("patched=%#v", got)
	}

	req = httptest.NewRequest(http.MethodPatch, base+"/missing-tenant",
		bytes.NewReader([]byte(`{"displayName":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing patch status=%d body=%s", rec.Code, rec.Body.String())
	}

	signLocked := `{"email":"tenant-lock@example.com","password":"hunter2-lab","tenantId":"lab-tenant"}`
	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signUp",
		bytes.NewReader([]byte(signLocked)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("signup after lock status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFirebaseVerifyClaimsCustomTokenAdminBranches(t *testing.T) {
	mux := testFirebaseMux(t)
	project := "noctaxris-gcp-local"
	adminBase := "/identitytoolkit.googleapis.com/v1/projects/" + project + "/accounts"
	localID, idToken := signUpUser(t, mux, "claims-lab@example.com")

	req := httptest.NewRequest(http.MethodPost, adminBase+":verifyIdToken",
		bytes.NewReader([]byte(`not-json`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("verify bad json")
	}
	req = httptest.NewRequest(http.MethodPost, adminBase+":verifyIdToken",
		bytes.NewReader([]byte(`{"idToken":""}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("verify empty token")
	}
	req = httptest.NewRequest(http.MethodPost, adminBase+":verifyIdToken",
		bytes.NewReader([]byte(`{"idToken":"not.a.jwt"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("verify invalid jwt")
	}

	req = httptest.NewRequest(http.MethodPost, adminBase+":setCustomUserClaims",
		bytes.NewReader([]byte(`{"localId":"`+localID+`","claims":{"role":"ops"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setClaims status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, adminBase+":setCustomUserClaims",
		bytes.NewReader([]byte(`{"localId":"","claims":{}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("setClaims missing localId")
	}
	req = httptest.NewRequest(http.MethodPost, adminBase+":setCustomUserClaims",
		bytes.NewReader([]byte(`{"localId":"missing-user","claims":{}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("setClaims missing user status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, adminBase+":createCustomToken",
		bytes.NewReader([]byte(`{"uid":"`+localID+`","claims":{"tier":1}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("createCustomToken status=%d body=%s", rec.Code, rec.Body.String())
	}
	var tok map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tok)
	custom, _ := tok["token"].(string)
	if custom == "" {
		t.Fatalf("token=%#v", tok)
	}
	req = httptest.NewRequest(http.MethodPost, adminBase+":createCustomToken",
		bytes.NewReader([]byte(`{"uid":""}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("createCustomToken empty uid")
	}

	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken",
		bytes.NewReader([]byte(`{"token":"`+custom+`","returnSecureToken":true}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signInCustomToken status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, adminBase,
		bytes.NewReader([]byte(`{"email":"admin-create@example.com","password":"secret123"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("adminCreate status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	createdID, _ := created["localId"].(string)
	if createdID == "" {
		t.Fatalf("adminCreate=%#v", created)
	}

	req = httptest.NewRequest(http.MethodDelete, adminBase+"/"+createdID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("adminDelete status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, adminBase+"/no-such-user", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("adminDelete missing status=%d", rec.Code)
	}

	_ = idToken
}
