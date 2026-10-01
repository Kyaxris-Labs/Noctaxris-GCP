package firebaseauth_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

func TestSignInWithCustomTokenRejectsUnsigned(t *testing.T) {
	authn.SetIdentityToolkitHMACKeyForTest([]byte("test-identity-toolkit-hmac-key!!"))
	mux := testFirebaseMux(t)
	root := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"uid":"` + root + `","aud":"https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit"}`))
	forged := header + "." + payload + "."
	req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken",
		bytes.NewReader([]byte(`{"token":"`+forged+`","returnSecureToken":true}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsigned custom token status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAccountsLookupRejectsUnsignedIDToken(t *testing.T) {
	authn.SetIdentityToolkitHMACKeyForTest([]byte("test-identity-toolkit-hmac-key!!"))
	mux := testFirebaseMux(t)
	localID, _ := signUpUser(t, mux, "lookup-forge@example.com")
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"user_id":"` + localID + `","sub":"` + localID + `","iss":"forged-lab"}`))
	forged := header + "." + payload + "."
	req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:lookup",
		bytes.NewReader([]byte(`{"idToken":"`+forged+`"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsigned lookup status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCustomClaimsCannotForgeOwnerBearer(t *testing.T) {
	authn.SetIdentityToolkitHMACKeyForTest([]byte("test-identity-toolkit-hmac-key!!"))
	mux := testFirebaseMux(t)
	project := "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	localID, _ := signUpUser(t, mux, "claims-victim@example.com")
	adminBase := "/identitytoolkit.googleapis.com/v1/projects/" + project + "/accounts"
	req := httptest.NewRequest(http.MethodPost, adminBase+":setCustomUserClaims",
		bytes.NewReader([]byte(`{"localId":"`+localID+`","customAttributes":{"user_id":"`+root+`","sub":"`+root+`","tier":"gold"}}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setCustomUserClaims status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signInWithPassword",
		bytes.NewReader([]byte(`{"email":"claims-victim@example.com","password":"secret123","returnSecureToken":true}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signIn status=%d body=%s", rec.Code, rec.Body.String())
	}
	var signed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &signed)
	idToken, _ := signed["idToken"].(string)
	uid, ok := authn.LabIdentityToolkitUID(idToken)
	if !ok || uid != localID {
		t.Fatalf("uid=%q ok=%v want %s", uid, ok, localID)
	}
	a := &authn.Authenticator{RootAccessToken: "other", RootServiceAccount: root}
	p, err := a.AuthenticateToken(idToken)
	if err != nil {
		t.Fatal(err)
	}
	if p.Email == root || p.Email == "user:"+root {
		t.Fatalf("principal must stay victim localId, got %q", p.Email)
	}
}
