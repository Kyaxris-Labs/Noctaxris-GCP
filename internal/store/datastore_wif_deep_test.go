package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestDatastoreEntityAndTxDeep(t *testing.T) {
	st := openTestStore(t)
	project := "noctaxris-gcp-local"
	if err := st.PutDatastoreEntity(store.DatastoreEntity{
		ProjectID: project, Namespace: "", Kind: "Task", KeyPath: "Task/name:t1", KeyName: "t1",
		PropertiesJSON: `{"title":"\"hello\"","done":"false"}`,
	}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetDatastoreEntity(project, "", "Task/name:t1")
	if err != nil || !ok || got.KeyName != "t1" {
		t.Fatalf("%#v ok=%v err=%v", got, ok, err)
	}
	id, err := st.NextDatastoreID(project, "", "Task")
	if err != nil || id < 1 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	if err := st.PutDatastoreEntity(store.DatastoreEntity{
		ProjectID: project, Kind: "Task", KeyPath: "Task/id:" + "2", KeyID: 2,
		PropertiesJSON: `{"title":"\"n2\"","done":"true"}`,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.QueryDatastoreEntities(store.QueryDatastoreEntitiesFilter{
		ProjectID: project, Kind: "Task", PropEquals: map[string]string{"done": "true"},
	})
	if err != nil || len(rows) < 1 {
		t.Fatalf("query=%v err=%v", rows, err)
	}
	if err := st.PutDatastoreTransaction("dtx1", project, ""); err != nil {
		t.Fatal(err)
	}
	ok, err = st.ConsumeDatastoreTransaction("dtx1", project)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := st.PutDatastoreTransaction("dtx2", project, ""); err != nil {
		t.Fatal(err)
	}
	ok, err = st.DeleteDatastoreTransaction("dtx2")
	if err != nil || !ok {
		t.Fatal(err)
	}
	ok, err = st.DeleteDatastoreEntity(project, "", "Task/name:t1")
	if err != nil || !ok {
		t.Fatal(err)
	}
}

func TestWIFUpdateAndListDeleted(t *testing.T) {
	st := openTestStore(t)
	project := "noctaxris-gcp-local"
	pool, err := st.CreateWIFPool(project, "global", "pool-a", "Pool A", "desc", false)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetWIFPool(pool.Name)
	if err != nil || !ok || got.PoolID != "pool-a" {
		t.Fatal(err)
	}
	list, err := st.ListWIFPools(project, "global", false)
	if err != nil || len(list) < 1 {
		t.Fatalf("list=%v", list)
	}
	prov, err := st.CreateWIFProvider(pool.Name, "oidc1", "OIDC", "d", "https://example.com", `{"google.subject":"assertion.sub"}`, `["aud"]`, false)
	if err != nil {
		t.Fatal(err)
	}
	gotP, ok, err := st.GetWIFProvider(prov.Name)
	if err != nil || !ok || gotP.ProviderID != "oidc1" {
		t.Fatal(err)
	}
	provs, err := st.ListWIFProviders(pool.Name, false)
	if err != nil || len(provs) < 1 {
		t.Fatalf("provs=%v", provs)
	}
	updated, ok, err := st.UpdateWIFProvider(prov.Name, "OIDC2", "d2", "https://example.com/2", `{"google.subject":"assertion.sub"}`, `["aud2"]`, false,
		true, true, true, true, true, false)
	if err != nil || !ok || updated.DisplayName != "OIDC2" {
		t.Fatalf("update %#v ok=%v err=%v", updated, ok, err)
	}
	_, ok, err = st.DeleteWIFProvider(prov.Name)
	if err != nil || !ok {
		t.Fatal(err)
	}
	deletedList, err := st.ListWIFProviders(pool.Name, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = deletedList
	_, ok, err = st.DeleteWIFPool(pool.Name)
	if err != nil || !ok {
		t.Fatal(err)
	}
	poolsDel, err := st.ListWIFPools(project, "global", true)
	if err != nil || len(poolsDel) < 1 {
		t.Fatalf("deleted pools=%v", poolsDel)
	}
}
