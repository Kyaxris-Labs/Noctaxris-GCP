package logging_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/logging"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func setupLogging(t *testing.T) *http.ServeMux {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootSA := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureRoot("noctaxris-gcp-local", rootSA); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &logging.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: rootSA, IsRoot: true}, true
	})
	return mux
}

func TestWriteAndListEntries(t *testing.T) {
	mux := setupLogging(t)
	writeBody := `{
  "logName": "projects/noctaxris-gcp-local/logs/app",
  "entries": [
    {"textPayload": "alpha-event", "severity": "INFO"},
    {"logName": "projects/noctaxris-gcp-local/logs/other", "textPayload": "beta"}
  ]
}`
	req := httptest.NewRequest(http.MethodPost, "/v2/entries:write", bytes.NewReader([]byte(writeBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("write status=%d body=%s", rec.Code, rec.Body.String())
	}

	listBody := `{
  "resourceNames": ["projects/noctaxris-gcp-local"],
  "filter": "logName=\"projects/noctaxris-gcp-local/logs/app\"",
  "pageSize": 50
}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(listBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("entries = %#v", resp.Entries)
	}
	if resp.Entries[0]["textPayload"] != "alpha-event" {
		t.Fatalf("entry = %#v", resp.Entries[0])
	}

	listBody = `{
  "resourceNames": ["projects/noctaxris-gcp-local"],
  "filter": "textPayload:\"alpha\"",
  "pageSize": 50
}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(listBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list text filter status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("text filter entries = %#v", resp.Entries)
	}
}

func TestListDeleteLogsAndSeverityTimestampFilter(t *testing.T) {
	mux := setupLogging(t)
	writeBody := `{
  "logName": "projects/noctaxris-gcp-local/logs/app",
  "entries": [
    {"textPayload": "early", "severity": "INFO", "timestamp": "2026-01-01T10:00:00Z", "insertId": "e1"},
    {"textPayload": "late", "severity": "ERROR", "timestamp": "2026-01-01T12:00:00Z", "insertId": "e2"}
  ]
}`
	req := httptest.NewRequest(http.MethodPost, "/v2/entries:write", bytes.NewReader([]byte(writeBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("write status=%d body=%s", rec.Code, rec.Body.String())
	}

	other := `{"logName":"projects/noctaxris-gcp-local/logs/other","entries":[{"textPayload":"x","insertId":"o1"}]}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:write", bytes.NewReader([]byte(other)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("write other status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/projects/noctaxris-gcp-local/logs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list logs status=%d body=%s", rec.Code, rec.Body.String())
	}
	var logsResp struct {
		LogNames []string `json:"logNames"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &logsResp); err != nil {
		t.Fatal(err)
	}
	if len(logsResp.LogNames) != 2 {
		t.Fatalf("logNames=%#v", logsResp.LogNames)
	}

	listBody := `{
  "resourceNames": ["projects/noctaxris-gcp-local"],
  "filter": "severity=ERROR timestamp>=\"2026-01-01T11:00:00Z\"",
  "pageSize": 50
}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(listBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("filter list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 1 || resp.Entries[0]["textPayload"] != "late" {
		t.Fatalf("filtered entries=%#v", resp.Entries)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v2/projects/noctaxris-gcp-local/logs/app", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete log status=%d body=%s", rec.Code, rec.Body.String())
	}

	listBody = `{
  "resourceNames": ["projects/noctaxris-gcp-local"],
  "filter": "logName=\"projects/noctaxris-gcp-local/logs/app\"",
  "pageSize": 50
}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(listBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("expected empty after delete, got %#v", resp.Entries)
	}
}

func TestLoggingAuthzDenyNonRoot(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
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
	mux := http.NewServeMux()
	svc := &logging.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodPost, "/v2/entries:write", bytes.NewReader([]byte(`{"logName":"projects/noctaxris-gcp-local/logs/x","entries":[{"textPayload":"nope"}]}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSinksTailAndCopy(t *testing.T) {
	mux := setupLogging(t)

	create := `{"destination":"storage.googleapis.com/lab-bucket","filter":"severity>=ERROR"}`
	req := httptest.NewRequest(http.MethodPost, "/v2/projects/noctaxris-gcp-local/sinks?sinkId=lab-sink", bytes.NewReader([]byte(create)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create sink status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/projects/noctaxris-gcp-local/sinks/lab-sink", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get sink status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPatch, "/v2/projects/noctaxris-gcp-local/sinks/lab-sink", bytes.NewReader([]byte(`{"filter":"severity=ERROR"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update sink status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sink map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sink)
	if sink["filter"] != "severity=ERROR" {
		t.Fatalf("sink=%#v", sink)
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/projects/noctaxris-gcp-local/sinks", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sinks status=%d", rec.Code)
	}

	writeBody := `{"logName":"projects/noctaxris-gcp-local/logs/app","entries":[{"textPayload":"tail-me","insertId":"t1"}]}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:write", bytes.NewReader([]byte(writeBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("write status=%d", rec.Code)
	}

	tailBody := `{"resourceNames":["projects/noctaxris-gcp-local"],"filter":"textPayload:\"tail-me\"","pageSize":10}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:tail", bytes.NewReader([]byte(tailBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tail status=%d body=%s", rec.Code, rec.Body.String())
	}
	var tailResp struct {
		Entries []map[string]any `json:"entries"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &tailResp)
	if len(tailResp.Entries) != 1 {
		t.Fatalf("tail entries=%#v", tailResp.Entries)
	}

	copyBody := `{"resourceNames":["projects/noctaxris-gcp-local"],"destination":"storage.googleapis.com/out","filter":"severity=ERROR"}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:copy", bytes.NewReader([]byte(copyBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("copy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var copyResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &copyResp)
	if copyResp["done"] != true {
		t.Fatalf("copy=%#v", copyResp)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v2/projects/noctaxris-gcp-local/sinks/lab-sink", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete sink status=%d", rec.Code)
	}
}
