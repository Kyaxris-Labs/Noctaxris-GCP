package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCloudSQLInstanceStore(t *testing.T) {
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

	name := store.CloudSQLInstanceResourceName("p1", "inst1")
	created, err := st.CreateCloudSQLInstance(store.CloudSQLInstance{
		Name: name, ProjectID: "p1", InstanceID: "inst1", Region: "us-central1",
		DatabaseVersion: "POSTGRES_16",
	})
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	inst, ok, err := st.GetCloudSQLInstance(name)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if inst.Port != 5432 || inst.State != "RUNNABLE" {
		t.Fatalf("inst=%+v", inst)
	}
	list, err := st.ListCloudSQLInstances("p1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%d err=%v", len(list), err)
	}
	del, cid, err := st.DeleteCloudSQLInstance(name)
	if err != nil || !del || cid != "" {
		t.Fatalf("delete: del=%v cid=%q err=%v", del, cid, err)
	}
}
