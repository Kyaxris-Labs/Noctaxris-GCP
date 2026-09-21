package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestLogSinksAndFilterList(t *testing.T) {
	st := openTestStore(t)
	project := "noctaxris-gcp-local"
	if err := st.WriteLogEntries([]store.LogEntry{{
		ProjectID: project, LogName: "projects/" + project + "/logs/lab",
		Severity: "INFO", PayloadJSON: `{"msg":"hi"}`, InsertID: "1",
	}, {
		ProjectID: project, LogName: "projects/" + project + "/logs/lab",
		Severity: "ERROR", PayloadJSON: `{"msg":"err"}`, InsertID: "2",
	}}); err != nil {
		t.Fatal(err)
	}
	names, err := st.ListLogNames(project)
	if err != nil || len(names) < 1 {
		t.Fatalf("names=%v err=%v", names, err)
	}
	entries, err := st.ListLogEntries(store.ListLogEntriesFilter{
		ProjectID: project, ExactLogName: "projects/" + project + "/logs/lab", PageSize: 10,
	})
	if err != nil || len(entries) < 2 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	sink, created, err := st.CreateLogSink(store.LogSink{
		ProjectID: project, SinkID: "lab-sink", Destination: "storage.googleapis.com/bkt", Filter: "severity>=ERROR",
	})
	if err != nil || !created {
		t.Fatalf("create sink %#v created=%v err=%v", sink, created, err)
	}
	sinkName := sink.Name
	got, ok, err := st.GetLogSink(sinkName)
	if err != nil || !ok || got.Filter != "severity>=ERROR" {
		t.Fatal(err)
	}
	list, err := st.ListLogSinks(project)
	if err != nil || len(list) < 1 {
		t.Fatalf("list=%v", list)
	}
	upd, ok, err := st.UpdateLogSink(sinkName, "storage.googleapis.com/other", "severity>=WARNING", false)
	if err != nil || !ok || upd.Destination != "storage.googleapis.com/other" {
		t.Fatalf("update %#v", upd)
	}
	n, err := st.DeleteLogEntries(project, "projects/"+project+"/logs/lab")
	if err != nil || n < 1 {
		t.Fatalf("delete entries n=%d err=%v", n, err)
	}
	ok, err = st.DeleteLogSink(sinkName)
	if err != nil || !ok {
		t.Fatal(err)
	}
}

func TestLogSinkDisabledSkippedOnMatch(t *testing.T) {
	st := openTestStore(t)
	project := "noctaxris-gcp-local"
	entry := store.LogEntry{ProjectID: project, LogName: "projects/" + project + "/logs/lab", Severity: "ERROR"}
	live, created, err := st.CreateLogSink(store.LogSink{
		ProjectID: project, SinkID: "live", Destination: "storage.googleapis.com/live", Filter: "severity=ERROR",
	})
	if err != nil || !created {
		t.Fatalf("live sink %#v created=%v err=%v", live, created, err)
	}
	off, created, err := st.CreateLogSink(store.LogSink{
		ProjectID: project, SinkID: "off", Destination: "storage.googleapis.com/off", Filter: "severity=ERROR", Disabled: true,
	})
	if err != nil || !created {
		t.Fatalf("disabled sink %#v created=%v err=%v", off, created, err)
	}
	got, ok, err := st.GetLogSink(off.Name)
	if err != nil || !ok || !got.Disabled {
		t.Fatalf("get disabled %#v ok=%v err=%v", got, ok, err)
	}
	matched, err := st.MatchingLogSinks(project, entry)
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || matched[0].SinkID != "live" {
		t.Fatalf("matching=%#v", matched)
	}
	upd, ok, err := st.UpdateLogSink(off.Name, off.Destination, off.Filter, false)
	if err != nil || !ok || upd.Disabled {
		t.Fatalf("enable %#v ok=%v err=%v", upd, ok, err)
	}
	matched, err = st.MatchingLogSinks(project, entry)
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 2 {
		t.Fatalf("after enable matching=%#v", matched)
	}
}
