package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestDNSManagedZonesAndRrsetsStore(t *testing.T) {
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

	zoneName := store.DNSZoneResourceName("p", "example-zone")
	ok, err := st.CreateDNSManagedZone(store.DNSManagedZone{
		Name: zoneName, ProjectID: "p", ZoneID: "example-zone", NumericID: "1",
		DNSName: "example.com.", Visibility: "public",
		NameServersJSON: store.MarshalStringSlice([]string{"ns1.lab."}),
	})
	if err != nil || !ok {
		t.Fatalf("create zone: ok=%v err=%v", ok, err)
	}
	z, found, err := st.GetDNSManagedZone(zoneName)
	if err != nil || !found || z.DNSName != "example.com." {
		t.Fatalf("get zone: %#v found=%v err=%v", z, found, err)
	}
	list, err := st.ListDNSManagedZones("p")
	if err != nil || len(list) != 1 {
		t.Fatalf("list zones: %v err=%v", list, err)
	}

	ok, err = st.CreateDNSRrset(store.DNSRrset{
		ProjectID: "p", ZoneName: zoneName, ZoneID: "example-zone",
		RrsetName: "www.example.com.", RrsetType: "A", TTL: 300,
		RrdatasJSON: store.MarshalStringSlice([]string{"1.2.3.4"}),
	})
	if err != nil || !ok {
		t.Fatalf("create rrset: ok=%v err=%v", ok, err)
	}
	rr, found, err := st.GetDNSRrset(zoneName, "www.example.com.", "A")
	if err != nil || !found || rr.TTL != 300 {
		t.Fatalf("get rrset: %#v found=%v err=%v", rr, found, err)
	}
	rrsets, err := st.ListDNSRrsets(zoneName)
	if err != nil || len(rrsets) != 1 {
		t.Fatalf("list rrsets: %v err=%v", rrsets, err)
	}
	ok, err = st.DeleteDNSRrset(zoneName, "www.example.com.", "a")
	if err != nil || !ok {
		t.Fatalf("delete rrset: ok=%v err=%v", ok, err)
	}
	ok, err = st.DeleteDNSManagedZone(zoneName)
	if err != nil || !ok {
		t.Fatalf("delete zone: ok=%v err=%v", ok, err)
	}
}

func TestDataflowJobsTheatreStore(t *testing.T) {
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

	jobID := store.NewDataflowJobID()
	name := store.DataflowJobResourceName("p", "us-central1", jobID)
	ok, err := st.CreateDataflowJob(store.DataflowJob{
		Name: name, ProjectID: "p", Location: "us-central1", JobID: jobID,
		JobName: "lab-job", JobType: "JOB_TYPE_BATCH", CurrentState: "JOB_STATE_RUNNING",
		JobJSON: `{"name":"lab-job"}`,
	})
	if err != nil || !ok {
		t.Fatalf("create job: ok=%v err=%v", ok, err)
	}
	adv, found, err := st.AdvanceDataflowJobToDone(name)
	if err != nil || !found || adv.CurrentState != "JOB_STATE_DONE" {
		t.Fatalf("advance: %#v found=%v err=%v", adv, found, err)
	}
	again, _, err := st.AdvanceDataflowJobToDone(name)
	if err != nil || again.CurrentState != "JOB_STATE_DONE" {
		t.Fatalf("second advance should stay DONE: %#v err=%v", again, err)
	}
	list, err := st.ListDataflowJobs("p", "us-central1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v err=%v", list, err)
	}
	all, err := st.ListDataflowJobsProject("p")
	if err != nil || len(all) != 1 {
		t.Fatalf("list project: %v err=%v", all, err)
	}
}

func TestDNSDataflowServiceUsageSeeded(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	for _, svc := range []string{"dns.googleapis.com", "dataflow.googleapis.com"} {
		usage, ok, err := st.GetServiceUsage("noctaxris-gcp-local", svc)
		if err != nil {
			t.Fatalf("seed %s: %v", svc, err)
		}
		if !ok {
			t.Fatalf("seed %s: not found", svc)
		}
		if usage.State != "ENABLED" {
			t.Fatalf("seed %s: state=%q want ENABLED", svc, usage.State)
		}
	}
}
