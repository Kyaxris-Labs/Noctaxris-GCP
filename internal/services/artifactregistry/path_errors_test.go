package artifactregistry_test

import (
	"net/http"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/artifactregistry"
)

func TestIsRegistryV2Path(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/v2", true},
		{"/v2/", true},
		{"/v2/foo/blobs/sha256:abc", true},
		{"/v2/foo/manifests/latest", true},
		{"/v2/foo/tags/list", false},
		{"/v1/repos", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := artifactregistry.IsRegistryV2Path(tc.path); got != tc.want {
			t.Fatalf("IsRegistryV2Path(%q)=%v want %v", tc.path, got, tc.want)
		}
	}
}

func TestArtifactRegistryCRUDErrorsAndDeletes(t *testing.T) {
	f := setupRegistry(t)
	loc := artifactregistry.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/repositories"

	rec := f.do(http.MethodPost, base, []byte(`{"format":"DOCKER"}`), nil)
	if rec.Code == http.StatusOK {
		t.Fatal("create without repositoryId should fail")
	}
	rec = f.do(http.MethodPost, base+"?repositoryId=err-repo",
		[]byte(`{"format":"docker","description":"d","labels":{"a":"b"},"mode":"STANDARD_REPOSITORY"}`), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodPost, base+"?repositoryId=err-repo", []byte(`{"format":"DOCKER"}`), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate repo: %d", rec.Code)
	}

	repo := base + "/err-repo"
	rec = f.do(http.MethodPatch, repo+"?updateMask=description,labels",
		[]byte(`{"description":"updated","labels":{"env":"lab"}}`), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodGet, repo+"/files", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list files: %d", rec.Code)
	}
	rec = f.do(http.MethodGet, repo+"/packages/nope/tags", nil, nil)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("list tags: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.do(http.MethodPost, repo+"/packages", []byte(`{"displayName":"x"}`), nil)
	if rec.Code == http.StatusOK {
		t.Fatal("create package without id")
	}
	rec = f.do(http.MethodPost, repo+"/packages?packageId=app",
		[]byte(`{"displayName":"app"}`), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pkg: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodPost, repo+"/packages?packageId=app",
		[]byte(`{"displayName":"app"}`), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup pkg: %d", rec.Code)
	}
	rec = f.do(http.MethodGet, repo+"/packages/missing", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing pkg: %d", rec.Code)
	}

	rec = f.do(http.MethodPost, repo+"/packages/app/versions?versionId=1.0.0",
		[]byte(`{"description":"v1","relatedTags":["latest"]}`), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("create version: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodGet, repo+"/packages/app/versions/missing", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing version: %d", rec.Code)
	}
	rec = f.do(http.MethodDelete, repo+"/packages/app/versions/1.0.0", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete version: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodDelete, repo+"/packages/app/versions/1.0.0", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete version: %d", rec.Code)
	}
	rec = f.do(http.MethodDelete, repo+"/packages/app", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete package: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodDelete, repo+"/packages/app", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete package: %d", rec.Code)
	}
	rec = f.do(http.MethodDelete, base+"/missing-repo", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing repo: %d", rec.Code)
	}
}

func TestRegistryV2BlobManifestErrorPaths(t *testing.T) {
	f := setupRegistry(t)
	name := "noctaxris-gcp-local/lab/errpaths"
	blob := []byte("err-blob")
	digest := sha256Sum(blob)

	rec := f.do(http.MethodPut, "/v2/"+name+"/blobs/uploads/missing-uuid?digest="+digest, blob, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown upload: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.do(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	rec = f.do(http.MethodPut, loc+"?digest=sha256:deadbeef", blob, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad digest format: %d", rec.Code)
	}
	rec = f.do(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, nil)
	loc = rec.Header().Get("Location")
	rec = f.do(http.MethodPut, loc+"?digest="+sha256Sum([]byte("other")), blob, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("digest mismatch: %d", rec.Code)
	}
	rec = f.do(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, nil)
	loc = rec.Header().Get("Location")
	rec = f.do(http.MethodPut, loc+"?digest="+digest, blob, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("good put: %d %s", rec.Code, rec.Body.String())
	}

	man := []byte(`{"schemaVersion":2}`)
	rec = f.do(http.MethodPut, "/v2/"+name+"/manifests/v1", man, map[string]string{
		"Docker-Content-Digest": sha256Sum([]byte("wrong")),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("manifest digest mismatch: %d", rec.Code)
	}
	rec = f.do(http.MethodPut, "/v2/"+name+"/manifests/v1", man, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("manifest put: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodGet, "/v2/"+name+"/blobs/"+sha256Sum([]byte("missing")), nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing blob: %d", rec.Code)
	}
	rec = f.do(http.MethodGet, "/v2/"+name+"/manifests/missing-tag", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing manifest: %d", rec.Code)
	}
}
