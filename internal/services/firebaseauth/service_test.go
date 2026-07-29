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

func testFirebaseMux(t *testing.T) *http.ServeMux {
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
	if err := st.EnsureRoot(project, "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &firebaseauth.Service{
		Store: st, Authz: &authz.Evaluator{Policies: st}, DefaultProject: project,
	}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})
	return mux
}

func TestFirebaseAuthSignUpSignInAdminCRUDVerify(t *testing.T) {
	mux := testFirebaseMux(t)
	project := "noctaxris-gcp-local"
	adminBase := "/identitytoolkit.googleapis.com/v1/projects/" + project + "/accounts"

	signUp := `{"email":"lab@example.com","password":"secret123","returnSecureToken":true}`
	req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signUp", bytes.NewReader([]byte(signUp)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signUp status=%d body=%s", rec.Code, rec.Body.String())
	}
	var user map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &user)
	localID, _ := user["localId"].(string)
	idToken, _ := user["idToken"].(string)
	if localID == "" || idToken == "" {
		t.Fatalf("signUp=%#v", user)
	}

	signIn := `{"email":"lab@example.com","password":"secret123","returnSecureToken":true}`
	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signInWithPassword", bytes.NewReader([]byte(signIn)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signIn status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, adminBase+"/"+localID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin get status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, adminBase+"/"+localID,
		bytes.NewReader([]byte(`{"displayName":"Lab User"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin patch status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &user)
	if user["displayName"] != "Lab User" {
		t.Fatalf("patched=%#v", user)
	}

	req = httptest.NewRequest(http.MethodGet, adminBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listBody)
	users, _ := listBody["users"].([]any)
	if len(users) < 1 {
		t.Fatalf("list=%#v", listBody)
	}

	verify := `{"idToken":"` + idToken + `"}`
	req = httptest.NewRequest(http.MethodPost, adminBase+":verifyIdToken", bytes.NewReader([]byte(verify)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verifyIdToken status=%d body=%s", rec.Code, rec.Body.String())
	}
	var verified map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &verified)
	if verified["valid"] != true || verified["uid"] != localID {
		t.Fatalf("verify=%#v", verified)
	}

	req = httptest.NewRequest(http.MethodDelete, adminBase+"/"+localID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, adminBase+"/"+localID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func signUpUser(t *testing.T, mux *http.ServeMux, email string) (localID, idToken string) {
	t.Helper()
	body := `{"email":"` + email + `","password":"secret123","returnSecureToken":true}`
	req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signUp", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signUp status=%d body=%s", rec.Code, rec.Body.String())
	}
	var user map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &user)
	localID, _ = user["localId"].(string)
	idToken, _ = user["idToken"].(string)
	if localID == "" || idToken == "" {
		t.Fatalf("signUp=%#v", user)
	}
	return localID, idToken
}

func TestFirebaseClientDeleteUpdateRequireIDToken(t *testing.T) {
	mux := testFirebaseMux(t)
	localID, idToken := signUpUser(t, mux, "client-auth@example.com")
	otherID, _ := signUpUser(t, mux, "other@example.com")

	t.Run("delete missing idToken", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:delete",
			bytes.NewReader([]byte(`{"localId":"`+localID+`"}`)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("MISSING_ID_TOKEN")) {
			t.Fatalf("body=%s", rec.Body.String())
		}
	})

	t.Run("update missing idToken", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:update",
			bytes.NewReader([]byte(`{"localId":"`+localID+`","displayName":"Nope"}`)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("MISSING_ID_TOKEN")) {
			t.Fatalf("body=%s", rec.Body.String())
		}
	})

	t.Run("delete invalid idToken", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:delete",
			bytes.NewReader([]byte(`{"localId":"`+localID+`","idToken":"not-a-jwt"}`)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("INVALID_ID_TOKEN")) {
			t.Fatalf("body=%s", rec.Body.String())
		}
	})

	t.Run("update idToken localId mismatch", func(t *testing.T) {
		payload := `{"localId":"` + otherID + `","idToken":"` + idToken + `","displayName":"Hijack"}`
		req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:update",
			bytes.NewReader([]byte(payload)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("INVALID_ID_TOKEN")) {
			t.Fatalf("body=%s", rec.Body.String())
		}
	})

	t.Run("update with matching idToken", func(t *testing.T) {
		payload := `{"localId":"` + localID + `","idToken":"` + idToken + `","displayName":"Client Updated"}`
		req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:update",
			bytes.NewReader([]byte(payload)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var user map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &user)
		if user["displayName"] != "Client Updated" {
			t.Fatalf("user=%#v", user)
		}
	})

	t.Run("delete with matching idToken", func(t *testing.T) {
		payload := `{"localId":"` + localID + `","idToken":"` + idToken + `"}`
		req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:delete",
			bytes.NewReader([]byte(payload)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("admin delete still Bearer without idToken", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete,
			"/identitytoolkit.googleapis.com/v1/projects/noctaxris-gcp-local/accounts/"+otherID, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("admin delete status=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestFirebaseAuthAdminAuthzFailClosed(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &firebaseauth.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodGet, "/identitytoolkit.googleapis.com/v1/projects/noctaxris-gcp-local/accounts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
