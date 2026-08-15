package store

import (
	"path/filepath"
	"testing"
)

func TestEventarcURIHelpers(t *testing.T) {
	if !isLabInvokeURI("http://127.0.0.1:4588/v2/projects/p/locations/us/functions/f:invoke") {
		t.Fatal("expected lab invoke URI")
	}
	if isLabInvokeURI("https://example.com/hook") {
		t.Fatal("public URI is not lab invoke")
	}
	if functionNameFromInvokeURI("http://127.0.0.1:4588/v2/projects/p/locations/us/functions/myfn:invoke") == "" {
		t.Fatal("expected function name from invoke URI")
	}
	trig := EventarcTrigger{
		ProjectID: "p", Location: "us-central1",
		DestinationJSON: `{"cloudFunction":"projects/p/locations/us-central1/functions/fn1"}`,
	}
	if cloudFunctionNameFromDestination(trig) == "" {
		t.Fatal("expected cloud function name from destination")
	}
	trig.DestinationJSON = `{"cloudFunction":{"name":"projects/p/locations/us-central1/functions/fn2"}}`
	if cloudFunctionNameFromDestination(trig) == "" {
		t.Fatal("expected cloud function name from object destination")
	}
}

func TestEventarcAuthHeaderAndListSubsPushFail(t *testing.T) {
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
	sa := "ea-sa@" + project + ".iam.gserviceaccount.com"
	trig := EventarcTrigger{ProjectID: project, ServiceAccount: sa}
	hdr, err := st.eventarcAuthHeader(trig, "https://example.com/hook")
	if err != nil {
		t.Fatal(err)
	}
	if hdr == "" || hdr[:7] != "Bearer " {
		t.Fatalf("auth header=%q", hdr)
	}

	topic := "projects/" + project + "/topics/push-t"
	if _, _, err := st.CreateTopic(topic, project); err != nil {
		t.Fatal(err)
	}
	dlTopic := "projects/" + project + "/topics/dlq"
	if _, _, err := st.CreateTopic(dlTopic, project); err != nil {
		t.Fatal(err)
	}
	sub := "projects/" + project + "/subscriptions/push-s"
	if _, _, err := st.CreateSubscriptionFull(sub, topic, project, 10, "", nil, "", dlTopic, 5, false, "", ""); err != nil {
		t.Fatal(err)
	}
	subs, err := st.ListSubscriptions(project)
	if err != nil || len(subs) < 1 {
		t.Fatalf("list subs: n=%d err=%v", len(subs), err)
	}
	msgID, err := st.Publish(topic, []byte("x"), map[string]string{"k": "v"})
	if err != nil || msgID == "" {
		t.Fatalf("publish: id=%q err=%v", msgID, err)
	}
	pulled, err := st.Pull(sub, 10)
	if err != nil || len(pulled) < 1 {
		t.Fatalf("pull: n=%d err=%v", len(pulled), err)
	}
	ack := pulled[0].AckID
	dead, err := st.RecordPushDeliveryFailure(sub, ack)
	if err != nil {
		t.Fatal(err)
	}
	_ = dead
	dead, err = st.RecordPushDeliveryFailure(sub, ack)
	if err != nil {
		t.Fatal(err)
	}
	_ = dead
}
