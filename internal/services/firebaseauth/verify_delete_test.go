package firebaseauth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFirebaseVerifyIdTokenAndAdminDelete(t *testing.T) {
	mux := testFirebaseMux(t)
	project := "noctaxris-gcp-local"
	adminBase := "/identitytoolkit.googleapis.com/v1/projects/" + project + "/accounts"
	clientBase := "/identitytoolkit.googleapis.com/v1/accounts"

	localID, idToken := signUpUser(t, mux, "verify-del@example.com")

	req := httptest.NewRequest(http.MethodPost, adminBase+":verifyIdToken",
		bytes.NewReader([]byte(`{"idToken":"`+idToken+`"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verifyIdToken: %d %s", rec.Code, rec.Body.String())
	}
	var verified map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &verified)
	if verified["localId"] == nil && verified["user_id"] == nil && verified["uid"] == nil {
		t.Fatalf("verify response=%#v", verified)
	}

	req = httptest.NewRequest(http.MethodDelete, adminBase+"/"+localID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("adminDelete: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, adminBase+"/"+localID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
		// get may 404 after delete
		t.Fatalf("get after delete: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusOK {
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["localId"] == localID {
			t.Fatal("deleted user still present")
		}
	}

	req = httptest.NewRequest(http.MethodPost, clientBase+":lookup",
		bytes.NewReader([]byte(`{"localId":["`+localID+`"]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	// lookup of deleted may be empty or error; just exercise the branch
	_ = rec.Code
}
