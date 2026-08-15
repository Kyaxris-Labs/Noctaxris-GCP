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
	upd, ok, err := st.UpdateLogSink(sinkName, "storage.googleapis.com/other", "severity>=WARNING")
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
