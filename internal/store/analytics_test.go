package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestAnalyticsBQAndFirebase(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	d, created, err := st.CreateBQDataset(store.BQDataset{ProjectID: "p", DatasetID: "ds"})
	if err != nil || !created || d == nil {
		t.Fatalf("dataset: created=%v err=%v", created, err)
	}
	tbl, created, err := st.CreateBQTable(store.BQTable{ProjectID: "p", DatasetID: "ds", TableID: "t", SchemaJSON: `[{"name":"n","type":"STRING"}]`})
	if err != nil || !created || tbl == nil {
		t.Fatalf("table: created=%v err=%v", created, err)
	}
	if err := st.InsertBQRows("p", "ds", "t", []map[string]any{{"n": "x"}}, []string{"i1"}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListBQRows("p", "ds", "t")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}

	u, created, err := st.CreateFirebaseUser(store.FirebaseUser{
		ProjectID: "p", Email: "a@b.c", PasswordHash: "hash",
	})
	if err != nil || !created {
		t.Fatalf("firebase create: %v %v", created, err)
	}
	got, ok, err := st.GetFirebaseUserByEmail("p", "a@b.c")
	if err != nil || !ok || got.LocalID != u.LocalID {
		t.Fatalf("get email: %#v ok=%v err=%v", got, ok, err)
	}
}

func TestAnalyticsMonitoringDatastoreEventarc(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	md, created, err := st.CreateMetricDescriptor(store.MetricDescriptorRow{
		ProjectID: "p", Type: "custom.googleapis.com/x", MetricKind: "GAUGE", ValueType: "DOUBLE",
	})
	if err != nil || !created {
		t.Fatalf("descriptor: %v %v", created, err)
	}
	_ = md
	if err := st.CreateTimeSeriesPoints([]store.TimeSeriesPoint{{
		ProjectID: "p", MetricType: "custom.googleapis.com/x", ValueJSON: `{"doubleValue":1}`, EndTime: "2026-01-01T00:00:00Z",
	}}); err != nil {
		t.Fatal(err)
	}
	n, err := st.DeleteTimeSeriesPoints("p", "custom.googleapis.com/x")
	if err != nil || n != 1 {
		t.Fatalf("delete ts n=%d err=%v", n, err)
	}
	pol, created, err := st.CreateAlertPolicy(store.AlertPolicyRow{
		ProjectID: "p", PolicyID: "ap1", DisplayName: "d", Enabled: true, ConditionsJSON: `[]`,
	})
	if err != nil || !created {
		t.Fatalf("alert create: %v %v", created, err)
	}
	pol.DisplayName = "d2"
	updated, ok, err := st.UpdateAlertPolicy(*pol)
	if err != nil || !ok || updated.DisplayName != "d2" {
		t.Fatalf("alert update: %#v ok=%v err=%v", updated, ok, err)
	}
	ok, err = st.DeleteAlertPolicy(pol.Name)
	if err != nil || !ok {
		t.Fatalf("alert delete ok=%v err=%v", ok, err)
	}

	if err := st.PutDatastoreEntity(store.DatastoreEntity{
		ProjectID: "p", Kind: "K", KeyPath: "K/name:a", KeyName: "a", PropertiesJSON: `{"n":"v"}`,
	}); err != nil {
		t.Fatal(err)
	}
	ents, err := st.QueryDatastoreEntities(store.QueryDatastoreEntitiesFilter{
		ProjectID: "p", Kind: "K", PropEquals: map[string]string{"n": `"v"`},
	})
	if err != nil || len(ents) != 1 {
		t.Fatalf("query: %v err=%v", ents, err)
	}

	tr, created, err := st.CreateEventarcTrigger(store.EventarcTrigger{
		ProjectID: "p", Location: "us-central1", TriggerID: "t1",
		FiltersJSON: `[{"attribute":"type","value":"google.cloud.storage.object.v1.finalized"},{"attribute":"bucket","value":"b"}]`,
		DestinationJSON: `{"httpEndpoint":{"uri":"http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/nope"}}`,
	})
	if err != nil || !created {
		t.Fatalf("trigger: %v %v", created, err)
	}
	got, ok, err := st.GetEventarcTrigger(tr.Name)
	if err != nil || !ok || got.TriggerID != "t1" {
		t.Fatalf("get trigger: %#v", got)
	}
}
