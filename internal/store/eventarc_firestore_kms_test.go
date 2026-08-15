package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestStoreEventarcDeliveryCatcherAndFirestoreKMS(t *testing.T) {
	st := openTestStore(t)
	store.ClearHTTPCatcher()
	store.ClearCloudFunctionInvokes()

	project := "noctaxris-gcp-local"
	loc := "us-central1"
	topic := "projects/" + project + "/topics/ea-t"
	if _, created, err := st.CreateTopic(topic, project); err != nil || !created {
		t.Fatal(err)
	}
	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/ea-unit"
	trig, created, err := st.CreateEventarcTrigger(store.EventarcTrigger{
		ProjectID: project, Location: loc, TriggerID: "ea1",
		FiltersJSON:     `[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}]`,
		DestinationJSON: `{"httpEndpoint":{"uri":"` + catcher + `"}}`,
		TransportJSON:   `{"pubsub":{"topic":"` + topic + `"}}`,
	})
	if err != nil || !created {
		t.Fatalf("trigger: %v %v", created, err)
	}
	list, err := st.ListEventarcTriggers(project, loc)
	if err != nil || len(list) != 1 {
		t.Fatalf("list triggers: %v", list)
	}
	ch, created, err := st.CreateEventarcChannel(store.EventarcChannel{
		ProjectID: project, Location: loc, ChannelID: "ch1", Provider: "custom", PubsubTopic: topic,
	})
	if err != nil || !created {
		t.Fatalf("channel: %v %v", created, err)
	}
	gotCh, ok, err := st.GetEventarcChannel(ch.Name)
	if err != nil || !ok || gotCh.ChannelID != "ch1" {
		t.Fatal(err)
	}
	chs, err := st.ListEventarcChannels(project, loc)
	if err != nil || len(chs) != 1 {
		t.Fatalf("list ch: %v", chs)
	}

	st.DeliverEventarcForPubSub(topic, []byte(`{"hello":"world"}`), map[string]string{"k": "v"})
	caught := store.ListHTTPCatcher()
	if len(caught) < 1 {
		t.Fatalf("expected catcher delivery, got %v (trigger=%s)", caught, trig.Name)
	}

	fnName := "projects/" + project + "/locations/" + loc + "/functions/fn1"
	if createdFN, err := st.CreateCloudFunction(store.CloudFunction{
		Name: fnName, ProjectID: project, Location: loc, FunctionID: "fn1", State: "ACTIVE",
	}); err != nil || !createdFN {
		t.Fatalf("create fn created=%v err=%v", createdFN, err)
	}
	trig2, created, err := st.CreateEventarcTrigger(store.EventarcTrigger{
		ProjectID: project, Location: loc, TriggerID: "ea-fn",
		FiltersJSON:     `[{"attribute":"type","value":"google.cloud.storage.object.v1.finalized"}]`,
		DestinationJSON: `{"cloudFunction":"` + fnName + `"}`,
	})
	if err != nil || !created {
		t.Fatalf("fn trigger: %v %v", created, err)
	}
	_ = trig2
	st.DeliverEventarcForGCSFinalize("bkt", "obj.txt", 1, 3, "text/plain")
	invokes := store.ListCloudFunctionInvokes()
	foundInvoke := false
	for _, inv := range invokes {
		if inv.Function == fnName {
			foundInvoke = true
			break
		}
	}
	if !foundInvoke {
		t.Fatalf("expected function invoke, got %v", invokes)
	}

	ok, err = st.DeleteEventarcTrigger(trig.Name)
	if err != nil || !ok {
		t.Fatal(err)
	}
	ok, err = st.DeleteEventarcChannel(ch.Name)
	if err != nil || !ok {
		t.Fatal(err)
	}

	docPath := "projects/p/databases/(default)/documents/c/d1"
	if err := st.PutFirestoreDoc(store.FirestoreDoc{Path: docPath, ProjectID: "p", CollectionID: "c", DocumentID: "d1", FieldsJSON: `{"a":1}`}); err != nil {
		t.Fatal(err)
	}
	doc, ok, err := st.GetFirestoreDoc(docPath)
	if err != nil || !ok || doc.DocumentID != "d1" {
		t.Fatal(err)
	}
	if err := st.ApplyFirestoreWritesAtomic([]store.FirestoreDoc{{
		Path: "projects/p/databases/(default)/documents/c/d2", ProjectID: "p", CollectionID: "c", DocumentID: "d2", FieldsJSON: `{"b":2}`,
	}}, []string{}); err != nil {
		t.Fatal(err)
	}
	docs, err := st.ListFirestoreDocs("p", "projects/p/databases/(default)/documents", "c", 10)
	if err != nil || len(docs) < 2 {
		t.Fatalf("list docs: %v", docs)
	}
	byCol, err := st.ListFirestoreDocsByCollection("p", "c", 10)
	if err != nil || len(byCol) < 2 {
		t.Fatalf("by col: %v", byCol)
	}
	batch, err := st.BatchGetFirestoreDocs([]string{docPath})
	if err != nil || len(batch) != 1 {
		t.Fatalf("batch: %v", batch)
	}
	if err := st.PutFirestoreTransaction("tok1", "(default)", "p"); err != nil {
		t.Fatal(err)
	}
	ok, err = st.ConsumeFirestoreTransaction("tok1", "(default)")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := st.PutFirestoreTransaction("tok2", "(default)", "p"); err != nil {
		t.Fatal(err)
	}
	ok, err = st.DeleteFirestoreTransaction("tok2")
	if err != nil || !ok {
		t.Fatal(err)
	}
	ok, err = st.DeleteFirestoreDoc(docPath)
	if err != nil || !ok {
		t.Fatal(err)
	}

	krName := "projects/p/locations/global/keyRings/lab"
	ok, err = st.CreateKMSKeyRing(store.KMSKeyRing{Name: krName, ProjectID: "p", Location: "global"})
	if err != nil || !ok {
		t.Fatal(err)
	}
	kr, ok, err := st.GetKMSKeyRing(krName)
	if err != nil || !ok || kr.Name != krName {
		t.Fatal(err)
	}
	rings, err := st.ListKMSKeyRings("p", "global")
	if err != nil || len(rings) < 1 {
		t.Fatalf("rings: %v", rings)
	}
	keyName := krName + "/cryptoKeys/k1"
	verName := keyName + "/cryptoKeyVersions/1"
	plain := []byte("0123456789abcdef0123456789abcdef")
	sealed, err := st.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	ok, err = st.CreateKMSCryptoKey(store.KMSCryptoKey{
		Name: keyName, KeyRing: krName, Purpose: store.KMSPurposeEncrypt,
	}, store.KMSKeyVersion{Name: verName, CryptoKey: keyName, VersionID: "1", State: store.KMSStateEnabled, KeyMaterialCiphertext: sealed})
	if err != nil || !ok {
		t.Fatal(err)
	}
	keys, err := st.ListKMSCryptoKeys(krName)
	if err != nil || len(keys) < 1 {
		t.Fatalf("keys: %v", keys)
	}
	vers, err := st.ListKMSKeyVersions(keyName)
	if err != nil || len(vers) < 1 {
		t.Fatalf("versions: %v", vers)
	}
	destroyed, ok, err := st.DestroyKMSKeyVersion(verName)
	if err != nil || !ok || destroyed.State == "" {
		t.Fatal(err)
	}
	restored, ok, err := st.RestoreKMSKeyVersion(verName)
	if err != nil || !ok || restored.State == "" {
		t.Fatal(err)
	}

	store.RecordHTTPCatcher("x")
	_ = store.ListHTTPCatcher()
	store.ClearHTTPCatcher()
	store.RecordCloudFunctionInvoke(fnName, "body")
	_ = store.ListCloudFunctionInvokes()
	store.ClearCloudFunctionInvokes()
}

