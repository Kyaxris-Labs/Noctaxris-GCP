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

func TestKafkaClusterCapacityGcpConfigStoreEcho(t *testing.T) {
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

	capacity := `{"vcpuCount":3,"memoryBytes":3221225472}`
	gcp := `{"accessConfig":{"networkConfigs":[{"subnet":"projects/p/regions/us-central1/subnetworks/default"}]}}`
	ok, err := st.CreateKafkaCluster(store.KafkaCluster{
		Name:               "projects/p/locations/us-central1/clusters/c1",
		ProjectID:          "p",
		Location:           "us-central1",
		ClusterID:          "c1",
		CapacityConfigJSON: capacity,
		GCPConfigJSON:      gcp,
		CreatedAt:          "2026-01-01T00:00:00Z",
	})
	if err != nil || !ok {
		t.Fatalf("create ok=%v err=%v", ok, err)
	}
	c, found, err := st.GetKafkaCluster("projects/p/locations/us-central1/clusters/c1")
	if err != nil || !found {
		t.Fatalf("get found=%v err=%v", found, err)
	}
	if c.CapacityConfigJSON != capacity || c.GCPConfigJSON != gcp {
		t.Fatalf("stored capacity=%q gcp=%q", c.CapacityConfigJSON, c.GCPConfigJSON)
	}
}

func TestKafkaTopicAndACLStoreCRUD(t *testing.T) {
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

	cluster := "projects/p/locations/us-central1/clusters/c1"
	ok, err := st.CreateKafkaCluster(store.KafkaCluster{
		Name: cluster, ProjectID: "p", Location: "us-central1", ClusterID: "c1",
		CreatedAt: "2026-01-01T00:00:00Z",
	})
	if err != nil || !ok {
		t.Fatalf("cluster create ok=%v err=%v", ok, err)
	}

	topicName := cluster + "/topics/orders"
	ok, err = st.CreateKafkaTopic(store.KafkaTopic{
		Name: topicName, ClusterName: cluster, ProjectID: "p", Location: "us-central1",
		ClusterID: "c1", TopicID: "orders", PartitionCount: 2, ReplicationFactor: 1,
		ConfigsJSON: `{"cleanup.policy":"delete"}`, CreatedAt: "2026-01-01T00:00:00Z",
	})
	if err != nil || !ok {
		t.Fatalf("topic create ok=%v err=%v", ok, err)
	}
	topic, found, err := st.GetKafkaTopic(topicName)
	if err != nil || !found || topic.TopicID != "orders" || topic.PartitionCount != 2 {
		t.Fatalf("get topic=%#v found=%v err=%v", topic, found, err)
	}
	topics, err := st.ListKafkaTopics(cluster)
	if err != nil || len(topics) != 1 {
		t.Fatalf("list topics=%v err=%v", topics, err)
	}

	aclName := cluster + "/acls/topic/orders"
	ok, err = st.CreateKafkaACL(store.KafkaACL{
		Name: aclName, ClusterName: cluster, ProjectID: "p", Location: "us-central1",
		ClusterID: "c1", ACLID: "topic/orders", ResourceType: "TOPIC", ResourceName: "orders",
		PatternType: "LITERAL", ACLEntriesJSON: `[{"principal":"User:*","permissionType":"ALLOW","operation":"READ","host":"*"}]`,
		Etag: "ACAB", CreatedAt: "2026-01-01T00:00:00Z",
	})
	if err != nil || !ok {
		t.Fatalf("acl create ok=%v err=%v", ok, err)
	}
	acl, found, err := st.GetKafkaACL(aclName)
	if err != nil || !found || acl.ACLID != "topic/orders" {
		t.Fatalf("get acl=%#v found=%v err=%v", acl, found, err)
	}
	acls, err := st.ListKafkaACLs(cluster)
	if err != nil || len(acls) != 1 {
		t.Fatalf("list acls=%v err=%v", acls, err)
	}

	_, ok, err = st.DeleteKafkaACL(aclName)
	if err != nil || !ok {
		t.Fatalf("delete acl ok=%v err=%v", ok, err)
	}
	_, ok, err = st.DeleteKafkaTopic(topicName)
	if err != nil || !ok {
		t.Fatalf("delete topic ok=%v err=%v", ok, err)
	}

	// Re-create then cascade via cluster delete.
	_, _ = st.CreateKafkaTopic(store.KafkaTopic{
		Name: topicName, ClusterName: cluster, ProjectID: "p", Location: "us-central1",
		ClusterID: "c1", TopicID: "orders", PartitionCount: 1, ReplicationFactor: 1,
		CreatedAt: "2026-01-01T00:00:00Z",
	})
	_, _ = st.CreateKafkaACL(store.KafkaACL{
		Name: aclName, ClusterName: cluster, ProjectID: "p", Location: "us-central1",
		ClusterID: "c1", ACLID: "topic/orders", ResourceType: "TOPIC", ResourceName: "orders",
		CreatedAt: "2026-01-01T00:00:00Z",
	})
	_, ok, err = st.DeleteKafkaCluster(cluster)
	if err != nil || !ok {
		t.Fatalf("delete cluster ok=%v err=%v", ok, err)
	}
	topics, err = st.ListKafkaTopics(cluster)
	if err != nil || len(topics) != 0 {
		t.Fatalf("cascade topics=%v err=%v", topics, err)
	}
	acls, err = st.ListKafkaACLs(cluster)
	if err != nil || len(acls) != 0 {
		t.Fatalf("cascade acls=%v err=%v", acls, err)
	}
}
