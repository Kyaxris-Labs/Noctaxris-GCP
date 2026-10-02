package store

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
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
	if err := st.PutIAMPolicyJSON(fnName, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/cloudfunctions.invoker",
			Members: []string{"allUsers"},
		}},
	}); err != nil {
		t.Fatal(err)
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

	ClearCloudFunctionInvokes()
	deniedFn := "projects/" + project + "/locations/us-central1/functions/ea-denied"
	if _, err := st.CreateCloudFunction(CloudFunction{
		Name: deniedFn, ProjectID: project, Location: "us-central1", FunctionID: "ea-denied",
		URI: "http://127.0.0.1:4588/v2/" + deniedFn + ":invoke",
	}); err != nil {
		t.Fatal(err)
	}
	st.deliverEventarc(EventarcTrigger{
		ProjectID: project, Location: "us-central1", TriggerID: "no-invoker",
		DestinationJSON: `{"cloudFunction":"` + deniedFn + `"}`,
		FiltersJSON:     `[]`, TransportJSON: `{}`,
	}, map[string]any{"type": "test", "data": "deny"})
	if len(ListCloudFunctionInvokes()) != 0 {
		t.Fatal("Eventarc must not invoke Functions without Invoker")
	}

	// SA Invoker on the function resource (not allUsers) must allow delivery.
	ClearCloudFunctionInvokes()
	saFn := "projects/" + project + "/locations/us-central1/functions/ea-sa"
	if _, err := st.CreateCloudFunction(CloudFunction{
		Name: saFn, ProjectID: project, Location: "us-central1", FunctionID: "ea-sa",
		URI: "http://127.0.0.1:4588/v2/" + saFn + ":invoke",
	}); err != nil {
		t.Fatal(err)
	}
	deliverySA := "eventarc-deliver@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureServiceAccount(project, deliverySA, "eventarc"); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON(saFn, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/cloudfunctions.invoker",
			Members: []string{"serviceAccount:" + deliverySA},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	st.deliverEventarc(EventarcTrigger{
		ProjectID: project, Location: "us-central1", TriggerID: "sa-invoker",
		ServiceAccount:  deliverySA,
		DestinationJSON: `{"cloudFunction":"` + saFn + `"}`,
		FiltersJSON:     `[]`, TransportJSON: `{}`,
	}, map[string]any{"type": "test", "data": "sa"})
	if len(ListCloudFunctionInvokes()) < 1 {
		t.Fatal("Eventarc must invoke Functions when trigger SA has Invoker")
	}

	ClearHTTPCatcher()
	trig.DestinationJSON = `{"httpEndpoint":{"uri":"http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/ea"}}`
	st.deliverEventarc(trig, map[string]any{"type": "test", "data": "y"})
	if len(ListHTTPCatcher()) < 1 {
		t.Fatal("expected http catcher delivery")
	}
}
