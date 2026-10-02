package gcs_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/gcs"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestGCSAllUsersAnonymousAndComposeSourceGet(t *testing.T) {
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
	if _, _, err := st.CreateBucket("pub", project, "US", "STANDARD"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("pub", "a.txt", "text/plain", []byte("A")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("pub", "b.txt", "text/plain", []byte("B")); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := &gcs.Handler{Store: st, Authz: &authz.Evaluator{Policies: st}, Principal: func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	}}
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/storage/v1/b/pub/o/a.txt?alt=media", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous without allUsers want 401/403, got %d", rec.Code)
	}

	if err := st.PutIAMPolicyJSON(store.BucketIAMResource("pub"), authz.Policy{
		Bindings: []authz.Binding{{
			Role: "roles/storage.objectViewer", Members: []string{"allUsers"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/storage/v1/b/pub/o/a.txt?alt=media", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "A" {
		t.Fatalf("allUsers get: %d %q", rec.Code, rec.Body.String())
	}

	composeBody := `{"sourceObjects":[{"name":"a.txt"},{"name":"b.txt"}],"destination":{"contentType":"text/plain"}}`
	viewer := "viewer@example.com"
	if err := st.PutIAMPolicyJSON(store.BucketIAMResource("pub"), authz.Policy{
		Bindings: []authz.Binding{{
			Role: "roles/storage.objectViewer", Members: []string{"serviceAccount:" + viewer},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	h.Principal = func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: viewer, IsRoot: false}, true
	}
	req = httptest.NewRequest(http.MethodPost, "/storage/v1/b/pub/o/c-viewer.txt/compose", bytes.NewReader([]byte(composeBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("objectViewer compose want 403, got %d %s", rec.Code, rec.Body.String())
	}

	creator := "creator@example.com"
	if err := st.PutIAMPolicyJSON(store.BucketIAMResource("pub"), authz.Policy{
		Bindings: []authz.Binding{{
			Role: "roles/storage.objectCreator", Members: []string{"serviceAccount:" + creator},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	h.Principal = func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: creator, IsRoot: false}, true
	}
	req = httptest.NewRequest(http.MethodPost, "/storage/v1/b/pub/o/c-creator.txt/compose", bytes.NewReader([]byte(composeBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("objectCreator without get compose want 403, got %d %s", rec.Code, rec.Body.String())
	}

	composer := "composer@example.com"
	if err := st.PutIAMPolicyJSON(store.BucketIAMResource("pub"), authz.Policy{
		Bindings: []authz.Binding{
			{Role: "roles/storage.objectCreator", Members: []string{"serviceAccount:" + composer}},
			{Role: "roles/storage.objectViewer", Members: []string{"serviceAccount:" + composer}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	h.Principal = func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: composer, IsRoot: false}, true
	}
	req = httptest.NewRequest(http.MethodPost, "/storage/v1/b/pub/o/c.txt/compose", bytes.NewReader([]byte(composeBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("compose with create+get: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGCSSignedURLPUTRequiresCreate(t *testing.T) {
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
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateBucket("sign-b", project, "US", "STANDARD"); err != nil {
		t.Fatal(err)
	}
	viewer := "viewer@example.com"
	if err := st.PutIAMPolicyJSON(store.BucketIAMResource("sign-b"), authz.Policy{
		Bindings: []authz.Binding{{
			Role: "roles/storage.objectViewer", Members: []string{"serviceAccount:" + viewer},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := &gcs.Handler{Store: st, Authz: &authz.Evaluator{Policies: st}, Principal: func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: viewer, IsRoot: false}, true
	}}
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/storage/v1/b/sign-b/o/x.txt:generateSignedUrl",
		bytes.NewReader([]byte(`{"method":"PUT","expires":3600}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer minting PUT signed URL want 403, got %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/storage/v1/b/sign-b/o/x.txt:generateSignedUrl",
		bytes.NewReader([]byte(`{"method":"GET","expires":3600}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer minting GET signed URL: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["signedUrl"] == nil || out["signedUrl"] == "" {
		t.Fatalf("missing signedUrl: %#v", out)
	}
}

func TestGCSSignedURLDoesNotSkipBucketIAM(t *testing.T) {
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
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateBucket("iam-b", project, "US", "STANDARD"); err != nil {
		t.Fatal(err)
	}
	iamPath := "/storage/v1/b/iam-b/iam"
	signed, err := store.GenerateV4SignedURL(store.SignedURLRequest{
		Method:  "GET",
		Host:    "127.0.0.1:4588",
		Path:    iamPath,
		Expires: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h := &gcs.Handler{Store: st, Authz: &authz.Evaluator{Policies: st}, Principal: func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	}}
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, signed, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Fatalf("signed URL on bucket IAM must not skip authz, got %d %s", rec.Code, rec.Body.String())
	}
}
