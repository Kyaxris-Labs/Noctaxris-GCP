package managedkafka_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/managedkafka"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/memorystore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type stubKafkaEngine struct {
	err           error
	topicCalls    int
	lastTopic     string
	lastContainer string
}

func (s *stubKafkaEngine) Enabled() bool { return true }

func (s *stubKafkaEngine) EnsureRedpanda(context.Context, string) (string, string, error) {
	return "", "", s.err
}

func (s *stubKafkaEngine) RemoveRedpanda(context.Context, string) error { return nil }

func (s *stubKafkaEngine) CreateRedpandaTopic(_ context.Context, containerRef, topic string, _, _ int) error {
	s.topicCalls++
	s.lastContainer = containerRef
	s.lastTopic = topic
	return s.err
}

func (s *stubKafkaEngine) Close() error { return nil }

func mountManagedKafkaTest(t *testing.T, svc *managedkafka.Service) *http.ServeMux {
	t.Helper()
	restlab.ClearLocationOperationGetHooks()
	mux := http.NewServeMux()
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})
	return mux
}

func TestManagedKafkaCreateOperationShapeAndConfigEcho(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	mux := mountManagedKafkaTest(t, &managedkafka.Service{Store: st, Authz: &authz.Evaluator{Policies: st}})

	loc := managedkafka.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/clusters"
	body := `{
		"displayName":"Lab Kafka",
		"capacityConfig":{"vcpuCount":3,"memoryBytes":3221225472},
		"gcpConfig":{"accessConfig":{"networkConfigs":[{"subnet":"projects/p/regions/us-central1/subnetworks/default"}]}}
	}`
	req := httptest.NewRequest(http.MethodPost, base+"?clusterId=lab-kafka", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &createOp)
	if createOp["done"] != true {
		t.Fatalf("create expected Operation, got %#v", createOp)
	}
	if createOp["state"] != nil {
		t.Fatalf("create body must be Operation not bare Cluster: %#v", createOp)
	}
	resp, _ := createOp["response"].(map[string]any)
	if resp == nil {
		t.Fatalf("create missing response: %#v", createOp)
	}
	if resp["@type"] != "type.googleapis.com/google.cloud.managedkafka.v1.Cluster" {
		t.Fatalf("@type=%v", resp["@type"])
	}
	capacity, _ := resp["capacityConfig"].(map[string]any)
	if capacity == nil || capacity["vcpuCount"] != float64(3) {
		t.Fatalf("create response capacity=%#v", resp["capacityConfig"])
	}
	gcp, _ := resp["gcpConfig"].(map[string]any)
	if gcp == nil {
		t.Fatalf("create response missing gcpConfig: %#v", resp)
	}

	opName, _ := createOp["name"].(string)
	if !strings.HasSuffix(opName, "/operations/create-lab-kafka") {
		t.Fatalf("op name=%q", opName)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/locations/"+loc+"/operations/create-lab-kafka", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get operation status=%d body=%s", rec.Code, rec.Body.String())
	}
	var polled map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &polled)
	if polled["done"] != true {
		t.Fatalf("poll expected done: %#v", polled)
	}
	pollResp, _ := polled["response"].(map[string]any)
	if pollResp == nil || pollResp["@type"] != "type.googleapis.com/google.cloud.managedkafka.v1.Cluster" {
		t.Fatalf("poll response missing typed Cluster: %#v", polled)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lab-kafka", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cluster map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &cluster)
	if cluster["capacityConfig"] == nil || cluster["gcpConfig"] == nil {
		t.Fatalf("get echo=%#v", cluster)
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	items, _ := list["clusters"].([]any)
	if len(items) != 1 {
		t.Fatalf("list=%#v", list)
	}
	first, _ := items[0].(map[string]any)
	if first["capacityConfig"] == nil || first["gcpConfig"] == nil {
		t.Fatalf("list echo=%#v", first)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/lab-kafka", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	var delOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &delOp)
	if delOp["done"] != true {
		t.Fatalf("delete expected Operation, got %#v", delOp)
	}
	if len(delOp) <= 1 {
		t.Fatalf("delete must not return empty object: %#v", delOp)
	}
	if !strings.HasSuffix(delOp["name"].(string), "/operations/delete-lab-kafka") {
		t.Fatalf("delete op=%#v", delOp)
	}
}

func TestManagedKafkaOperationsGetViaMemorystoreMux(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	restlab.ClearLocationOperationGetHooks()
	mux := http.NewServeMux()
	principal := func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	}
	// Production order: Memorystore mounts Operations.get first; Kafka registers a hook.
	(&memorystore.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, principal)
	(&managedkafka.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, principal)

	loc := managedkafka.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/clusters"
	body := `{"displayName":"Shared Mux","capacityConfig":{"vcpuCount":3,"memoryBytes":3221225472}}`
	req := httptest.NewRequest(http.MethodPost, base+"?clusterId=shared-mux-kafka", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/locations/"+loc+"/operations/create-shared-mux-kafka", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("poll status=%d body=%s", rec.Code, rec.Body.String())
	}
	var polled map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &polled)
	if polled["done"] != true {
		t.Fatalf("poll=%#v", polled)
	}
	resp, _ := polled["response"].(map[string]any)
	if resp == nil || resp["@type"] != "type.googleapis.com/google.cloud.managedkafka.v1.Cluster" {
		t.Fatalf("Memorystore-first poll must return typed Cluster via Kafka hook: %#v", polled)
	}
}

