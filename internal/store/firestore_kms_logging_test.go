package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestFirestoreDocRoundTrip(t *testing.T) {
	st := openTestStore(t)
	path := "projects/p1/databases/(default)/documents/users/u1"
	doc := store.FirestoreDoc{
		Path:         path,
		ProjectID:    "p1",
		CollectionID: "users",
		DocumentID:   "u1",
		FieldsJSON:   `{"name":{"stringValue":"Ada"}}`,
	}
	if err := st.PutFirestoreDoc(doc); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetFirestoreDoc(path)
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	if got.FieldsJSON != doc.FieldsJSON {
		t.Fatalf("fields = %q", got.FieldsJSON)
	}
	doc.FieldsJSON = `{"name":{"stringValue":"Bob"}}`
	if err := st.PutFirestoreDoc(doc); err != nil {
		t.Fatal(err)
	}
	got, _, err = st.GetFirestoreDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.FieldsJSON != doc.FieldsJSON {
		t.Fatalf("updated fields = %q", got.FieldsJSON)
	}
	ok, err = st.DeleteFirestoreDoc(path)
	if err != nil || !ok {
		t.Fatalf("delete ok=%v err=%v", ok, err)
	}
	_, ok, err = st.GetFirestoreDoc(path)
	if err != nil || ok {
		t.Fatalf("after delete ok=%v err=%v", ok, err)
	}
}

func TestKMSKeyLifecycle(t *testing.T) {
	st := openTestStore(t)
	krName := "projects/p1/locations/global/keyRings/ring1"
	created, err := st.CreateKMSKeyRing(store.KMSKeyRing{
		Name: krName, ProjectID: "p1", Location: "global",
	})
	if err != nil || !created {
		t.Fatalf("create ring created=%v err=%v", created, err)
	}
	plain := []byte("0123456789abcdef0123456789abcdef")
	sealed, err := st.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	keyName := krName + "/cryptoKeys/key1"
	verName := keyName + "/cryptoKeyVersions/1"
	created, err = st.CreateKMSCryptoKey(
		store.KMSCryptoKey{Name: keyName, KeyRing: krName},
		store.KMSKeyVersion{
			Name: verName, CryptoKey: keyName, VersionID: "1",
			State: store.KMSStateEnabled, KeyMaterialCiphertext: sealed,
		},
	)
	if err != nil || !created {
		t.Fatalf("create key created=%v err=%v", created, err)
	}
	v, ok, err := st.PrimaryKMSKeyVersion(keyName)
	if err != nil || !ok {
		t.Fatalf("primary ok=%v err=%v", ok, err)
	}
	out, err := st.Unseal(v.KeyMaterialCiphertext)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(plain) {
		t.Fatalf("material mismatch")
	}
	v, ok, err = st.DestroyKMSKeyVersion(verName)
	if err != nil || !ok {
		t.Fatalf("destroy ok=%v err=%v", ok, err)
	}
	if v.State != store.KMSStateDestroyed {
		t.Fatalf("state = %q", v.State)
	}
}

func TestLogEntriesWriteList(t *testing.T) {
	st := openTestStore(t)
	entries := []store.LogEntry{{
		InsertID: "i1", ProjectID: "p1",
		LogName: "projects/p1/logs/app", Severity: "INFO",
		PayloadJSON: `{"textPayload":"hello lab"}`,
	}, {
		InsertID: "i2", ProjectID: "p1",
		LogName: "projects/p1/logs/other", Severity: "ERROR",
		PayloadJSON: `{"textPayload":"nope"}`,
	}}
	if err := st.WriteLogEntries(entries); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListLogEntries(store.ListLogEntriesFilter{
		ProjectID: "p1", ExactLogName: "projects/p1/logs/app", PageSize: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].InsertID != "i1" {
		t.Fatalf("exact logName got %#v", got)
	}
	got, err = st.ListLogEntries(store.ListLogEntriesFilter{
		ProjectID: "p1", TextPayloadContain: "hello", PageSize: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].InsertID != "i1" {
		t.Fatalf("text contains got %#v", got)
	}
}
