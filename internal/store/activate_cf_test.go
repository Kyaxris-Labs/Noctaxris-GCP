package store

import (
	"path/filepath"
	"testing"
)

func TestActivateCloudFunctionsForStorageSource(t *testing.T) {
	dir := t.TempDir()
	key, err := LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project := "noctaxris-gcp-local"
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	loc := "us-central1"
	cfg := `{"buildConfig":{"source":{"storageSource":{"bucket":"src-b","object":"src.zip"}}}}`
	name := "projects/" + project + "/locations/" + loc + "/functions/fn-src"
	created, err := st.CreateCloudFunction(CloudFunction{
		Name: name, ProjectID: project, Location: loc, FunctionID: "fn-src",
		State: "DEPLOYING", ConfigJSON: cfg,
	})
	if err != nil || !created {
		t.Fatalf("create fn: created=%v err=%v", created, err)
	}
	n, err := st.ActivateCloudFunctionsForStorageSource(project, loc, "src-b", "src.zip")
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("activated=%d", n)
	}
	n, err = st.ActivateCloudFunctionsForStorageSource(project, loc, "other", "x.zip")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("no match activated=%d", n)
	}
}
