package managedkafka_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/managedkafka"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestManagedKafkaACLAndTopicGetDeletePaths(t *testing.T) {
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
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := mountManagedKafkaTest(t, &managedkafka.Service{Store: st, Authz: &authz.Evaluator{Policies: st}})
	loc := managedkafka.DefaultLocation
	base := "/v1/projects/" + project + "/locations/" + loc + "/clusters"

	req := httptest.NewRequest(http.MethodPost, base+"?clusterId=acl-lab",
		bytes.NewReader([]byte(`{"displayName":"ACL Lab"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create cluster status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/acl-lab", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get cluster status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/missing-cluster", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing cluster status=%d", rec.Code)
	}

	topicBase := base + "/acl-lab/topics"
	topicBody := `{"partitionCount":3,"replicationFactor":2,"configs":{"retention.ms":"1000"}}`
	req = httptest.NewRequest(http.MethodPost, topicBase+"?topicId=orders", bytes.NewReader([]byte(topicBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create topic status=%d body=%s", rec.Code, rec.Body.String())
	}
	var topic map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &topic)
	if topic["partitionCount"] != float64(3) {
		t.Fatalf("topic=%#v", topic)
	}
	req = httptest.NewRequest(http.MethodGet, topicBase+"/orders", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get topic status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, topicBase+"/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing topic status=%d", rec.Code)
	}

	aclBase := base + "/acl-lab/acls"
	aclBody := `{"aclEntries":[{"principal":"User:lab","operation":"READ","permissionType":"ALLOW"}]}`
	req = httptest.NewRequest(http.MethodPost, aclBase+"?aclId=topic/orders", bytes.NewReader([]byte(aclBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create acl status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, aclBase+"?aclId=topic/orders", bytes.NewReader([]byte(aclBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup acl status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, aclBase, bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("acl missing id")
	}
	req = httptest.NewRequest(http.MethodPost, aclBase+"?aclId=consumerGroup/cg1",
		bytes.NewReader([]byte(`{"name":"projects/p/locations/l/clusters/c/acls/consumerGroup/cg1","aclEntries":[]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create cg acl status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, aclBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list acls status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	if acls, _ := listed["acls"].([]any); len(acls) < 2 {
		t.Fatalf("list=%#v", listed)
	}

	req = httptest.NewRequest(http.MethodGet, aclBase+"/topic/orders", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get acl status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, aclBase+"/topic/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing acl status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, aclBase+"/topic/orders", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete acl status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, aclBase+"/topic/orders", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing acl status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, topicBase+"/orders", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete topic status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, topicBase+"/orders", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing topic status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/acl-lab", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete cluster status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/acl-lab", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing cluster status=%d", rec.Code)
	}
}
