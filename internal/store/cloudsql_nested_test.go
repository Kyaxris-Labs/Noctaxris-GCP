package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestUpdateCloudSQLInstanceNested(t *testing.T) {
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
	name := "projects/" + project + "/instances/sql1"
	created, err := st.CreateCloudSQLInstance(store.CloudSQLInstance{
		Name: name, ProjectID: project, InstanceID: "sql1", Region: "us-central1",
		DatabaseVersion: "MYSQL_8_0", State: "RUNNABLE",
	})
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	if err := st.UpdateCloudSQLInstanceNested("", "h", 1, "c"); err == nil {
		t.Fatal("empty name")
	}
	if err := st.UpdateCloudSQLInstanceNested(name, "127.0.0.1", 3306, "ctr-1"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetCloudSQLInstance(name)
	if err != nil || !ok {
		t.Fatalf("get: %v %v", ok, err)
	}
	if got.Host != "127.0.0.1" || got.Port != 3306 || got.ContainerID != "ctr-1" {
		t.Fatalf("nested=%#v", got)
	}
}
