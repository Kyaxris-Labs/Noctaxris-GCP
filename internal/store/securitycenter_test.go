package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestSCCSourcesAndFindingsStore(t *testing.T) {
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

	parent := store.DefaultOrganizationName
	srcName := store.SCCSourceResourceName(parent, "src1")
	created, err := st.CreateSCCSource(store.SCCSource{
		Name: srcName, Parent: parent, SourceID: "src1", DisplayName: "S1",
	})
	if err != nil || !created {
		t.Fatalf("create source created=%v err=%v", created, err)
	}
	got, ok, err := st.GetSCCSource(srcName)
	if err != nil || !ok || got.DisplayName != "S1" {
		t.Fatalf("get source ok=%v got=%#v err=%v", ok, got, err)
	}
	list, err := st.ListSCCSources(parent)
	if err != nil || len(list) != 1 {
		t.Fatalf("list sources=%v err=%v", list, err)
	}

	fName := store.SCCFindingResourceName(srcName, "f1")
	created, err = st.CreateSCCFinding(store.SCCFinding{
		Name: fName, Parent: parent, SourceName: srcName, FindingID: "f1",
		Category: "XSS", Severity: "MEDIUM", State: "ACTIVE",
	})
	if err != nil || !created {
		t.Fatalf("create finding created=%v err=%v", created, err)
	}
	f, ok, err := st.UpdateSCCFindingState(fName, "INACTIVE")
	if err != nil || !ok || f.State != "INACTIVE" {
		t.Fatalf("set state ok=%v f=%#v err=%v", ok, f, err)
	}
	findings, err := st.ListSCCFindings(srcName)
	if err != nil || len(findings) != 1 {
		t.Fatalf("list findings=%v err=%v", findings, err)
	}
	ok, err = st.DeleteSCCSource(srcName)
	if err != nil || !ok {
		t.Fatalf("delete source ok=%v err=%v", ok, err)
	}
	_, ok, err = st.GetSCCFinding(fName)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("finding should cascade-delete with source")
	}
}
