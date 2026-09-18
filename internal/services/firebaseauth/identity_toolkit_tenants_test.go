package firebaseauth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIdentityToolkitTenantsSignupGate(t *testing.T) {
	mux := testFirebaseMux(t)
	project := "noctaxris-gcp-local"

	openBody := `{"displayName":"open","allowPasswordSignup":true}`
	req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v2/projects/"+project+"/tenants?tenantId=open-t", bytes.NewReader([]byte(openBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create open tenant status=%d body=%s", rec.Code, rec.Body.String())
	}

	lockedBody := `{"displayName":"locked","allowPasswordSignup":false}`
	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v2/projects/"+project+"/tenants?tenantId=locked-t", bytes.NewReader([]byte(lockedBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create locked tenant status=%d body=%s", rec.Code, rec.Body.String())
	}

	signOpen := `{"email":"a@example.com","password":"hunter2-lab","tenantId":"open-t"}`
	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signUp", bytes.NewReader([]byte(signOpen)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("open signup status=%d body=%s", rec.Code, rec.Body.String())
	}

	signLocked := `{"email":"b@example.com","password":"hunter2-lab","tenantId":"locked-t"}`
	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signUp", bytes.NewReader([]byte(signLocked)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("locked signup status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("admin-restricted-operation")) {
		t.Fatalf("locked body=%s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/identitytoolkit.googleapis.com/v2/projects/"+project+"/tenants", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tenants status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	tenants, _ := listed["tenants"].([]any)
	if len(tenants) < 2 {
		t.Fatalf("tenants=%#v", listed)
	}
}