func TestStoreArtifactCloudBuildDeep(t *testing.T) {
	st := openTestStore(t)
	repoName := "projects/p/locations/us-central1/repositories/r1"
	ok, err := st.CreateArRepository(store.ArRepository{
		Name: repoName, ProjectID: "p", Location: "us-central1", RepositoryID: "r1", Format: "DOCKER",
	})
	if err != nil || !ok {
		t.Fatal(err)
	}
	updated, ok, err := st.UpdateArRepository(repoName, "desc", `{"env":"lab"}`)
	if err != nil || !ok || updated.Description != "desc" {
		t.Fatalf("update %#v", updated)
	}
	pkgName := repoName + "/packages/pkg"
	ok, err = st.CreateArPackage(store.ArPackage{Name: pkgName, RepositoryName: repoName, PackageID: "pkg"})
	if err != nil || !ok {
		t.Fatal(err)
	}
	pkg, ok, err := st.GetArPackage(pkgName)
	if err != nil || !ok || pkg.PackageID != "pkg" {
		t.Fatal(err)
	}
	verName := pkgName + "/versions/1.0.0"
	ok, err = st.CreateArVersion(store.ArVersion{Name: verName, PackageName: pkgName, VersionID: "1.0.0"})
	if err != nil || !ok {
		t.Fatal(err)
	}
	ver, ok, err := st.GetArVersion(verName)
	if err != nil || !ok || ver.VersionID != "1.0.0" {
		t.Fatal(err)
	}
	buildName := "projects/p/locations/global/builds/b1"
	ok, err = st.CreateCbBuild(store.CbBuild{
		Name: buildName, ProjectID: "p", Location: "global", BuildID: "b1", Status: "WORKING", BuildJSON: `{}`,
	})
	if err != nil || !ok {
		t.Fatal(err)
	}
	b, ok, err := st.GetCbBuildByID("p", "b1")
	if err != nil || !ok || b.BuildID != "b1" {
		t.Fatal(err)
	}
	builds, err := st.ListCbBuilds("p", "global")
	if err != nil || len(builds) < 1 {
		t.Fatalf("builds: %v", builds)
	}
	adv, ok, err := st.AdvanceCbBuildToSuccess(buildName)
	if err != nil || !ok || adv.Status != "SUCCESS" {
		t.Fatalf("advance %#v", adv)
	}
	trigName := "projects/p/locations/us-central1/triggers/t1"
	ok, err = st.CreateCbTrigger(store.CbTrigger{
		Name: trigName, ProjectID: "p", Location: "us-central1", TriggerID: "t1", TriggerJSON: `{}`,
	})
	if err != nil || !ok {
		t.Fatal(err)
	}
	tr, ok, err := st.GetCbTrigger(trigName)
	if err != nil || !ok || tr.TriggerID != "t1" {
		t.Fatal(err)
	}
	id := store.NewCbTriggerID()
	if id == "" {
		t.Fatal("NewCbTriggerID empty")
	}
}
