package cloudrun_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountCloudRun(t *testing.T, principal func(*http.Request) (authn.Principal, bool)) *http.ServeMux {
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
	svc := &cloudrun.Service{
		Store:   st,
		Authz:   &authz.Evaluator{Policies: st},
		Invoker: compute.MockInvoker{},
	}
	svc.Mount(mux, principal)
	return mux
}

func TestCloudRunServiceCRUDAndInvoke(t *testing.T) {
	mux := mountCloudRun(t, nil)
	loc := cloudrun.DefaultLocation
	project := "noctaxris-gcp-local"
	base := "/v2/projects/" + project + "/locations/" + loc + "/services"

	body := `{"template":{"containers":[{"image":"demo"}],"labResponseBody":"{\"ok\":true}"}}`
	req := httptest.NewRequest(http.MethodPost, base+"?serviceId=demo", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createOp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &createOp); err != nil {
		t.Fatal(err)
	}
	if createOp["done"] != true {
		t.Fatalf("expected done operation: %#v", createOp)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/demo", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/demo:invoke", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"ok"`)) {
		t.Fatalf("invoke body=%s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/demo", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudRunAuthzDenyNonRoot(t *testing.T) {
	mux := mountCloudRun(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	path := "/v2/projects/noctaxris-gcp-local/locations/" + cloudrun.DefaultLocation + "/services"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudRunUnauthenticated(t *testing.T) {
	mux := mountCloudRun(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	})
	path := "/v2/projects/noctaxris-gcp-local/locations/" + cloudrun.DefaultLocation + "/services"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
