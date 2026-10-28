package store

import (
	"fmt"
	"strings"
	"testing"
)

func TestLogEntriesKeysetSkipsEarlierRows(t *testing.T) {
	st := openCloudAuditStore(t)
	const logName = "projects/p1/logs/requests"
	var entries []LogEntry
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		entries = append(entries, LogEntry{
			InsertID: id, ProjectID: "p1", LogName: logName,
			Timestamp:   "2026-08-01T00:00:0" + string(rune('1'+i)) + "Z",
			PayloadJSON: `{"textPayload":"row"}`,
		})
	}
	if err := st.WriteLogEntries(entries); err != nil {
		t.Fatal(err)
	}

	page, err := st.ListLogEntries(ListLogEntriesFilter{
		ProjectID: "p1", ExactLogName: logName, PageSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].InsertID != "a" || page[1].InsertID != "b" {
		t.Fatalf("first page %#v", page)
	}
	last := page[len(page)-1]
	page, err = st.ListLogEntries(ListLogEntriesFilter{
		ProjectID: "p1", ExactLogName: logName, PageSize: 2,
		AfterTimestamp: last.Timestamp, AfterInsertID: last.InsertID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].InsertID != "c" || page[1].InsertID != "d" {
		t.Fatalf("second page %#v", page)
	}
	last = page[len(page)-1]
	page, err = st.ListLogEntries(ListLogEntriesFilter{
		ProjectID: "p1", ExactLogName: logName, PageSize: 2,
		AfterTimestamp: last.Timestamp, AfterInsertID: last.InsertID,
		Offset: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].InsertID != "e" {
		t.Fatalf("last page %#v", page)
	}
}

func TestLogEntriesKeysetDescending(t *testing.T) {
	st := openCloudAuditStore(t)
	const logName = "projects/p1/logs/requests"
	var entries []LogEntry
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		entries = append(entries, LogEntry{
			InsertID: id, ProjectID: "p1", LogName: logName,
			Timestamp:   "2026-08-01T00:00:0" + string(rune('1'+i)) + "Z",
			PayloadJSON: `{"textPayload":"row"}`,
		})
	}
	if err := st.WriteLogEntries(entries); err != nil {
		t.Fatal(err)
	}

	page, err := st.ListLogEntries(ListLogEntriesFilter{
		ProjectID: "p1", ExactLogName: logName, PageSize: 2, Descending: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].InsertID != "e" || page[1].InsertID != "d" {
		t.Fatalf("first desc page %#v", page)
	}
	last := page[len(page)-1]
	page, err = st.ListLogEntries(ListLogEntriesFilter{
		ProjectID: "p1", ExactLogName: logName, PageSize: 2, Descending: true,
		AfterTimestamp: last.Timestamp, AfterInsertID: last.InsertID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].InsertID != "c" || page[1].InsertID != "b" {
		t.Fatalf("second desc page %#v", page)
	}
}

func TestLogEntriesMultiPageNoGaps(t *testing.T) {
	st := openCloudAuditStore(t)
	const (
		logName  = "projects/p1/logs/requests"
		n        = 2500
		pageSize = 1000
	)
	entries := make([]LogEntry, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, LogEntry{
			InsertID: fmt.Sprintf("id-%04d", i), ProjectID: "p1", LogName: logName,
			Timestamp:   fmt.Sprintf("2026-08-01T00:%02d:%02dZ", i/60, i%60),
			PayloadJSON: `{"textPayload":"row"}`,
		})
	}
	if err := st.WriteLogEntries(entries); err != nil {
		t.Fatal(err)
	}

	for _, descending := range []bool{false, true} {
		var got []string
		var afterTS, afterID string
		for page := 0; page < 10; page++ {
			f := ListLogEntriesFilter{
				ProjectID: "p1", ExactLogName: logName, PageSize: pageSize, Descending: descending,
				AfterTimestamp: afterTS, AfterInsertID: afterID,
			}
			rows, err := st.ListLogEntries(f)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) == 0 {
				break
			}
			for i, row := range rows {
				if i > 0 {
					prev := rows[i-1]
					if descending {
						if row.Timestamp > prev.Timestamp || (row.Timestamp == prev.Timestamp && row.InsertID > prev.InsertID) {
							t.Fatalf("desc order broke at %s %s", row.Timestamp, row.InsertID)
						}
					} else if row.Timestamp < prev.Timestamp || (row.Timestamp == prev.Timestamp && row.InsertID < prev.InsertID) {
						t.Fatalf("asc order broke at %s %s", row.Timestamp, row.InsertID)
					}
				}
				got = append(got, row.InsertID)
			}
			if len(rows) < pageSize {
				break
			}
			last := rows[len(rows)-1]
			afterTS, afterID = last.Timestamp, last.InsertID
		}
		if len(got) != n {
			t.Fatalf("descending=%v got %d want %d", descending, len(got), n)
		}
		seen := map[string]struct{}{}
		for _, id := range got {
			if _, ok := seen[id]; ok {
				t.Fatalf("descending=%v duplicate %s", descending, id)
			}
			seen[id] = struct{}{}
		}
	}
}

func TestLogEntriesListPlanUsesIndex(t *testing.T) {
	st := openCloudAuditStore(t)
	rows, err := st.db.Query(`EXPLAIN QUERY PLAN
		SELECT insert_id FROM log_entries
		WHERE project_id = ? AND log_name = ? AND timestamp >= ?
		  AND (timestamp, insert_id) > (?, ?)
		ORDER BY timestamp ASC, insert_id ASC
		LIMIT ?`,
		"p1", "projects/p1/logs/requests", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "a", 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "idx_log_entries_log_ts") {
		t.Fatalf("list plan did not use idx_log_entries_log_ts:\n%s", plan.String())
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	rows, err = st.db.Query(`EXPLAIN QUERY PLAN
		SELECT insert_id FROM log_entries
		WHERE project_id = ? AND log_name = ? AND timestamp >= ?
		  AND (timestamp, insert_id) < (?, ?)
		ORDER BY timestamp DESC, insert_id DESC
		LIMIT ?`,
		"p1", "projects/p1/logs/requests", "2026-01-01T00:00:00Z", "2026-08-01T00:00:00Z", "z", 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan.Reset()
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "idx_log_entries_log_ts") {
		t.Fatalf("desc list plan did not use idx_log_entries_log_ts:\n%s", plan.String())
	}
}
