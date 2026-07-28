package store_test

import (
	"bytes"
	"testing"
	"time"

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
	if _, created, err := st.CreateSubscriptionFull(sub, topic, "noctaxris-gcp-local", 10, "http://127.0.0.1:9/push", nil, "", "", 0, false); err != nil || !created {
		t.Fatalf("sub: %v %v", created, err)
	}
	ack := 30
	ep := ""
	updated, err := st.UpdateSubscription(sub, &ack, &ep, &map[string]string{"s": "1"}, nil, nil, nil)
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

func TestGCSRewriteListDelimiterResumable(t *testing.T) {
	st := openTestStore(t)
	if _, created, err := st.CreateBucket("lab-bucket", "noctaxris-gcp-local", "US", "STANDARD"); err != nil || !created {
		t.Fatalf("create bucket: %v %v", created, err)
	}
	src, err := st.PutObjectBytes("lab-bucket", "a/src.txt", "text/plain", []byte("rewrite-me"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("lab-bucket", "a/other.txt", "text/plain", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("lab-bucket", "b/nested.txt", "text/plain", []byte("y")); err != nil {
		t.Fatal(err)
	}
	rewritten, err := st.RewriteObject("lab-bucket", "a/src.txt", 0, "lab-bucket", "a/dst.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := st.ReadObjectBytes(rewritten)
	if err != nil || string(data) != "rewrite-me" {
		t.Fatalf("rewrite data=%q err=%v", data, err)
	}
	if _, ok, _ := st.GetObject("lab-bucket", "a/src.txt", 0); !ok {
		t.Fatal("source should remain after rewrite")
	}
	if err := st.CheckGenerationMatch("lab-bucket", "a/src.txt", src.Generation); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckGenerationMatch("lab-bucket", "a/src.txt", src.Generation+1); err != store.ErrPreconditionFailed {
		t.Fatalf("expected precondition fail, got %v", err)
	}
	listed, err := st.ListObjectsDelimited("lab-bucket", "", "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Prefixes) < 2 {
		t.Fatalf("prefixes = %#v", listed.Prefixes)
	}
	sess, err := st.CreateUploadSession("lab-bucket", "resumable.txt", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := st.CompleteUploadSession(sess.UploadID, []byte("chunk"))
	if err != nil {
		t.Fatal(err)
	}
	if obj.Name != "resumable.txt" || obj.Size != 5 {
		t.Fatalf("resumable = %#v", obj)
	}
}

func TestPubSubFilterAndSeek(t *testing.T) {
	st := openTestStore(t)
	topic := "projects/noctaxris-gcp-local/topics/t-filter"
	sub := "projects/noctaxris-gcp-local/subscriptions/s-filter"
	if _, created, err := st.CreateTopic(topic, "noctaxris-gcp-local"); err != nil || !created {
		t.Fatalf("topic: %v %v", created, err)
	}
	if _, created, err := st.CreateSubscriptionFull(sub, topic, "noctaxris-gcp-local", 10, "", nil, `attributes.region = "us"`, "", 0, false); err != nil || !created {
		t.Fatalf("sub: %v %v", created, err)
	}
	if _, _, err := st.PublishFanout(topic, []byte("eu"), map[string]string{"region": "eu"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PublishFanout(topic, []byte("us"), map[string]string{"region": "us"}); err != nil {
		t.Fatal(err)
	}
	msgs, err := st.Pull(sub, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || string(msgs[0].Data) != "us" {
		t.Fatalf("filtered pull = %#v", msgs)
	}
	if err := st.SeekToTime(sub, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	msgs, err = st.Pull(sub, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("after seek expected 1, got %#v", msgs)
	}
}

func TestPubSubSnapshotCRUD(t *testing.T) {
	st := openTestStore(t)
	topic := "projects/noctaxris-gcp-local/topics/t-snap"
	sub := "projects/noctaxris-gcp-local/subscriptions/s-snap"
	snapName := "projects/noctaxris-gcp-local/snapshots/snap1"
	if _, created, err := st.CreateTopic(topic, "noctaxris-gcp-local"); err != nil || !created {
		t.Fatalf("topic: %v %v", created, err)
	}
	if _, created, err := st.CreateSubscription(sub, topic, "noctaxris-gcp-local", 10); err != nil || !created {
		t.Fatalf("sub: %v %v", created, err)
	}
	snap, created, err := st.CreateSnapshot(snapName, sub, map[string]string{"k": "v"})
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	if snap.Topic != topic || snap.ExpireTime == "" {
		t.Fatalf("snap = %#v", snap)
	}
	got, ok, err := st.GetSnapshot(snapName)
	if err != nil || !ok || got.Labels["k"] != "v" {
		t.Fatalf("get = %#v ok=%v err=%v", got, ok, err)
	}
	list, err := st.ListSnapshots("noctaxris-gcp-local")
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %#v err=%v", list, err)
	}
	deleted, err := st.DeleteSnapshot(snapName)
	if err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	_, ok, err = st.GetSnapshot(snapName)
	if err != nil || ok {
		t.Fatalf("get after delete ok=%v err=%v", ok, err)
	}
}

func TestSecretReplicationCMEKAndVersionFilter(t *testing.T) {
	st := openTestStore(t)
	name := "projects/noctaxris-gcp-local/secrets/cmek-sec"
	rep := map[string]any{"automatic": map[string]any{}}
	sec, created, err := st.CreateSecretWithMeta(name, "noctaxris-gcp-local", nil, nil, rep, "projects/p/locations/global/keyRings/r/cryptoKeys/k")
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	if sec.CMEKKmsKeyName == "" || sec.Replication["automatic"] == nil {
		t.Fatalf("secret = %#v", sec)
	}
	if _, err := st.AddSecretVersion(name, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSecretVersion(name, []byte("b")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetSecretVersionState(name, "1", store.SecretVersionDisabled); err != nil {
		t.Fatal(err)
	}
	enabled, err := st.ListSecretVersions(name, store.SecretVersionEnabled)
	if err != nil {
		t.Fatal(err)
	}
	if len(enabled) != 1 || enabled[0].VersionID != "2" {
		t.Fatalf("enabled = %#v", enabled)
	}
	disabled, err := st.ListSecretVersions(name, store.SecretVersionDisabled)
	if err != nil || len(disabled) != 1 {
		t.Fatalf("disabled = %#v err=%v", disabled, err)
	}
}


func TestPubSubDeadLetterAndExactlyOnce(t *testing.T) {
	st := openTestStore(t)
	topic := "projects/noctaxris-gcp-local/topics/main"
	dlTopic := "projects/noctaxris-gcp-local/topics/dlq"
	sub := "projects/noctaxris-gcp-local/subscriptions/s-dl"
	dlSub := "projects/noctaxris-gcp-local/subscriptions/s-dl-reader"
	if _, created, err := st.CreateTopic(topic, "noctaxris-gcp-local"); err != nil || !created {
		t.Fatalf("topic: %v %v", created, err)
	}
	if _, created, err := st.CreateTopic(dlTopic, "noctaxris-gcp-local"); err != nil || !created {
		t.Fatalf("dl topic: %v %v", created, err)
	}
	created, ok, err := st.CreateSubscriptionFull(sub, topic, "noctaxris-gcp-local", 1, "", nil, "", dlTopic, 5, true)
	if err != nil || !ok {
		t.Fatalf("sub: ok=%v err=%v", ok, err)
	}
	if !created.EnableExactlyOnceDelivery || created.DeadLetterTopic != dlTopic || created.MaxDeliveryAttempts != 5 {
		t.Fatalf("created = %#v", created)
	}
	if _, created, err := st.CreateSubscription(dlSub, dlTopic, "noctaxris-gcp-local", 10); err != nil || !created {
		t.Fatalf("dl sub: %v %v", created, err)
	}
	if _, err := st.Publish(topic, []byte("poison"), nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		msgs, err := st.Pull(sub, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) != 1 {
			t.Fatalf("attempt %d msgs=%d", i+1, len(msgs))
		}
		// Expire lease so next Pull redelivers.
		if err := st.ModifyAckDeadline(sub, []string{msgs[0].AckID}, 0); err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := st.Pull(sub, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected dead-lettered, still have %#v", msgs)
	}
	dlMsgs, err := st.Pull(dlSub, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dlMsgs) != 1 || string(dlMsgs[0].Data) != "poison" {
		t.Fatalf("dl msgs = %#v", dlMsgs)
	}
}

