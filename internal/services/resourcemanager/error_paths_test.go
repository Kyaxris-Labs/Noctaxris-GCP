package resourcemanager_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/resourcemanager"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCRMErrorPathsAndAuthz(t *testing.T) {
	mux, _ := openCRM(t)

	req := httptest.NewRequest(http.MethodPost, "/v3/folders", bytes.NewReader([]byte(`not-json`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad json")
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders", bytes.NewReader([]byte(`{"displayName":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("missing parent")
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders", bytes.NewReader([]byte(`{"parent":"organizations/x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("missing displayName")
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders",
		bytes.NewReader([]byte(`{"parent":"projects/p","displayName":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad parent type")
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders",
		bytes.NewReader([]byte(`{"parent":"organizations/no-such-org","displayName":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing org parent: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders",
		bytes.NewReader([]byte(`{"parent":"folders/no-such","displayName":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing folder parent: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/noctaxris-gcp-local:unknownMethod", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("unknown v1 method")
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/noctaxris-gcp-local", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("missing colon action")
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders/x:bogus", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("unknown folder method")
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/organizations/x:bogus", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("unknown org method")
	}

	req = httptest.NewRequest(http.MethodPatch, "/v3/folders/missing?updateMask=labels",
		bytes.NewReader([]byte(`{"displayName":"n"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad updateMask")
	}
	req = httptest.NewRequest(http.MethodGet, "/v3/folders?parent="+store.DefaultOrganizationName+"&showDeleted=true", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list showDeleted: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v3/tagKeys", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("tagKeys without parent")
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/tagKeys", bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("tagKey bad json")
	}

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
	deny := http.NewServeMux()
	h := &resourcemanager.Handler{
		Store: st, Authz: &authz.Evaluator{Policies: st},
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
		},
	}
	h.Mount(deny)
	req = httptest.NewRequest(http.MethodGet, "/v3/projects/noctaxris-gcp-local", nil)
	rec = httptest.NewRecorder()
	deny.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("authz deny: %d %s", rec.Code, rec.Body.String())
	}
}
