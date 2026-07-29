package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCloudSQLInstanceStore(t *testing.T) {
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

	name := store.CloudSQLInstanceResourceName("p1", "inst1")
	created, err := st.CreateCloudSQLInstance(store.CloudSQLInstance{
		Name: name, ProjectID: "p1", InstanceID: "inst1", Region: "us-central1",
		DatabaseVersion: "POSTGRES_16",
	})
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	inst, ok, err := st.GetCloudSQLInstance(name)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if inst.Port != 5432 || inst.State != "RUNNABLE" {
		t.Fatalf("inst=%+v", inst)
	}
	list, err := st.ListCloudSQLInstances("p1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%d err=%v", len(list), err)
	}
	del, cid, err := st.DeleteCloudSQLInstance(name)
	if err != nil || !del || cid != "" {
		t.Fatalf("delete: del=%v cid=%q err=%v", del, cid, err)
	}
}

func TestCloudSQLUserAndDatabaseStore(t *testing.T) {
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

	name := store.CloudSQLInstanceResourceName("p1", "inst1")
	if _, err := st.CreateCloudSQLInstance(store.CloudSQLInstance{
		Name: name, ProjectID: "p1", InstanceID: "inst1", Region: "us-central1",
		DatabaseVersion: "POSTGRES_16",
	}); err != nil {
		t.Fatal(err)
	}

	created, err := st.CreateCloudSQLUser(store.CloudSQLUser{
		InstanceName: name, ProjectID: "p1", InstanceID: "inst1",
		Name: "app", Host: "", Password: "secret", Type: "BUILT_IN",
	})
	if err != nil || !created {
		t.Fatalf("create user: created=%v err=%v", created, err)
	}
	u, ok, err := st.GetCloudSQLUser(name, "app", "")
	if err != nil || !ok || u.Name != "app" {
		t.Fatalf("get user: ok=%v u=%+v err=%v", ok, u, err)
	}
	users, err := st.ListCloudSQLUsers(name)
	if err != nil || len(users) != 1 {
		t.Fatalf("list users=%d err=%v", len(users), err)
	}

	created, err = st.CreateCloudSQLDatabase(store.CloudSQLDatabase{
		InstanceName: name, ProjectID: "p1", InstanceID: "inst1",
		Name: "appdb", Charset: "UTF8", Collation: "en_US.UTF8",
	})
	if err != nil || !created {
		t.Fatalf("create database: created=%v err=%v", created, err)
	}
	d, ok, err := st.GetCloudSQLDatabase(name, "appdb")
	if err != nil || !ok || d.Charset != "UTF8" {
		t.Fatalf("get database: ok=%v d=%+v err=%v", ok, d, err)
	}
	dbs, err := st.ListCloudSQLDatabases(name)
	if err != nil || len(dbs) != 1 {
		t.Fatalf("list databases=%d err=%v", len(dbs), err)
	}

	if del, err := st.DeleteCloudSQLUser(name, "app", ""); err != nil || !del {
		t.Fatalf("delete user: del=%v err=%v", del, err)
	}
	if del, err := st.DeleteCloudSQLDatabase(name, "appdb"); err != nil || !del {
		t.Fatalf("delete database: del=%v err=%v", del, err)
	}

	// Recreate and ensure instance delete cascades.
	_, _ = st.CreateCloudSQLUser(store.CloudSQLUser{
		InstanceName: name, ProjectID: "p1", InstanceID: "inst1", Name: "casc",
	})
	_, _ = st.CreateCloudSQLDatabase(store.CloudSQLDatabase{
		InstanceName: name, ProjectID: "p1", InstanceID: "inst1", Name: "cascdb",
	})
	if _, _, err := st.DeleteCloudSQLInstance(name); err != nil {
		t.Fatal(err)
	}
	users, err = st.ListCloudSQLUsers(name)
	if err != nil || len(users) != 0 {
		t.Fatalf("cascade users=%d err=%v", len(users), err)
	}
	dbs, err = st.ListCloudSQLDatabases(name)
	if err != nil || len(dbs) != 0 {
		t.Fatalf("cascade databases=%d err=%v", len(dbs), err)
	}
}
