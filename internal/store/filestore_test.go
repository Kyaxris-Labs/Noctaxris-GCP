package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestFilestoreInstanceCRUD(t *testing.T) {
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

	name := "projects/p/locations/us-central1-a/instances/nfs1"
	ok, err := st.CreateFilestoreInstance(store.FilestoreInstance{
		Name: name, ProjectID: "p", Location: "us-central1-a", InstanceID: "nfs1",
		Tier: "BASIC_HDD", FileSharesJSON: `[{"name":"share1","capacityGb":"1024"}]`,
		NetworksJSON: `[{"network":"default","modes":["MODE_IPV4"]}]`,
	})
	if err != nil || !ok {
		t.Fatalf("create: ok=%v err=%v", ok, err)
	}
	inst, found, err := st.GetFilestoreInstance(name)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if inst.Tier != "BASIC_HDD" || inst.State != "READY" {
		t.Fatalf("inst=%#v", inst)
	}
	list, err := st.ListFilestoreInstances("p", "us-central1-a")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if delOK, err := st.DeleteFilestoreInstance(name); err != nil || !delOK {
		t.Fatalf("delete: ok=%v err=%v", delOK, err)
	}
}
