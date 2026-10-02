package cdn_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cdn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCDNEdgeRequiresStorageIAMAndProjectBind(t *testing.T) {
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
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateBucket("edge-priv", project, "US", "STANDARD"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("edge-priv", "a.js", "application/javascript", []byte("x")); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &cdn.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		if strings.HasPrefix(r.URL.Path, "/cdn/") {
			return authn.Principal{}, false
		}
		return authn.Principal{Email: root, IsRoot: true}, true
	})

	body := `{"origin":{"gcs":{"bucket":"edge-priv"}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/global/distributions?distributionId=e1", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/cdn/"+project+"/e1/a.js", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no allUsers want 403, got %d", rec.Code)
	}

	if err := st.PutIAMPolicyJSON(store.BucketIAMResource("edge-priv"), authz.Policy{
		Bindings: []authz.Binding{{Role: "roles/storage.objectViewer", Members: []string{"allUsers"}}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/cdn/"+project+"/e1/a.js", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "x" {
		t.Fatalf("allUsers edge: %d %q", rec.Code, rec.Body.String())
	}

	// Wrong project must not resolve the distribution.
	req = httptest.NewRequest(http.MethodGet, "/cdn/other-project/e1/a.js", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-project edge want 404, got %d", rec.Code)
	}
}
