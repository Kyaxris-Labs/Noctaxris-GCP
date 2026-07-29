package cloudfunctions_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudfunctions"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountFunctions(t *testing.T, principal func(*http.Request) (authn.Principal, bool)) *http.ServeMux {
	t.Helper()
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
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if principal == nil {
		principal = func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: root, IsRoot: true}, true
		}
	}
	mux := http.NewServeMux()
	svc := &cloudfunctions.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, principal)
	return mux
}

func TestCloudFunctionsCRUDAndInvokeStub(t *testing.T) {
	mux := mountFunctions(t, nil)
	loc := cloudfunctions.DefaultLocation
	project := "noctaxris-gcp-local"
	base := "/v2/projects/" + project + "/locations/" + loc + "/functions"

	body := `{"labResponse":{"answer":42}}`
	req := httptest.NewRequest(http.MethodPost, base+"?functionId=fn1", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var fn map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fn); err != nil {
		t.Fatal(err)
	}
	if fn["state"] != "ACTIVE" {
		t.Fatalf("state=%v", fn["state"])
	}

	req = httptest.NewRequest(http.MethodPost, base+"/fn1:invoke", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`42`)) {
		t.Fatalf("invoke stub body=%s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/fn1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudFunctionsAuthzDenyNonRoot(t *testing.T) {
	mux := mountFunctions(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	base := "/v2/projects/noctaxris-gcp-local/locations/" + cloudfunctions.DefaultLocation + "/functions"
	req := httptest.NewRequest(http.MethodGet, base, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
