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

func TestLoggingExclusionMatchesAndListBranches(t *testing.T) {
	mux := setupLogging(t)
	const project = "noctaxris-gcp-local"

	req := httptest.NewRequest(http.MethodPost, "/v2/projects/"+project+"/exclusions?exclusionId=drop-da",
		bytes.NewReader([]byte(`{"filter":"logName:data_access","disabled":false}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create exclusion status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v2/projects/"+project+"/exclusions?exclusionId=drop-gce",
		bytes.NewReader([]byte(`{"filter":"resource.type=\"gce_instance\"","disabled":false}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create resource exclusion status=%d body=%s", rec.Code, rec.Body.String())
	}

	writeBody := `{
  "entries": [
    {"logName":"projects/` + project + `/logs/cloudaudit.googleapis.com%2Fdata_access","textPayload":"secret","insertId":"da1"},
    {"logName":"projects/` + project + `/logs/app","textPayload":"keep","insertId":"a1","resource":{"type":"gce_instance"}},
    {"logName":"projects/` + project + `/logs/app","textPayload":"also-keep","insertId":"a2","resource":{"type":"global"}}
  ]
}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:write", bytes.NewReader([]byte(writeBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("write status=%d body=%s", rec.Code, rec.Body.String())
	}

	listBody := `{"resourceNames":["projects/` + project + `"],"pageSize":50}`
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
	for _, e := range resp.Entries {
		ln, _ := e["logName"].(string)
		if bytes.Contains([]byte(ln), []byte("data_access")) {
			t.Fatalf("data_access should be excluded: %#v", e)
		}
		if res, ok := e["resource"].(map[string]any); ok && res["type"] == "gce_instance" {
			t.Fatalf("gce_instance should be excluded: %#v", e)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("list bad json")
	}
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list",
		bytes.NewReader([]byte(`{"resourceNames":[],"pageSize":50}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("list missing project")
	}
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list",
		bytes.NewReader([]byte(`{"resourceNames":["projects/`+project+`"],"pageToken":"bad","pageSize":10}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("list bad pageToken")
	}
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list",
		bytes.NewReader([]byte(`{"resourceNames":["projects/`+project+`"],"pageSize":1,"pageToken":"0"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("paged list status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v2/entries:tail", bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("tail bad json")
	}
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:tail",
		bytes.NewReader([]byte(`{"resourceNames":["projects/`+project+`"],"pageSize":5}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tail status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v2/projects/"+project+"/sinks/_Required", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("_Required sink delete should fail")
	}
	req = httptest.NewRequest(http.MethodDelete, "/v2/projects/"+project+"/sinks/missing-sink", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing sink delete status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v2/projects/"+project+"/sinks/missing-sink", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing sink get status=%d", rec.Code)
	}
}

func TestLoggingListDeleteAuthzDeny(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodGet, "/v2/projects/noctaxris-gcp-local/logs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("listLogs deny status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/v2/projects/noctaxris-gcp-local/logs/app", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("deleteLog deny status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list",
		bytes.NewReader([]byte(`{"resourceNames":["projects/noctaxris-gcp-local"]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("listEntries deny status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:tail",
		bytes.NewReader([]byte(`{"resourceNames":["projects/noctaxris-gcp-local"]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tail deny status=%d body=%s", rec.Code, rec.Body.String())
	}
}