func TestManagedKafkaClustersCRUD(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	mux := mountManagedKafkaTest(t, &managedkafka.Service{Store: st, Authz: &authz.Evaluator{Policies: st}})

	loc := managedkafka.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/clusters"
	body := `{"displayName":"Lab Kafka","labels":{"env":"lab"}}`
	req := httptest.NewRequest(http.MethodPost, base+"?clusterId=lab-kafka", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &createOp)
	if createOp["done"] != true {
		t.Fatalf("create expected Operation: %#v", createOp)
	}
	cluster, _ := createOp["response"].(map[string]any)
	if cluster == nil || cluster["state"] != "ACTIVE" {
		t.Fatalf("cluster=%#v", createOp)
	}
	bs, _ := cluster["bootstrapServers"].(string)
	if bs == "" {
		t.Fatal("expected theatre bootstrapServers")
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lab-kafka", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	items, _ := list["clusters"].([]any)
	if len(items) != 1 {
		t.Fatalf("list=%#v", list)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/lab-kafka", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	var delOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &delOp)
	if delOp["done"] != true {
		t.Fatalf("delete expected Operation: %#v", delOp)
	}
}

func TestManagedKafkaNestedEngineFailClosed(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	startErr := errors.New("redpanda start refused")
	mux := mountManagedKafkaTest(t, &managedkafka.Service{
		Store:  st,
		Authz:  &authz.Evaluator{Policies: st},
		Engine: &stubKafkaEngine{err: startErr},
	})

	loc := managedkafka.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/clusters"
	body := `{"displayName":"Lab Kafka"}`

	t.Setenv(compute.EnvNestedEngineFailClosed, "")
	req := httptest.NewRequest(http.MethodPost, base+"?clusterId=soft-kafka", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("soft create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var soft map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &soft)
	resp, _ := soft["response"].(map[string]any)
	if resp == nil || resp["state"] != "ACTIVE" {
		t.Fatalf("soft cluster=%#v", soft)
	}

	t.Setenv(compute.EnvNestedEngineFailClosed, "1")
	req = httptest.NewRequest(http.MethodPost, base+"?clusterId=hard-kafka", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fail-closed create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	detail, _ := errBody["error"].(map[string]any)
	if detail["status"] != "FAILED_PRECONDITION" {
		t.Fatalf("fail-closed body=%#v", errBody)
	}
	msg, _ := detail["message"].(string)
	if !strings.Contains(msg, "nested engine start failed") || !strings.Contains(msg, "redpanda start refused") {
		t.Fatalf("message=%q", msg)
	}
	_, ok, err := st.GetKafkaCluster("projects/noctaxris-gcp-local/locations/" + loc + "/clusters/hard-kafka")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("fail-closed create should roll back store row")
	}
}

func TestManagedKafkaAuthzFailClosed(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &managedkafka.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestManagedKafkaNilAuthzFailClosed(t *testing.T) {
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

	mux := http.NewServeMux()
	svc := &managedkafka.Service{Store: st, Authz: nil}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nil Authz expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestManagedKafkaTopicsAndACLsCRUD(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	engine := &stubKafkaEngine{}
	mux := mountManagedKafkaTest(t, &managedkafka.Service{
		Store:  st,
		Authz:  &authz.Evaluator{Policies: st},
		Engine: engine,
	})

	loc := managedkafka.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/clusters"
	req := httptest.NewRequest(http.MethodPost, base+"?clusterId=lab-kafka", bytes.NewReader([]byte(`{"displayName":"Lab"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create cluster status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Theatre path: no container_id, topic create must still persist without nested call.
	topicsBase := base + "/lab-kafka/topics"
	body := `{"partitionCount":3,"replicationFactor":1,"configs":{"cleanup.policy":"delete"}}`
	req = httptest.NewRequest(http.MethodPost, topicsBase+"?topicId=orders", bytes.NewReader([]byte(body)))
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
	if engine.topicCalls != 0 {
		t.Fatalf("expected no nested topic call without container_id, got %d", engine.topicCalls)
	}

	req = httptest.NewRequest(http.MethodGet, topicsBase+"/orders", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get topic status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, topicsBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list topics status=%d body=%s", rec.Code, rec.Body.String())
	}
	var topicList map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &topicList)
	items, _ := topicList["topics"].([]any)
	if len(items) != 1 {
		t.Fatalf("list topics=%#v", topicList)
	}

	// Seed nested container_id and verify best-effort rpk path is invoked.
	clusterName := "projects/noctaxris-gcp-local/locations/" + loc + "/clusters/lab-kafka"
	if err := st.UpdateKafkaClusterNested(clusterName, "broker:9092", "cid-redpanda", "ACTIVE"); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, topicsBase+"?topicId=events", bytes.NewReader([]byte(`{"partitionCount":1,"replicationFactor":1}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create topic with container status=%d body=%s", rec.Code, rec.Body.String())
	}
	if engine.topicCalls != 1 || engine.lastTopic != "events" || engine.lastContainer != "cid-redpanda" {
		t.Fatalf("nested topic call=%d topic=%q container=%q", engine.topicCalls, engine.lastTopic, engine.lastContainer)
	}

	aclsBase := base + "/lab-kafka/acls"
	aclBody := `{"aclEntries":[{"principal":"User:*","permissionType":"ALLOW","operation":"READ","host":"*"}]}`
	req = httptest.NewRequest(http.MethodPost, aclsBase+"?aclId=topic/orders", bytes.NewReader([]byte(aclBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create acl status=%d body=%s", rec.Code, rec.Body.String())
	}
	var acl map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &acl)
	if acl["resourceType"] != "TOPIC" || acl["resourceName"] != "orders" || acl["patternType"] != "LITERAL" {
		t.Fatalf("acl=%#v", acl)
	}

	req = httptest.NewRequest(http.MethodGet, aclsBase+"/topic/orders", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get acl status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, aclsBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list acls status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, aclsBase+"/topic/orders", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete acl status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, topicsBase+"/orders", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete topic status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Cluster delete cascades remaining topics.
	req = httptest.NewRequest(http.MethodDelete, base+"/lab-kafka", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete cluster status=%d body=%s", rec.Code, rec.Body.String())
	}
	list, err := st.ListKafkaTopics(clusterName)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected cascade delete topics, got %#v", list)
	}
}

func TestManagedKafkaTopicsAuthzFailClosed(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &managedkafka.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters/x/topics", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
