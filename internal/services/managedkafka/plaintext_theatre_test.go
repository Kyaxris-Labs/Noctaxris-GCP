package managedkafka_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/managedkafka"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestKafkaClusterDeclaresPLAINTEXTACLTheatre(t *testing.T) {
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
	project := "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &managedkafka.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	})
	loc := managedkafka.DefaultLocation
	base := "/v1/projects/" + project + "/locations/" + loc + "/clusters"
	req := httptest.NewRequest(http.MethodPost, base+"?clusterId=lab", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	resp, _ := op["response"].(map[string]any)
	sec, _ := resp["securityConfig"].(map[string]any)
	if sec["securityProtocol"] != "PLAINTEXT" || sec["aclEnforcement"] != "CONTROL_PLANE_ONLY" {
		t.Fatalf("securityConfig=%#v", sec)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/lab/acls?aclId=topic/t1",
		bytes.NewReader([]byte(`{"aclEntries":[{"principal":"User:alice","operation":"READ","permissionType":"ALLOW"}]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create acl: %d %s", rec.Code, rec.Body.String())
	}
	var acl map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &acl)
	if acl["aclEnforcement"] != "CONTROL_PLANE_ONLY" {
		t.Fatalf("acl=%#v", acl)
	}
}
