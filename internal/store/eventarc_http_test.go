package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestEventarcCloudFunctionNameObjectDestination(t *testing.T) {
	st := openTestStore(t)
	project := "noctaxris-gcp-local"
	loc := "us-central1"
	topic := "projects/" + project + "/topics/http-ea"
	if _, created, err := st.CreateTopic(topic, project); err != nil || !created {
		t.Fatal(err)
	}
	fnName := "projects/" + project + "/locations/" + loc + "/functions/http-fn"
	if created, err := st.CreateCloudFunction(store.CloudFunction{
		Name: fnName, ProjectID: project, Location: loc, FunctionID: "http-fn", State: "ACTIVE",
	}); err != nil || !created {
		t.Fatalf("fn: %v %v", created, err)
	}
	_, created, err := st.CreateEventarcTrigger(store.EventarcTrigger{
		ProjectID: project, Location: loc, TriggerID: "http-ea",
		FiltersJSON:     `[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}]`,
		DestinationJSON: `{"cloudFunction":{"name":"` + fnName + `"}}`,
		TransportJSON:   `{"pubsub":{"topic":"` + topic + `"}}`,
	})
	if err != nil || !created {
		t.Fatalf("trigger: %v %v", created, err)
	}
	store.ClearCloudFunctionInvokes()
	st.DeliverEventarcForPubSub(topic, []byte(`{"x":1}`), nil)
	inv := store.ListCloudFunctionInvokes()
	if len(inv) < 1 {
		t.Fatalf("expected invoke via cloudFunction name object, got %#v", inv)
	}
}
