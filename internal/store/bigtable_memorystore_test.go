package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestBigtableAndMemorystoreStoreCRUD(t *testing.T) {
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

	instOK, err := st.CreateBigtableInstance(store.BigtableInstance{
		Name: "projects/p/instances/bt", ProjectID: "p", InstanceID: "bt",
		DisplayName: "BT", ClustersJSON: `{"c1":{"location":"us-central1-b","serveNodes":1}}`,
	})
	if err != nil || !instOK {
		t.Fatalf("create bigtable instance: ok=%v err=%v", instOK, err)
	}
	dup, err := st.CreateBigtableInstance(store.BigtableInstance{
		Name: "projects/p/instances/bt", ProjectID: "p", InstanceID: "bt",
	})
	if err != nil || dup {
		t.Fatalf("duplicate bigtable instance: ok=%v err=%v", dup, err)
	}
	got, ok, err := st.GetBigtableInstance("projects/p/instances/bt")
	if err != nil || !ok || got.State != "READY" {
		t.Fatalf("get bigtable instance: %#v ok=%v err=%v", got, ok, err)
	}
	tblOK, err := st.CreateBigtableTable(store.BigtableTable{
		Name: "projects/p/instances/bt/tables/t1", InstanceName: "projects/p/instances/bt",
		ProjectID: "p", InstanceID: "bt", TableID: "t1",
		ColumnFamiliesJSON: `{"cf":{}}`,
	})
	if err != nil || !tblOK {
		t.Fatalf("create bigtable table: ok=%v err=%v", tblOK, err)
	}
	tables, err := st.ListBigtableTables("projects/p/instances/bt")
	if err != nil || len(tables) != 1 {
		t.Fatalf("list tables: %#v err=%v", tables, err)
	}
	if ok, err := st.DeleteBigtableTable("projects/p/instances/bt/tables/t1"); err != nil || !ok {
		t.Fatalf("delete table: ok=%v err=%v", ok, err)
	}
	if ok, err := st.DeleteBigtableInstance("projects/p/instances/bt"); err != nil || !ok {
		t.Fatalf("delete instance: ok=%v err=%v", ok, err)
	}

	msOK, err := st.CreateMemorystoreRedisInstance(store.MemorystoreRedisInstance{
		Name: "projects/p/locations/us-central1/instances/r1", ProjectID: "p",
		Location: "us-central1", InstanceID: "r1", Tier: "STANDARD_HA", MemorySizeGb: 2,
		AuthEnabled: true, AuthString: "store-auth-pass",
	})
	if err != nil || !msOK {
		t.Fatalf("create redis: ok=%v err=%v", msOK, err)
	}
	ms, ok, err := st.GetMemorystoreRedisInstance("projects/p/locations/us-central1/instances/r1")
	if err != nil || !ok || ms.State != "READY" || ms.Host == "" || ms.Port != 6379 {
		t.Fatalf("get redis: %#v ok=%v err=%v", ms, ok, err)
	}
	if !ms.AuthEnabled || ms.AuthString != "store-auth-pass" {
		t.Fatalf("auth fields: enabled=%v string=%q", ms.AuthEnabled, ms.AuthString)
	}
	list, err := st.ListMemorystoreRedisInstances("p", "us-central1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list redis: %#v err=%v", list, err)
	}
	if err := st.SetMemorystoreRedisRuntime(ms.Name, "noctaxris-gcp-redis-r1", "cid-1", 6379); err != nil {
		t.Fatalf("set runtime: %v", err)
	}
	ms2, ok, err := st.GetMemorystoreRedisInstance(ms.Name)
	if err != nil || !ok || ms2.Host != "noctaxris-gcp-redis-r1" || ms2.ContainerID != "cid-1" {
		t.Fatalf("runtime update: %#v ok=%v err=%v", ms2, ok, err)
	}
	if ok, err := st.DeleteMemorystoreRedisInstance(ms.Name); err != nil || !ok {
		t.Fatalf("delete redis: ok=%v err=%v", ok, err)
	}
}
