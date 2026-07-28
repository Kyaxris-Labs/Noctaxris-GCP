package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func openStage2Store(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestCRMOrganizationSeeded(t *testing.T) {
	st := openStage2Store(t)
	o, ok, err := st.GetOrganization(store.DefaultOrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || o.Name != store.DefaultOrganizationName {
		t.Fatalf("org = %#v ok=%v", o, ok)
	}
	if o.State != "ACTIVE" {
		t.Fatalf("state = %s", o.State)
	}
}

func TestCRMFolderCRUD(t *testing.T) {
	st := openStage2Store(t)
	f, created, err := st.CreateFolder(store.Folder{
		Parent:      store.DefaultOrganizationName,
		DisplayName: "Lab Folder",
	})
	if err != nil || !created {
		t.Fatalf("create folder created=%v err=%v", created, err)
	}
	if f.Name == "" || f.Parent != store.DefaultOrganizationName {
		t.Fatalf("folder = %#v", f)
	}

	got, ok, err := st.GetFolder(f.FolderID)
	if err != nil || !ok || got.DisplayName != "Lab Folder" {
		t.Fatalf("get = %#v ok=%v err=%v", got, ok, err)
	}

	list, err := st.ListFolders(store.DefaultOrganizationName, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}

	patched, ok, err := st.UpdateFolderDisplayName(f.FolderID, "Renamed")
	if err != nil || !ok || patched.DisplayName != "Renamed" {
		t.Fatalf("patch = %#v ok=%v err=%v", patched, ok, err)
	}

	deleted, ok, err := st.DeleteFolder(f.FolderID)
	if err != nil || !ok || deleted.State != "DELETE_REQUESTED" {
		t.Fatalf("delete = %#v ok=%v err=%v", deleted, ok, err)
	}

	active, err := st.ListFolders(store.DefaultOrganizationName, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active after delete = %#v", active)
	}
	withDeleted, err := st.ListFolders(store.DefaultOrganizationName, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(withDeleted) != 1 {
		t.Fatalf("showDeleted = %#v", withDeleted)
	}
}

func TestAppEngineAppServiceVersionStore(t *testing.T) {
	st := openStage2Store(t)
	created, err := st.CreateAppEngineApp(store.AppEngineApp{
		AppID:      "noctaxris-gcp-local",
		LocationID: "us-central",
	})
	if err != nil || !created {
		t.Fatalf("create app created=%v err=%v", created, err)
	}

	created, err = st.CreateAppEngineVersion(store.AppEngineVersion{
		AppID:            "noctaxris-gcp-local",
		ServiceID:        "default",
		VersionID:        "v1",
		Runtime:          "python311",
		EnvVariablesJSON: `{"GREETING":"hi"}`,
	})
	if err != nil || !created {
		t.Fatalf("create version created=%v err=%v", created, err)
	}

	svc, ok, err := st.GetAppEngineService("noctaxris-gcp-local", "default")
	if err != nil || !ok || svc.ServiceID != "default" {
		t.Fatalf("service = %#v ok=%v err=%v", svc, ok, err)
	}

	v, ok, err := st.GetAppEngineVersion("noctaxris-gcp-local", "default", "v1")
	if err != nil || !ok || v.Runtime != "python311" {
		t.Fatalf("version = %#v ok=%v err=%v", v, ok, err)
	}

	vers, err := st.ListAppEngineVersions("noctaxris-gcp-local", "default")
	if err != nil || len(vers) != 1 {
		t.Fatalf("list versions = %#v err=%v", vers, err)
	}

	okDel, err := st.DeleteAppEngineVersion("noctaxris-gcp-local", "default", "v1")
	if err != nil || !okDel {
		t.Fatalf("delete version ok=%v err=%v", okDel, err)
	}
}
