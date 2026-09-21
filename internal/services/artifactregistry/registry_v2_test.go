package artifactregistry_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/artifactregistry"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type registryFixture struct {
	mux  *http.ServeMux
	st   *store.Store
	who  authn.Principal
	have bool
}

func setupRegistry(t *testing.T) *registryFixture {
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
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	f := &registryFixture{
		mux:  http.NewServeMux(),
		st:   st,
		who:  authn.Principal{Email: "root@" + project + ".iam.gserviceaccount.com", IsRoot: true},
		have: true,
	}
	svc := &artifactregistry.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	svc.Mount(f.mux, func(*http.Request) (authn.Principal, bool) {
		return f.who, f.have
	})
	return f
}

func (f *registryFixture) do(method, path string, body []byte, hdr map[string]string) *httptest.ResponseRecorder {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func sha256Sum(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestRegistryV2Unauthorized401(t *testing.T) {
	f := setupRegistry(t)
	f.have = false
	rec := f.do(http.MethodGet, "/v2/", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(strings.ToLower(got), "bearer") {
		t.Fatalf("WWW-Authenticate=%q", got)
	}
	if rec.Header().Get("Docker-Distribution-API-Version") != "registry/2.0" {
		t.Fatalf("api version header=%q", rec.Header().Get("Docker-Distribution-API-Version"))
	}
}

func TestRegistryV2RootPushThenPull(t *testing.T) {
	f := setupRegistry(t)
	rec := f.do(http.MethodGet, "/v2/", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty ping status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Docker-Distribution-API-Version") != "registry/2.0" {
		t.Fatalf("ping version=%q", rec.Header().Get("Docker-Distribution-API-Version"))
	}
	var ping map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ping); err != nil {
		t.Fatal(err)
	}

	name := "noctaxris-gcp-local/lab/hello"
	blob := []byte("hello-registry-blob")
	digest := sha256Sum(blob)

	rec = f.do(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start upload status=%d body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc == "" || rec.Header().Get("Docker-Upload-UUID") == "" {
		t.Fatalf("upload location=%q uuid=%q", loc, rec.Header().Get("Docker-Upload-UUID"))
	}
	putURL := loc
	if strings.Contains(putURL, "?") {
		putURL += "&digest=" + digest
	} else {
		putURL += "?digest=" + digest
	}
	rec = f.do(http.MethodPut, putURL, blob, map[string]string{"Content-Type": "application/octet-stream"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("put blob status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Docker-Content-Digest") != digest {
		t.Fatalf("blob digest header=%q", rec.Header().Get("Docker-Content-Digest"))
	}

	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json","layers":[{"digest":"` + digest + `"}]}`)
	rec = f.do(http.MethodPut, "/v2/"+name+"/manifests/latest", manifest, map[string]string{
		"Content-Type": "application/vnd.docker.distribution.manifest.v2+json",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("put manifest status=%d body=%s", rec.Code, rec.Body.String())
	}
	manDigest := rec.Header().Get("Docker-Content-Digest")
	if manDigest != sha256Sum(manifest) {
		t.Fatalf("manifest digest=%q", manDigest)
	}

	rec = f.do(http.MethodGet, "/v2/"+name+"/blobs/"+digest, nil, nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), blob) {
		t.Fatalf("get blob status=%d body=%q", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodHead, "/v2/"+name+"/blobs/"+digest, nil, nil)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("head blob status=%d len=%d", rec.Code, rec.Body.Len())
	}
	rec = f.do(http.MethodGet, "/v2/"+name+"/manifests/latest", nil, nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), manifest) {
		t.Fatalf("get manifest status=%d body=%q", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodGet, "/v2/"+name+"/manifests/"+manDigest, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get manifest by digest status=%d", rec.Code)
	}
}

func TestRegistryV2NonRootWithoutIAM403(t *testing.T) {
	f := setupRegistry(t)
	name := "noctaxris-gcp-local/lab/denied"
	blob := []byte("secret-layer")
	digest := sha256Sum(blob)
	rec := f.do(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodPut, rec.Header().Get("Location")+"?digest="+digest, blob, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("put blob: %d %s", rec.Code, rec.Body.String())
	}
	man := []byte(`{"schemaVersion":2}`)
	rec = f.do(http.MethodPut, "/v2/"+name+"/manifests/v1", man, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("put man: %d %s", rec.Code, rec.Body.String())
	}

	f.who = authn.Principal{Email: "nobody@example.com", IsRoot: false}
	rec = f.do(http.MethodGet, "/v2/"+name+"/blobs/"+digest, nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("blob deny status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodGet, "/v2/"+name+"/manifests/v1", nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("manifest deny status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("push deny status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegistryV2GrantDownloadArtifactsPull200(t *testing.T) {
	f := setupRegistry(t)
	project := "noctaxris-gcp-local"
	name := project + "/lab/pullok"
	blob := []byte("pull-me-layer")
	digest := sha256Sum(blob)
	rec := f.do(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodPut, rec.Header().Get("Location")+"?digest="+digest, blob, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("put blob: %d %s", rec.Code, rec.Body.String())
	}
	man := []byte(`{"schemaVersion":2,"layers":[{"digest":"` + digest + `"}]}`)
	rec = f.do(http.MethodPut, "/v2/"+name+"/manifests/latest", man, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("put man: %d %s", rec.Code, rec.Body.String())
	}

	puller := "puller@" + project + ".iam.gserviceaccount.com"
	if _, err := f.st.CreateCustomRole(project, "arPull", "AR pull", "", "GA", []string{"artifactregistry.repositories.downloadArtifacts"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + project + "/roles/arPull",
			Members: []string{"serviceAccount:" + puller},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	f.who = authn.Principal{Email: puller, IsRoot: false}
	rec = f.do(http.MethodGet, "/v2/"+name+"/blobs/"+digest, nil, nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), blob) {
		t.Fatalf("granted pull status=%d body=%q", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodGet, "/v2/"+name+"/manifests/latest", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("granted manifest status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("puller must not push status=%d", rec.Code)
	}
}

func TestRegistryV2ListFilesSizeBytesMatchesBlob(t *testing.T) {
	f := setupRegistry(t)
	name := "noctaxris-gcp-local/lab/sized"
	blob := []byte("size-check-payload")
	digest := sha256Sum(blob)
	rec := f.do(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodPut, rec.Header().Get("Location")+"?digest="+digest, blob, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("put blob: %d %s", rec.Code, rec.Body.String())
	}
	man := []byte(`{"schemaVersion":2,"config":{"digest":"` + digest + `"}}`)
	rec = f.do(http.MethodPut, "/v2/"+name+"/manifests/latest", man, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("put man: %d %s", rec.Code, rec.Body.String())
	}

	filesURL := "/v1/projects/noctaxris-gcp-local/locations/" + artifactregistry.DefaultLocation + "/repositories/lab/files"
	rec = f.do(http.MethodGet, filesURL, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("listFiles status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	files, _ := body["files"].([]any)
	want := strconv.Itoa(len(blob))
	found := false
	for _, raw := range files {
		fm, _ := raw.(map[string]any)
		if fm["sizeBytes"] == want || fm["sizeBytes"] == float64(len(blob)) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sizeBytes %s in files=%s", want, rec.Body.String())
	}

	pkgURL := "/v1/projects/noctaxris-gcp-local/locations/" + artifactregistry.DefaultLocation + "/repositories/lab/packages"
	rec = f.do(http.MethodGet, pkgURL, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("listPackages status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pkgs map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &pkgs)
	if len(pkgs["packages"].([]any)) < 1 {
		t.Fatalf("packages=%s", rec.Body.String())
	}
}
