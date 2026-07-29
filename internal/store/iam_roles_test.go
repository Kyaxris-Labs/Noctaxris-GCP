package store_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCustomRoleCRUD(t *testing.T) {
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

	const project = "noctaxris-gcp-local"
	created, err := st.CreateCustomRole(project, "bucketLister", "Bucket Lister", "list buckets", "GA",
		[]string{"storage.buckets.list"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "projects/"+project+"/roles/bucketLister" {
		t.Fatalf("name=%s", created.Name)
	}
	if _, err := st.CreateCustomRole(project, "bucketLister", "", "", "", nil); !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("duplicate: %v", err)
	}

	got, ok, err := st.GetCustomRole(created.Name)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if len(got.IncludedPermissions) != 1 || got.IncludedPermissions[0] != "storage.buckets.list" {
		t.Fatalf("perms=%v", got.IncludedPermissions)
	}

	perms, ok, err := st.GetRoleIncludedPermissions(created.Name)
	if err != nil || !ok || len(perms) != 1 {
		t.Fatalf("role store: ok=%v perms=%v err=%v", ok, perms, err)
	}

	updated, ok, err := st.UpdateCustomRole(created.Name, "Lister", "", "BETA",
		[]string{"storage.buckets.list", "storage.objects.get"},
		true, false, true, true)
	if err != nil || !ok {
		t.Fatalf("update: ok=%v err=%v", ok, err)
	}
	if updated.Title != "Lister" || updated.Stage != "BETA" || len(updated.IncludedPermissions) != 2 {
		t.Fatalf("updated=%+v", updated)
	}

	list, err := st.ListCustomRoles(project, false)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: n=%d err=%v", len(list), err)
	}

	deleted, ok, err := st.DeleteCustomRole(created.Name)
	if err != nil || !ok || !deleted.Deleted {
		t.Fatalf("delete: ok=%v deleted=%+v err=%v", ok, deleted, err)
	}
	_, ok, err = st.GetRoleIncludedPermissions(created.Name)
	if err != nil || ok {
		t.Fatalf("deleted role must not grant: ok=%v err=%v", ok, err)
	}
	list, err = st.ListCustomRoles(project, false)
	if err != nil || len(list) != 0 {
		t.Fatalf("active list after delete: n=%d err=%v", len(list), err)
	}
	list, err = st.ListCustomRoles(project, true)
	if err != nil || len(list) != 1 {
		t.Fatalf("showDeleted list: n=%d err=%v", len(list), err)
	}
}
