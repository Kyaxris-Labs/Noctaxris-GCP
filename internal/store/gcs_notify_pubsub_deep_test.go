package store_test

import (
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestDeliverGCSNotificationsAndPubSubDeep(t *testing.T) {
	st := openTestStore(t)
	project := "noctaxris-gcp-local"
	if _, created, err := st.CreateBucket("notify-deep", project, "US", "STANDARD"); err != nil || !created {
		t.Fatal(err)
	}
	topic := "projects/" + project + "/topics/notify-deep-t"
	if _, created, err := st.CreateTopicWithLabels(topic, project, map[string]string{"env": "lab"}); err != nil || !created {
		t.Fatal(err)
	}
	gotTopic, ok, err := st.GetTopic(topic)
	if err != nil || !ok || gotTopic.Name != topic {
		t.Fatal(err)
	}
	if _, err := st.UpdateTopicLabels(topic, map[string]string{"env": "prod"}); err != nil {
		t.Fatal(err)
	}
	topics, err := st.ListTopics(project)
	if err != nil || len(topics) < 1 {
		t.Fatalf("topics=%v", topics)
	}

	sub := "projects/" + project + "/subscriptions/notify-deep-s"
	if _, created, err := st.CreateSubscription(sub, topic, project, 10); err != nil || !created {
		t.Fatal(err)
	}

	n, err := st.CreateNotificationConfig("notify-deep", store.NotificationConfig{
		Topic: topic, PayloadFormat: "JSON_API_V1",
		EventTypes: []string{store.GCSEventObjectFinalize, store.GCSEventObjectDelete},
		ObjectNamePrefix: "in/", CustomAttributes: map[string]string{"x": "y"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = n

	obj, err := st.PutObjectBytes("notify-deep", "in/hello.txt", "text/plain", []byte("hi"))
	if err != nil {
		t.Fatal(err)
	}
	// PutObjectBytes already schedules async delivery; also call sync for coverage.
	st.DeliverGCSNotifications(store.GCSEventObjectFinalize, obj)
	deadline := time.Now().Add(2 * time.Second)
	var msgs []store.PubSubMessage
	for time.Now().Before(deadline) {
		msgs, err = st.Pull(sub, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(msgs) < 1 {
		t.Fatalf("expected notification publish, got none")
	}
	ackIDs := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ackIDs = append(ackIDs, m.AckID)
	}
	if err := st.Acknowledge(sub, ackIDs); err != nil {
		t.Fatal(err)
	}

	none, err := st.CreateNotificationConfig("notify-deep", store.NotificationConfig{
		Topic: topic, PayloadFormat: "NONE", EventTypes: []string{store.GCSEventObjectDelete},
	})
	if err != nil {
		t.Fatal(err)
	}
	st.DeliverGCSNotifications(store.GCSEventObjectDelete, obj)
	_ = none

	snapName := "projects/" + project + "/snapshots/notify-snap"
	if _, created, err := st.CreateSnapshot(snapName, sub, map[string]string{"k": "v"}); err != nil || !created {
		t.Fatalf("snapshot: %v %v", created, err)
	}
	snap, ok, err := st.GetSnapshot(snapName)
	if err != nil || !ok || snap.Name != snapName {
		t.Fatal(err)
	}
	snaps, err := st.ListSnapshots(project)
	if err != nil || len(snaps) < 1 {
		t.Fatalf("snaps=%v", snaps)
	}
	if err := st.SeekToTime(sub, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	ok, err = st.DeleteSnapshot(snapName)
	if err != nil || !ok {
		t.Fatal(err)
	}

	mid, copies, err := st.PublishFanout(topic, []byte("fan"), map[string]string{"a": "b"})
	if err != nil || mid == "" || len(copies) < 1 {
		t.Fatalf("fanout mid=%s copies=%d err=%v", mid, len(copies), err)
	}
	pulled, err := st.Pull(sub, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(pulled) > 0 {
		ids := []string{pulled[0].AckID}
		if err := st.ModifyAckDeadline(sub, ids, 30); err != nil {
			t.Fatal(err)
		}
		if err := st.Acknowledge(sub, ids); err != nil {
			t.Fatal(err)
		}
	}

	ok, err = st.DeleteSubscription(sub)
	if err != nil || !ok {
		t.Fatal(err)
	}
	ok, err = st.DeleteTopic(topic)
	if err != nil || !ok {
		t.Fatal(err)
	}
}
