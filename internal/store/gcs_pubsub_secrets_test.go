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

func TestSecretAddAccessDestroy(t *testing.T) {
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
