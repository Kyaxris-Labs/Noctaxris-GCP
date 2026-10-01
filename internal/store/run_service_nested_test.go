package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestUpdateRunServiceNested(t *testing.T) {
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

	name := "projects/noctaxris-gcp-local/locations/us-central1/services/web"
	created, err := st.CreateRunService(store.RunService{
		Name: name, ProjectID: "noctaxris-gcp-local", Location: "us-central1", ServiceID: "web",
	})
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	if _, err := st.UpdateRunServiceNested("", "u", "h", 1, "c"); err == nil {
		t.Fatal("empty name must be rejected")
	}
	if ok, err := st.UpdateRunServiceNested("projects/x/locations/l/services/none", "u", "h", 1, "c"); err != nil || ok {
		t.Fatalf("missing service: ok=%v err=%v", ok, err)
	}

	const uri = "http://127.0.0.1:4588/run/noctaxris-gcp-local/us-central1/web/"
	if ok, err := st.UpdateRunServiceNested(name, uri, "noctaxris-gcp-engine", 49153, "ctr-1"); err != nil || !ok {
		t.Fatalf("update: ok=%v err=%v", ok, err)
	}
	got, ok, err := st.GetRunService(name)
	if err != nil || !ok {
		t.Fatalf("get: %v %v", ok, err)
	}
	if got.URI != uri || got.NestedHost != "noctaxris-gcp-engine" || got.NestedPort != 49153 || got.ContainerID != "ctr-1" {
		t.Fatalf("nested=%#v", got)
	}

	patched, ok, err := st.UpdateRunService(name, `{"containers":[{"image":"x"}]}`, "", "")
	if err != nil || !ok {
		t.Fatalf("patch: %v %v", ok, err)
	}
	if patched.ContainerID != "ctr-1" || patched.NestedPort != 49153 {
		t.Fatalf("patch must keep nested fields: %#v", patched)
	}

	if ok, err := st.UpdateRunServiceNested(name, "", "", 0, ""); err != nil || !ok {
		t.Fatalf("clear: ok=%v err=%v", ok, err)
	}
	got, _, _ = st.GetRunService(name)
	if got.URI != store.DefaultRunServiceURI(name) || got.NestedHost != "" || got.NestedPort != 0 || got.ContainerID != "" {
		t.Fatalf("cleared=%#v", got)
	}

	list, err := st.ListRunServices("noctaxris-gcp-local", "us-central1")
	if err != nil || len(list) != 1 || list[0].NestedHost != "" {
		t.Fatalf("list=%#v err=%v", list, err)
	}
}
