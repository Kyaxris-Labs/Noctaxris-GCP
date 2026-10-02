package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestLoggingRoutingStoreCRUD(t *testing.T) {
	st := openTestStore(t)
	const project = "noctaxris-gcp-local"

	if err := st.EnsureDefaultLogRouting(""); err == nil {
		t.Fatal("expected empty project error")
	}
	if err := st.EnsureDefaultLogRouting(project); err != nil {
		t.Fatal(err)
	}
	// Idempotent seed.
	if err := st.EnsureDefaultLogRouting(project); err != nil {
		t.Fatal(err)
	}

	buckets, err := st.ListLogBuckets(project, "global")
	if err != nil || len(buckets) < 2 {
		t.Fatalf("buckets=%#v err=%v", buckets, err)
	}
	allLoc, err := st.ListLogBuckets(project, "-")
	if err != nil || len(allLoc) < 2 {
		t.Fatalf("all loc buckets=%#v err=%v", allLoc, err)
	}
	reqName := "projects/" + project + "/locations/global/buckets/_Required"
	b, ok, err := st.GetLogBucket(reqName)
	if err != nil || !ok || b.BucketID != "_Required" {
		t.Fatalf("get bucket ok=%v err=%v got=%#v", ok, err, b)
	}
	_, ok, err = st.GetLogBucket(reqName + "-missing")
	if err != nil || ok {
		t.Fatalf("missing bucket ok=%v err=%v", ok, err)
	}

	ex, created, err := st.CreateLogExclusion(store.LogExclusion{
		ProjectID: project, ExclusionID: "skip-noise", Filter: "severity<ERROR", Disabled: true,
	})
	if err != nil || !created || ex == nil || !ex.Disabled {
		t.Fatalf("create exclusion created=%v err=%v got=%#v", created, err, ex)
	}
	_, created, err = st.CreateLogExclusion(store.LogExclusion{
		ProjectID: project, ExclusionID: "skip-noise", Filter: "severity<ERROR",
	})
	if err != nil || created {
		t.Fatalf("dup exclusion created=%v err=%v", created, err)
	}
	_, _, err = st.CreateLogExclusion(store.LogExclusion{ProjectID: project})
	if err == nil {
		t.Fatal("expected validation error")
	}

	got, ok, err := st.GetLogExclusion(ex.Name)
	if err != nil || !ok || got.ExclusionID != "skip-noise" {
		t.Fatalf("get exclusion ok=%v err=%v got=%#v", ok, err, got)
	}
	_, ok, err = st.GetLogExclusion(ex.Name + "-x")
	if err != nil || ok {
		t.Fatalf("missing exclusion ok=%v err=%v", ok, err)
	}
	list, err := st.ListLogExclusions(project)
	if err != nil || len(list) < 2 {
		t.Fatalf("list exclusions=%#v err=%v", list, err)
	}

	viewParent := "projects/" + project + "/locations/global/buckets/_Default"
	v, created, err := st.CreateLogView(store.LogView{
		ProjectID: project, Location: "global", BucketID: "_Default", ViewID: "errors",
		Filter: "severity>=ERROR",
	})
	if err != nil || !created || v == nil {
		t.Fatalf("create view created=%v err=%v got=%#v", created, err, v)
	}
	_, created, err = st.CreateLogView(store.LogView{
		ProjectID: project, Location: "global", BucketID: "_Default", ViewID: "errors",
	})
	if err != nil || created {
		t.Fatalf("dup view created=%v err=%v", created, err)
	}
	_, _, err = st.CreateLogView(store.LogView{ProjectID: project})
	if err == nil {
		t.Fatal("expected view validation error")
	}

	gv, ok, err := st.GetLogView(v.Name)
	if err != nil || !ok || gv.ViewID != "errors" {
		t.Fatalf("get view ok=%v err=%v got=%#v", ok, err, gv)
	}
	_, ok, err = st.GetLogView(v.Name + "-missing")
	if err != nil || ok {
		t.Fatalf("missing view ok=%v err=%v", ok, err)
	}
	views, err := st.ListLogViews(viewParent)
	if err != nil || len(views) < 1 {
		t.Fatalf("list views=%#v err=%v", views, err)
	}

	deleted, err := st.DeleteLogExclusion(ex.Name)
	if err != nil || !deleted {
		t.Fatalf("delete exclusion deleted=%v err=%v", deleted, err)
	}
	deleted, err = st.DeleteLogExclusion(ex.Name)
	if err != nil || deleted {
		t.Fatalf("second delete deleted=%v err=%v", deleted, err)
	}
}
