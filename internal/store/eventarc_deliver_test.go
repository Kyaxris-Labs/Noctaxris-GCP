package store

import (
	"path/filepath"
	"testing"
)

func TestDeliverEventarcCloudFunctionAndCatcher(t *testing.T) {
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
	fnName := "projects/" + project + "/locations/us-central1/functions/ea-fn"
	created, err := st.CreateCloudFunction(CloudFunction{
		Name: fnName, ProjectID: project, Location: "us-central1", FunctionID: "ea-fn",
		URI: "http://127.0.0.1:4588/v2/" + fnName + ":invoke",
	})
	if err != nil || !created {
		t.Fatalf("create function: created=%v err=%v", created, err)
	}
	ClearCloudFunctionInvokes()
	trig := EventarcTrigger{
		ProjectID: project, Location: "us-central1", TriggerID: "cf-trig",
		DestinationJSON: `{"cloudFunction":"` + fnName + `"}`,
		FiltersJSON:     `[]`, TransportJSON: `{}`,
	}
	st.deliverEventarc(trig, map[string]any{"type": "google.cloud.pubsub.topic.v1.messagePublished", "data": "x"})
	invokes := ListCloudFunctionInvokes()
	if len(invokes) < 1 {
		t.Fatal("expected cloud function invoke from eventarc deliver")
	}

	ClearHTTPCatcher()
	trig.DestinationJSON = `{"httpEndpoint":{"uri":"http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/ea"}}`
	st.deliverEventarc(trig, map[string]any{"type": "test", "data": "y"})
	if len(ListHTTPCatcher()) < 1 {
		t.Fatal("expected http catcher delivery")
	}
}
