package store_test

import (
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestRunRevisionGetListDeleteAndInvoke(t *testing.T) {
	st := openTestStore(t)
	const (
		project  = "p"
		location = "us-central1"
		svcID    = "web"
	)
	svcName := "projects/" + project + "/locations/" + location + "/services/" + svcID
	created, err := st.CreateRunService(store.RunService{
		Name: svcName, ProjectID: project, Location: location, ServiceID: svcID,
		TemplateJSON: `{"containers":[{"image":"gcr.io/demo/app:1"}]}`,
	})
	if err != nil || !created {
		t.Fatalf("create service created=%v err=%v", created, err)
	}

	revs, err := st.ListRunRevisions(svcName)
	if err != nil || len(revs) != 1 {
		t.Fatalf("list by service=%#v err=%v", revs, err)
	}
	revName := revs[0].Name

	got, ok, err := st.GetRunRevision(revName)
	if err != nil || !ok || got.Name != revName {
		t.Fatalf("get ok=%v err=%v got=%#v", ok, err, got)
	}
	_, ok, err = st.GetRunRevision("projects/p/locations/us-central1/services/web/revisions/missing")
	if err != nil || ok {
		t.Fatalf("missing get ok=%v err=%v", ok, err)
	}

	short := revName
	if i := strings.LastIndex(revName, "/"); i >= 0 {
		short = revName[i+1:]
	}
	byShort, ok, err := st.GetRunRevisionByShortName(project, location, short)
	if err != nil || !ok {
		t.Fatalf("by short ok=%v err=%v short=%q name=%q", ok, err, short, got.Name)
	}
	if byShort.ServiceName != svcName {
		t.Fatalf("byShort=%#v", byShort)
	}
	_, ok, err = st.GetRunRevisionByShortName(project, location, "")
	if err != nil || ok {
		t.Fatalf("empty short ok=%v err=%v", ok, err)
	}
	_, ok, err = st.GetRunRevisionByShortName(project, location, "no-such-rev")
	if err != nil || ok {
		t.Fatalf("missing short ok=%v err=%v", ok, err)
	}

	all, err := st.ListRunRevisionsByProject(project, location, "")
	if err != nil || len(all) < 1 {
		t.Fatalf("list all=%#v err=%v", all, err)
	}
	filtered, err := st.ListRunRevisionsByProject(project, location, svcID)
	if err != nil || len(filtered) != 1 {
		t.Fatalf("list filtered=%#v err=%v", filtered, err)
	}
	none, err := st.ListRunRevisionsByProject(project, location, "other")
	if err != nil || len(none) != 0 {
		t.Fatalf("list other=%#v err=%v", none, err)
	}

	if err := st.RecordRunInvoke(svcName, `{"status":200}`); err != nil {
		t.Fatal(err)
	}

	deleted, err := st.DeleteRunRevision(revName)
	if err != nil || !deleted {
		t.Fatalf("delete deleted=%v err=%v", deleted, err)
	}
	deleted, err = st.DeleteRunRevision(revName)
	if err != nil || deleted {
		t.Fatalf("second delete deleted=%v err=%v", deleted, err)
	}
}
