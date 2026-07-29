package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestKafkaClusterStoreCRUD(t *testing.T) {
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

	ok, err := st.CreateKafkaCluster(store.KafkaCluster{
		Name:             "projects/p/locations/us-central1/clusters/c1",
		ProjectID:        "p",
		Location:         "us-central1",
		ClusterID:        "c1",
		BootstrapServers: "c1.us-central1.kafka.noctaxris-gcp.lab:9092",
		State:            "ACTIVE",
		CreatedAt:        "2026-01-01T00:00:00Z",
	})
	if err != nil || !ok {
		t.Fatalf("create ok=%v err=%v", ok, err)
	}
	c, found, err := st.GetKafkaCluster("projects/p/locations/us-central1/clusters/c1")
	if err != nil || !found || c.ClusterID != "c1" {
		t.Fatalf("get c=%#v found=%v err=%v", c, found, err)
	}
	list, err := st.ListKafkaClusters("p", "us-central1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if err := st.UpdateKafkaClusterNested(c.Name, "broker:9092", "cid", "ACTIVE"); err != nil {
		t.Fatal(err)
	}
	_, ok, err = st.DeleteKafkaCluster(c.Name)
	if err != nil || !ok {
		t.Fatalf("delete ok=%v err=%v", ok, err)
	}
}
