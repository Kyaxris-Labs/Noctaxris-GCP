package firebaseauth_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFirebaseErrorBranches(t *testing.T) {
	mux := testFirebaseMux(t)
	clientBase := "/identitytoolkit.googleapis.com/v1/accounts"

	req := httptest.NewRequest(http.MethodPost, clientBase+":signUp", bytes.NewReader([]byte(`not-json`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad signup json")
	}
	req = httptest.NewRequest(http.MethodPost, clientBase+":sendOobCode",
		bytes.NewReader([]byte(`{"requestType":"VERIFY_EMAIL","email":"x@y.z"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("unsupported oob type")
	}
	req = httptest.NewRequest(http.MethodPost, clientBase+":sendOobCode",
		bytes.NewReader([]byte(`{"requestType":"PASSWORD_RESET"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("oob missing email")
	}
	req = httptest.NewRequest(http.MethodPost, clientBase+":sendOobCode",
		bytes.NewReader([]byte(`{"requestType":"PASSWORD_RESET","email":"missing@example.com"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("oob missing user")
	}
	req = httptest.NewRequest(http.MethodPost, clientBase+":resetPassword",
		bytes.NewReader([]byte(`{"oobCode":"","newPassword":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("reset missing fields")
	}
	req = httptest.NewRequest(http.MethodPost, clientBase+":resetPassword",
		bytes.NewReader([]byte(`{"oobCode":"nope","newPassword":"newsecret123"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("invalid oob")
	}
	req = httptest.NewRequest(http.MethodPost, clientBase+":signInWithPassword",
		bytes.NewReader([]byte(`{"email":"nope@example.com","password":"x","returnSecureToken":true}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad signin")
	}
}
