package store_test

import (
	"bytes"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestArRegistryV2BlobManifestAndFileSize(t *testing.T) {
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

	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	payload := []byte("in-process-blob")
	if err := st.PutArRegistryBlob(digest, payload); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetArRegistryBlob(digest)
	if err != nil || !ok || !bytes.Equal(got, payload) {
		t.Fatalf("get blob ok=%v err=%v got=%q", ok, err, got)
	}
	sz, ok, err := st.ArRegistryBlobSize(digest)
	if err != nil || !ok || sz != int64(len(payload)) {
		t.Fatalf("size=%d ok=%v err=%v", sz, ok, err)
	}
	if err := st.LinkArRegistryBlob(digest, "noctaxris-gcp-local/lab/img"); err != nil {
		t.Fatal(err)
	}

	repoName := "projects/noctaxris-gcp-local/locations/us-central1/repositories/lab"
	if _, err := st.CreateArRepository(store.ArRepository{
		Name: repoName, ProjectID: "noctaxris-gcp-local", Location: "us-central1", RepositoryID: "lab", Format: "DOCKER",
	}); err != nil {
		t.Fatal(err)
	}
	pkg := repoName + "/packages/img"
	if err := st.UpsertArPackage(store.ArPackage{Name: pkg, RepositoryName: repoName, PackageID: "img"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertArVersion(store.ArVersion{
		Name: pkg + "/versions/" + digest, PackageName: pkg, VersionID: digest,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutArRegistryManifest(store.ArRegistryManifest{
		Name: "noctaxris-gcp-local/lab/img", Reference: "latest", Digest: digest,
		MediaType: "application/vnd.docker.distribution.manifest.v2+json", Data: payload,
	}); err != nil {
		t.Fatal(err)
	}
	m, ok, err := st.GetArRegistryManifest("noctaxris-gcp-local/lab/img", "latest")
	if err != nil || !ok || !bytes.Equal(m.Data, payload) {
		t.Fatalf("manifest ok=%v err=%v", ok, err)
	}

	files, err := st.ListArFilesTheatreDeepen(repoName)
	if err != nil {
		t.Fatal(err)
	}
	want := strconv.Itoa(len(payload))
	found := false
	for _, f := range files {
		if f.SizeBytes == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("sizeBytes %s missing from %#v", want, files)
	}
}
