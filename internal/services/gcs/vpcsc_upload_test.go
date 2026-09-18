package gcs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	gcshandler "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/gcs"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type vpcscGCSFixture struct {
	mux     *http.ServeMux
	st      *store.Store
	who     authn.Principal
	project string
	bucket  string
}

func setupVPCSCGCS(t *testing.T) *vpcscGCSFixture {
	t.Helper()
	t.Setenv("NOCTAXRIS_GCP_VPCSC_ENFORCE", "1")
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
	project := "noctaxris-gcp-local"
	rootSA := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootSA); err != nil {
		t.Fatal(err)
	}
	if err := st.MigrateAccessContextManager(); err != nil {
		t.Fatal(err)
	}
	status, _ := json.Marshal(map[string]any{
		"resources":          []string{"projects/" + project},
		"restrictedServices": []string{"storage.googleapis.com"},
	})
	polName := store.AccessPolicyResourceName("gcs-pol")
	if ok, err := st.CreateAccessPolicy(store.AccessPolicy{
		Name: polName, PolicyID: "gcs-pol", Parent: "organizations/noctaxris-gcp-org", Title: "GCS",
	}); err != nil || !ok {
		t.Fatalf("policy ok=%v err=%v", ok, err)
	}
	if ok, err := st.CreateServicePerimeter(store.ServicePerimeter{
		Name: store.ServicePerimeterResourceName("gcs-pol", "gcs-edge"), PolicyName: polName,
		PerimeterID: "gcs-edge", Title: "gcs", StatusJSON: string(status),
	}); err != nil || !ok {
		t.Fatalf("perimeter ok=%v err=%v", ok, err)
	}
	bucket := "vpcsc-up"
	if _, ok, err := st.CreateBucket(bucket, project, "US", "STANDARD"); err != nil || !ok {
		t.Fatalf("bucket ok=%v err=%v", ok, err)
	}

	f := &vpcscGCSFixture{
		st:      st,
		who:     authn.Principal{Email: rootSA, IsRoot: true},
		project: project,
		bucket:  bucket,
	}
	mux := http.NewServeMux()
	h := &gcshandler.Handler{
		Store:          st,
		Authz:          &authz.Evaluator{Policies: st},
		DefaultProject: project,
		Principal:      func(*http.Request) (authn.Principal, bool) { return f.who, true },
	}
	h.Register(mux)
	f.mux = mux
	return f
}

func (f *vpcscGCSFixture) grantEditor(t *testing.T, email string) {
	t.Helper()
	member := email
	if !strings.Contains(email, ":") {
		member = "serviceAccount:" + email
	}
	if err := f.st.PutIAMPolicyJSON("projects/"+f.project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/editor",
			Members: []string{member},
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *vpcscGCSFixture) upload(name, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost,
		"/upload/storage/v1/b/"+f.bucket+"/o?uploadType=media&name="+name,
		strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	req.Host = "127.0.0.1:4588"
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func TestVPCSCGCSUploadWIFUnresolvedDenies(t *testing.T) {
	f := setupVPCSCGCS(t)
	email := "wif:missing:alice"
	f.grantEditor(t, email)
	f.who = authn.Principal{Email: email, IsRoot: false}
	rec := f.upload("unresolved.txt", "x")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unresolved WIF upload status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCGCSUploadWIFOtherProjectDenies(t *testing.T) {
	f := setupVPCSCGCS(t)
	pool, err := f.st.CreateWIFPool("other-proj", "global", "gcs-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	email := "wif:oidc-lab:alice"
	f.grantEditor(t, email)
	f.who = authn.Principal{Email: email, IsRoot: false}
	rec := f.upload("cross.txt", "x")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-project WIF upload status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCGCSUploadWIFSameProjectAllows(t *testing.T) {
	f := setupVPCSCGCS(t)
	pool, err := f.st.CreateWIFPool(f.project, "global", "gcs-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	email := "wif:oidc-lab:alice"
	f.grantEditor(t, email)
	f.who = authn.Principal{Email: email, IsRoot: false}
	rec := f.upload("same.txt", "ok")
	if rec.Code != http.StatusOK {
		t.Fatalf("same-project WIF upload status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCGCSUploadRootSkipsCallerCheck(t *testing.T) {
	f := setupVPCSCGCS(t)
	pool, err := f.st.CreateWIFPool("other-proj", "global", "gcs-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	f.who = authn.Principal{Email: "wif:oidc-lab:alice", IsRoot: true}
	rec := f.upload("root.txt", "ok")
	if rec.Code != http.StatusOK {
		t.Fatalf("root upload status=%d body=%s", rec.Code, rec.Body.String())
	}
}
