package bigquery_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBigQueryQueryDialectsAndJobsGet(t *testing.T) {
	mux, _ := testBigQueryMux(t)
	project := "noctaxris-gcp-local"
	qURL := "/bigquery/v2/projects/" + project + "/queries"
	doQuery := func(q string, dry bool) *httptest.ResponseRecorder {
		t.Helper()
		body := map[string]any{"query": q, "dryRun": dry}
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, qURL, bytes.NewReader(raw))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := doQuery("CREATE TABLE labds.items (id STRING, qty INT64, cat STRING)", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("create table: %d %s", rec.Code, rec.Body.String())
	}
	rec = doQuery("CREATE TABLE labds.items (id STRING)", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("create dryRun: %d %s", rec.Code, rec.Body.String())
	}

	tblBase := "/bigquery/v2/projects/" + project + "/datasets/labds/tables/items"
	insert := `{"rows":[{"json":{"id":"a","qty":1,"cat":"x"}},{"json":{"id":"b","qty":2,"cat":"x"}},{"json":{"id":"c","qty":3,"cat":"y"}}]}`
	req := httptest.NewRequest(http.MethodPost, tblBase+"/insertAll", bytes.NewReader([]byte(insert)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insertAll: %d %s", rec.Code, rec.Body.String())
	}

	rec = doQuery("CREATE TABLE labds.orders (id STRING, item_id STRING)", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("create orders: %d %s", rec.Code, rec.Body.String())
	}
	ordInsert := `{"rows":[{"json":{"id":"o1","item_id":"a"}},{"json":{"id":"o2","item_id":"b"}}]}`
	req = httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/datasets/labds/tables/orders/insertAll", bytes.NewReader([]byte(ordInsert)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert orders: %d %s", rec.Code, rec.Body.String())
	}

	rec = doQuery("SELECT i.id, o.id FROM labds.items i JOIN labds.orders o ON i.id = o.item_id LIMIT 10", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("join: %d %s", rec.Code, rec.Body.String())
	}
	rec = doQuery("SELECT i.id FROM labds.items i JOIN labds.orders o ON i.id = o.item_id LIMIT 1", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("join dry: %d %s", rec.Code, rec.Body.String())
	}

	rec = doQuery("SELECT table_name FROM labds.INFORMATION_SCHEMA.TABLES", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("info schema: %d %s", rec.Code, rec.Body.String())
	}
	rec = doQuery("SELECT * FROM labds.INFORMATION_SCHEMA.TABLES", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("info schema dry: %d %s", rec.Code, rec.Body.String())
	}

	rec = doQuery("SELECT cat, COUNT(*) AS n FROM labds.items GROUP BY cat", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("group: %d %s", rec.Code, rec.Body.String())
	}
	rec = doQuery("SELECT cat, SUM(qty) AS total FROM labds.items GROUP BY cat LIMIT 5", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("group sum: %d %s", rec.Code, rec.Body.String())
	}

	rec = doQuery("SELECT id FROM labds.items WHERE cat = 'x' LIMIT 2 UNION ALL SELECT id FROM labds.items WHERE cat = 'y' LIMIT 2", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("union all: %d %s", rec.Code, rec.Body.String())
	}

	rec = doQuery("SELECT id FROM labds.items WHERE cat = 'x' LIMIT 2", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("select where: %d %s", rec.Code, rec.Body.String())
	}
	var qr map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &qr)
	jobRef, _ := qr["jobReference"].(map[string]any)
	jobID, _ := jobRef["jobId"].(string)
	if jobID == "" {
		t.Fatalf("missing jobId in %#v", qr)
	}
	req = httptest.NewRequest(http.MethodGet, "/bigquery/v2/projects/"+project+"/jobs/"+jobID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("jobs.get: %d %s", rec.Code, rec.Body.String())
	}
}
