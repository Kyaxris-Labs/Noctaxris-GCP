package store_test

import (
	"bytes"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestGCSBucketObjectRoundTrip(t *testing.T) {
	st := openTestStore(t)
	b, created, err := st.CreateBucket("lab-bucket", "noctaxris-gcp-local", "US", "STANDARD")
	if err != nil || !created {
		t.Fatalf("create bucket: created=%v err=%v", created, err)
	}
	if b.Name != "lab-bucket" {
		t.Fatalf("name = %q", b.Name)
	}
	payload := []byte("hello-gcs")
	obj, err := st.PutObjectBytes("lab-bucket", "path/to/obj.txt", "text/plain", payload)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Generation != 1 || obj.Size != int64(len(payload)) {
		t.Fatalf("object = %#v", obj)
	}
	got, ok, err := st.GetObject("lab-bucket", "path/to/obj.txt", 0)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	data, err := st.ReadObjectBytes(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("data = %q", data)
	}
	_, err = st.PutObjectBytes("lab-bucket", "path/to/obj.txt", "text/plain", []byte("v2"))
	if err != nil {
		t.Fatal(err)
	}
	latest, ok, err := st.GetObject("lab-bucket", "path/to/obj.txt", 0)
	if err != nil || !ok || latest.Generation != 2 {
		t.Fatalf("latest = %#v ok=%v err=%v", latest, ok, err)
	}
}

func TestPubSubPublishPullAck(t *testing.T) {
	st := openTestStore(t)
	topic := "projects/noctaxris-gcp-local/topics/t1"
	sub := "projects/noctaxris-gcp-local/subscriptions/s1"
	if _, created, err := st.CreateTopic(topic, "noctaxris-gcp-local"); err != nil || !created {
		t.Fatalf("topic: %v %v", created, err)
	}
	if _, created, err := st.CreateSubscription(sub, topic, "noctaxris-gcp-local", 10); err != nil || !created {
		t.Fatalf("sub: %v %v", created, err)
	}
	msgID, err := st.Publish(topic, []byte("ping"), map[string]string{"k": "v"})
	if err != nil || msgID == "" {
		t.Fatalf("publish: id=%q err=%v", msgID, err)
	}
	msgs, err := st.Pull(sub, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || string(msgs[0].Data) != "ping" {
		t.Fatalf("msgs = %#v", msgs)
	}
	if err := st.Acknowledge(sub, []string{msgs[0].AckID}); err != nil {
		t.Fatal(err)
	}
	msgs, err = st.Pull(sub, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected empty after ack, got %#v", msgs)
	}
}

func TestGCSComposeCopyPatchIAM(t *testing.T) {
	st := openTestStore(t)
	if _, created, err := st.CreateBucket("lab-bucket", "noctaxris-gcp-local", "US", "STANDARD"); err != nil || !created {
		t.Fatalf("create bucket: %v %v", created, err)
	}
	if _, err := st.PutObjectBytes("lab-bucket", "a.txt", "text/plain", []byte("AA")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("lab-bucket", "b.txt", "text/plain", []byte("BB")); err != nil {
		t.Fatal(err)
	}
	composed, err := st.ComposeObject("lab-bucket", "c.txt", []string{"a.txt", "b.txt"}, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	data, err := st.ReadObjectBytes(composed)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "AABB" {
		t.Fatalf("compose = %q", data)
	}
	copied, err := st.CopyObject("lab-bucket", "c.txt", 0, "lab-bucket", "d.txt")
	if err != nil {
		t.Fatal(err)
	}
	if copied.Name != "d.txt" {
		t.Fatalf("copy name = %q", copied.Name)
	}
	meta := map[string]string{"k": "v"}
	patched, err := st.PatchObjectMetadata("lab-bucket", "d.txt", 0, &store.ObjectMeta{
		ContentType: "application/octet-stream",
		Metadata:    meta,
	})
	if err != nil {
		t.Fatal(err)
	}
	if patched.ContentType != "application/octet-stream" || patched.Metadata["k"] != "v" {
		t.Fatalf("patch = %#v", patched)
	}
	loc := "EU"
	b, err := st.PatchBucket("lab-bucket", &loc, nil, &map[string]string{"env": "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Location != "EU" || b.Labels["env"] != "lab" {
		t.Fatalf("bucket patch = %#v", b)
	}
	tooMany := make([]string, 33)
	for i := range tooMany {
		tooMany[i] = "a.txt"
	}
	if _, err := st.ComposeObject("lab-bucket", "x.txt", tooMany, ""); err == nil {
		t.Fatal("expected compose cap error")
	}
}

func TestPubSubUpdateModifyPushFields(t *testing.T) {
	st := openTestStore(t)
	topic := "projects/noctaxris-gcp-local/topics/t1"
	sub := "projects/noctaxris-gcp-local/subscriptions/s1"
	if _, created, err := st.CreateTopicWithLabels(topic, "noctaxris-gcp-local", map[string]string{"a": "1"}); err != nil || !created {
		t.Fatalf("topic: %v %v", created, err)
	}
	if _, err := st.UpdateTopicLabels(topic, map[string]string{"a": "2"}); err != nil {
		t.Fatal(err)
	}
	tgot, ok, err := st.GetTopic(topic)
	if err != nil || !ok || tgot.Labels["a"] != "2" {
		t.Fatalf("topic labels = %#v", tgot)
	}
	if _, created, err := st.CreateSubscriptionFull(sub, topic, "noctaxris-gcp-local", 10, "http://127.0.0.1:9/push", nil); err != nil || !created {
		t.Fatalf("sub: %v %v", created, err)
	}
	ack := 30
	ep := ""
	updated, err := st.UpdateSubscription(sub, &ack, &ep, &map[string]string{"s": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.AckDeadlineSeconds != 30 || updated.PushEndpoint != "" || updated.Labels["s"] != "1" {
		t.Fatalf("updated = %#v", updated)
	}
	msgID, copies, err := st.PublishFanout(topic, []byte("ping"), nil)
	if err != nil || msgID == "" || len(copies) != 1 {
		t.Fatalf("fanout id=%q copies=%d err=%v", msgID, len(copies), err)
	}
	if err := st.ModifyAckDeadline(sub, []string{copies[0].AckID}, 60); err != nil {
		t.Fatal(err)
	}
}

func TestSecretPatch(t *testing.T) {
	st := openTestStore(t)
	name := "projects/noctaxris-gcp-local/secrets/db-pass"
	if _, created, err := st.CreateSecret(name, "noctaxris-gcp-local"); err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	labels := map[string]string{"team": "lab"}
	sec, err := st.PatchSecret(name, &labels, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sec.Labels["team"] != "lab" {
		t.Fatalf("labels = %#v", sec.Labels)
	}
}

func TestSecretVersionRoundTrip(t *testing.T) {
	st := openTestStore(t)
	name := "projects/noctaxris-gcp-local/secrets/db-pass"
	if _, created, err := st.CreateSecret(name, "noctaxris-gcp-local"); err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	v, err := st.AddSecretVersion(name, []byte("s3cr3t"))
	if err != nil {
		t.Fatal(err)
	}
	if v.VersionID != "1" {
		t.Fatalf("version = %q", v.VersionID)
	}
	plain, got, err := st.AccessSecretVersion(name, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "s3cr3t" || got.VersionID != "1" {
		t.Fatalf("access = %q %#v", plain, got)
	}
	if _, err := st.SetSecretVersionState(name, "1", store.SecretVersionDestroyed); err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AccessSecretVersion(name, "1")
	if err == nil {
		t.Fatal("expected destroy refuse")
	}
}
