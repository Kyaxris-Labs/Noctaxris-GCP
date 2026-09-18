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

func setupLogsInject(t *testing.T, logsInject bool) *http.ServeMux {
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
	svc := &logging.Service{Store: st, Authz: &authz.Evaluator{Policies: st}, LogsInject: logsInject}
	who := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: rootSA, IsRoot: true}, true
	}
	svc.Mount(mux, who)
	svc.MountLab(mux, who, "noctaxris-gcp-local")
	return mux
}

func TestLogsInjectDeniedWhenDisabled(t *testing.T) {
	mux := setupLogsInject(t, false)
	body := `{"entries":[{"logName":"projects/noctaxris-gcp-local/logs/run","jsonPayload":{"ok":true},"resource":{"type":"cloud_run_revision"}}]}`
	req := httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/logs:inject", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLoggingResourceTypeFilter(t *testing.T) {
	t.Setenv(logging.EnvAuditInject, "1")
	mux := setupLogsInject(t, true)
	project := "noctaxris-gcp-local"
	inject := `{
  "projectId":"` + project + `",
  "entries":[
    {"logName":"projects/` + project + `/logs/requests","jsonPayload":{"enforcedSecurityPolicy":{"name":"armor-edge"},"previewSecurityPolicy":{"name":"armor-preview"}},"resource":{"type":"http_load_balancer"}},
    {"logName":"projects/` + project + `/logs/run.googleapis.com%2Frequests","jsonPayload":{"status":200},"resource":{"type":"cloud_run_revision"}},
    {"logName":"projects/` + project + `/logs/compute.googleapis.com%2Fvpc_flows","jsonPayload":{"connection":{"src_ip":"10.0.0.1","src_port":443,"dest_ip":"10.0.0.2","dest_port":8080,"protocol":6},"bytes_sent":4096},"resource":{"type":"gce_subnetwork"}},
    {"logName":"projects/` + project + `/logs/cloudsql.googleapis.com%2Fpostgres.log","jsonPayload":{"PgAuditEntry":{"statement":"SELECT 1"}},"resource":{"type":"cloudsql_database"}},
    {"logName":"projects/` + project + `/logs/dns.googleapis.com%2Fdns_queries","jsonPayload":{"queryName":"lab.example."},"resource":{"type":"dns_query"}}
  ]
}`
	req := httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/logs:inject", bytes.NewReader([]byte(inject)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logs inject status=%d body=%s", rec.Code, rec.Body.String())
	}

	cal := `{
  "projectId":"` + project + `",
  "entries":[{
    "logName":"projects/` + project + `/logs/cloudaudit.googleapis.com%2Fdata_access",
    "protoPayload":{
      "serviceName":"iamcredentials.googleapis.com",
      "methodName":"GenerateAccessToken",
      "serviceAccountDelegationInfo":[{"firstPartyPrincipal":{"principalEmail":"chain@` + project + `.iam.gserviceaccount.com"}}]
    },
    "resource":{"type":"service_account"}
  },{
    "logName":"projects/` + project + `/logs/cloudaudit.googleapis.com%2Fdata_access",
    "protoPayload":{"serviceName":"secretmanager.googleapis.com","methodName":"AccessSecretVersion"},
    "resource":{"type":"secretmanager.googleapis.com/SecretVersion"}
  }]
}`
	req = httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/auditLogs:inject", bytes.NewReader([]byte(cal)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cal inject status=%d body=%s", rec.Code, rec.Body.String())
	}

	for _, rt := range []string{"http_load_balancer", "cloud_run_revision", "gce_subnetwork", "cloudsql_database", "dns_query"} {
		list := `{"resourceNames":["projects/` + project + `"],"filter":"resource.type=\"` + rt + `\""}`
		req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(list)))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s list status=%d body=%s", rt, rec.Code, rec.Body.String())
		}
		var resp struct {
			Entries []map[string]any `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Entries) != 1 {
			t.Fatalf("%s entries=%#v", rt, resp.Entries)
		}
		res, _ := resp.Entries[0]["resource"].(map[string]any)
		if res["type"] != rt {
			t.Fatalf("%s resource=%#v", rt, res)
		}
	}

	list := `{"resourceNames":["projects/` + project + `"],"filter":"logName=\"projects/` + project + `/logs/cloudaudit.googleapis.com%2Fdata_access\" AND protoPayload.methodName=\"GenerateAccessToken\""}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(list)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cal list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var calResp struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &calResp); err != nil {
		t.Fatal(err)
	}
	foundTok, foundSecret := false, false
	for _, e := range calResp.Entries {
		pp, _ := e["protoPayload"].(map[string]any)
		switch pp["methodName"] {
		case "GenerateAccessToken":
			foundTok = true
			if pp["serviceAccountDelegationInfo"] == nil {
				t.Fatalf("missing delegation %#v", pp)
			}
		case "AccessSecretVersion":
			foundSecret = true
		}
	}
	if !foundTok || !foundSecret {
		t.Fatalf("cal methods tok=%v secret=%v entries=%#v", foundTok, foundSecret, calResp.Entries)
	}
}
