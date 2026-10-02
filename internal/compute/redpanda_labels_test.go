package compute

import "testing"

func TestRedpandaOwnerLabelsAndMatch(t *testing.T) {
	owner := RedpandaOwner{Project: "p1", Location: "us-central1", ClusterID: "c1"}
	labels := redpandaOwnerLabels(owner)
	if labels["noctaxris-gcp.kind"] != "managedkafka" {
		t.Fatalf("labels=%#v", labels)
	}
	if labels["noctaxris-gcp.project"] != "p1" || labels["noctaxris-gcp.cluster-id"] != "c1" {
		t.Fatalf("labels=%#v", labels)
	}
	if !redpandaLabelsMatch(labels, owner) {
		t.Fatal("expected match")
	}
	if redpandaLabelsMatch(nil, owner) {
		t.Fatal("nil labels should not match nonempty owner")
	}
	if !redpandaLabelsMatch(nil, RedpandaOwner{}) {
		t.Fatal("empty owner should match anything")
	}
	if redpandaLabelsMatch(map[string]string{
		"noctaxris-gcp.project":    "other",
		"noctaxris-gcp.location":   "us-central1",
		"noctaxris-gcp.cluster-id": "c1",
	}, owner) {
		t.Fatal("mismatched project should fail")
	}
}
