package firebaseauth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFirebaseOOBCustomTokenLookup(t *testing.T) {
	mux := testFirebaseMux(t)
	project := "noctaxris-gcp-local"
	adminBase := "/identitytoolkit.googleapis.com/v1/projects/" + project + "/accounts"
	clientBase := "/identitytoolkit.googleapis.com/v1/accounts"

	localID, idToken := signUpUser(t, mux, "oob@example.com")

	req := httptest.NewRequest(http.MethodPost, clientBase+":lookup",
		bytes.NewReader([]byte(`{"idToken":"`+idToken+`"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, clientBase+":sendOobCode",
		bytes.NewReader([]byte(`{"requestType":"PASSWORD_RESET","email":"oob@example.com"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sendOobCode: %d %s", rec.Code, rec.Body.String())
	}
	var oobResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &oobResp)
	oobCode, _ := oobResp["oobCode"].(string)
	if oobCode == "" {
		// Some builds return email only; try lab field names.
		oobCode, _ = oobResp["oobbCode"].(string)
	}
	if oobCode == "" {
		t.Fatalf("oob response=%#v", oobResp)
	}
	req = httptest.NewRequest(http.MethodPost, clientBase+":resetPassword",
		bytes.NewReader([]byte(`{"oobCode":"`+oobCode+`","newPassword":"newsecret123"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resetPassword: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, clientBase+":signInWithPassword",
		bytes.NewReader([]byte(`{"email":"oob@example.com","password":"newsecret123","returnSecureToken":true}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signIn after reset: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, adminBase+":createCustomToken",
		bytes.NewReader([]byte(`{"uid":"`+localID+`","claims":{"role":"lab"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("createCustomToken: %d %s", rec.Code, rec.Body.String())
	}
	var tok map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tok)
	custom, _ := tok["token"].(string)
	if custom == "" {
		custom, _ = tok["customToken"].(string)
	}
	if custom == "" {
		t.Fatalf("token=%#v", tok)
	}
	req = httptest.NewRequest(http.MethodPost, clientBase+":signInWithCustomToken",
		bytes.NewReader([]byte(`{"token":"`+custom+`","returnSecureToken":true}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signInWithCustomToken: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, adminBase+":setCustomUserClaims",
		bytes.NewReader([]byte(`{"localId":"`+localID+`","customAttributes":{"tier":"gold"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setCustomUserClaims: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, adminBase,
		bytes.NewReader([]byte(`{"email":"admin-created@example.com","password":"secret123","displayName":"Admin"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, clientBase+":update",
		bytes.NewReader([]byte(`{"idToken":"`+idToken+`","displayName":"Updated"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		// token may be invalidated after password reset; sign in again
		req = httptest.NewRequest(http.MethodPost, clientBase+":signInWithPassword",
			bytes.NewReader([]byte(`{"email":"oob@example.com","password":"newsecret123","returnSecureToken":true}`)))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var signed map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &signed)
		idToken, _ = signed["idToken"].(string)
		req = httptest.NewRequest(http.MethodPost, clientBase+":update",
			bytes.NewReader([]byte(`{"idToken":"`+idToken+`","displayName":"Updated"}`)))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodPost, clientBase+":delete",
		bytes.NewReader([]byte(`{"idToken":"`+idToken+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
}
