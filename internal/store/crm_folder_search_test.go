package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCRMOrganizationsFoldersSearchAndMove(t *testing.T) {
	st := openTestStore(t)

	created, err := st.CreateOrganization(store.Organization{
		OrgID: "org-lab", DisplayName: "Lab Org",
	})
	if err != nil || !created {
		t.Fatalf("create org created=%v err=%v", created, err)
	}
	orgName := "organizations/org-lab"
	got, ok, err := st.GetOrganization(orgName)
	if err != nil || !ok || got.DisplayName != "Lab Org" {
		t.Fatalf("get org ok=%v err=%v got=%#v", ok, err, got)
	}
	orgs, err := st.ListOrganizations()
	if err != nil || len(orgs) < 1 {
		t.Fatalf("list orgs=%#v err=%v", orgs, err)
	}
	parent, found, err := st.CRMParent("projects/noctaxris-gcp-local")
	if err != nil {
		t.Fatal(err)
	}
	if !found && parent == "" {
		// Seeded projects may or may not expose a CRM parent depending on EnsureRoot.
		t.Log("CRMParent returned empty for project (acceptable without seed)")
	}

	f1, created, err := st.CreateFolder(store.Folder{
		FolderID: "folder-a", Parent: orgName, DisplayName: "Alpha Ops",
	})
	if err != nil || !created {
		t.Fatalf("create folder created=%v err=%v", created, err)
	}
	f2, created, err := st.CreateFolder(store.Folder{
		FolderID: "folder-b", Parent: orgName, DisplayName: "Beta Lab",
	})
	if err != nil || !created {
		t.Fatalf("create folder b created=%v err=%v", created, err)
	}

	active, err := st.SearchFolders("")
	if err != nil || len(active) < 2 {
		t.Fatalf("empty search=%#v err=%v", active, err)
	}
	byName, err := st.SearchFolders("Alpha")
	if err != nil || len(byName) != 1 || byName[0].FolderID != "folder-a" {
		t.Fatalf("substring search=%#v err=%v", byName, err)
	}
	byDisplay, err := st.SearchFolders(`displayName=Alpha`)
	if err != nil || len(byDisplay) != 1 {
		t.Fatalf("displayName search=%#v err=%v", byDisplay, err)
	}
	byParent, err := st.SearchFolders("parent=" + orgName)
	if err != nil || len(byParent) < 2 {
		t.Fatalf("parent search=%#v err=%v", byParent, err)
	}
	byState, err := st.SearchFolders("state=ACTIVE")
	if err != nil || len(byState) < 2 {
		t.Fatalf("state search=%#v err=%v", byState, err)
	}
	byLife, err := st.SearchFolders("lifecycleState=ACTIVE and displayName=Beta")
	if err != nil || len(byLife) != 1 || byLife[0].FolderID != "folder-b" {
		t.Fatalf("lifecycle search=%#v err=%v", byLife, err)
	}

	renamed, ok, err := st.UpdateFolderDisplayName(f1.Name, "Alpha Renamed")
	if err != nil || !ok || renamed.DisplayName != "Alpha Renamed" {
		t.Fatalf("rename ok=%v err=%v got=%#v", ok, err, renamed)
	}
	moved, ok, err := st.MoveFolder(f2.Name, f1.Name)
	if err != nil || !ok || moved.Parent != f1.Name {
		t.Fatalf("move ok=%v err=%v got=%#v", ok, err, moved)
	}
	deleted, ok, err := st.DeleteFolder(f2.Name)
	if err != nil || !ok || deleted.State == "ACTIVE" {
		t.Fatalf("delete ok=%v err=%v got=%#v", ok, err, deleted)
	}
	restored, ok, err := st.UndeleteFolder(f2.Name)
	if err != nil || !ok || restored.State != "ACTIVE" {
		t.Fatalf("undelete ok=%v err=%v got=%#v", ok, err, restored)
	}
}
