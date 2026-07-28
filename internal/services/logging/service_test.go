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
