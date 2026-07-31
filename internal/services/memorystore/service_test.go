package memorystore_test

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
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/memorystore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type stubRedisEngine struct {
	err          error
	lastAuthPass string
}

func (s *stubRedisEngine) Enabled() bool { return true }

func (s *stubRedisEngine) EnsureMemorystoreRedis(_ context.Context, _, authPassword string) (compute.MemorystoreRedisResult, error) {
	s.lastAuthPass = authPassword
	return compute.MemorystoreRedisResult{}, s.err
}

func (s *stubRedisEngine) RemoveMemorystoreRedis(context.Context, string, string) error { return nil }

func (s *stubRedisEngine) Close() error { return nil }

func TestMemorystoreRedisInstancesCRUD(t *testing.T) {
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
	svc := &memorystore.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	loc := memorystore.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/instances"
	body := `{"tier":"BASIC","memorySizeGb":1,"displayName":"Lab Redis","redisVersion":"REDIS_7_0"}`
	req := httptest.NewRequest(http.MethodPost, base+"?instanceId=lab-redis", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &createOp)
	if createOp["done"] != true {
		t.Fatalf("create expected done Operation: %#v", createOp)
	}
	inst, _ := createOp["response"].(map[string]any)
	if inst == nil {
		t.Fatalf("create missing response: %#v", createOp)
	}
	if inst["state"] != "READY" {
		t.Fatalf("instance=%#v", inst)
	}
	host, _ := inst["host"].(string)
	if host == "" {
		t.Fatal("expected theatre host")
	}
	if !strings.Contains(host, "lab-redis") || !strings.Contains(host, ".redis.noctaxris-gcp.lab") {
		t.Fatalf("unexpected theatre host %q", host)
	}
	if int(inst["port"].(float64)) != 6379 {
		t.Fatalf("port=%v", inst["port"])
	}
	if int(inst["memorySizeGb"].(float64)) != 1 {
		t.Fatalf("memorySizeGb=%v", inst["memorySizeGb"])
	}
	opName, _ := createOp["name"].(string)
	if !strings.HasSuffix(opName, "/operations/create-lab-redis") {
		t.Fatalf("op name=%q", opName)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/locations/"+loc+"/operations/create-lab-redis", nil)
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

	req = httptest.NewRequest(http.MethodGet, base+"/lab-redis", nil)
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
	items, _ := list["instances"].([]any)
	if len(items) != 1 {
		t.Fatalf("list=%#v", list)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/lab-redis", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	var deleteOp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &deleteOp); err != nil {
		t.Fatal(err)
	}
	if deleteOp["done"] != true {
		t.Fatalf("delete expected done Operation: %#v", deleteOp)
	}
}

func TestMemorystoreNestedEngineFailClosed(t *testing.T) {
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

	startErr := errors.New("redis start refused")
	mux := http.NewServeMux()
	svc := &memorystore.Service{
		Store:  st,
		Authz:  &authz.Evaluator{Policies: st},
		Engine: &stubRedisEngine{err: startErr},
	}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	loc := memorystore.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/instances"
	body := `{"tier":"BASIC","memorySizeGb":1}`

	t.Setenv(compute.EnvNestedEngineFailClosed, "")
	req := httptest.NewRequest(http.MethodPost, base+"?instanceId=soft-redis", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("soft create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var softOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &softOp)
	if softOp["done"] != true {
		t.Fatalf("soft create expected done Operation: %#v", softOp)
	}
	soft, _ := softOp["response"].(map[string]any)
	if soft == nil || soft["state"] != "READY" {
		t.Fatalf("soft instance=%#v", softOp)
	}

	t.Setenv(compute.EnvNestedEngineFailClosed, "1")
	req = httptest.NewRequest(http.MethodPost, base+"?instanceId=hard-redis", bytes.NewReader([]byte(body)))
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
	if !strings.Contains(msg, "nested engine start failed") || !strings.Contains(msg, "redis start refused") {
		t.Fatalf("message=%q", msg)
	}
	_, ok, err := st.GetMemorystoreRedisInstance("projects/noctaxris-gcp-local/locations/" + loc + "/instances/hard-redis")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("fail-closed create should roll back store row")
	}
}

func TestMemorystoreAuthzFailClosed(t *testing.T) {
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
	svc := &memorystore.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/locations/us-central1/instances", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMemorystoreRedisAuthFields(t *testing.T) {
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

	eng := &stubRedisEngine{}
	mux := http.NewServeMux()
	svc := &memorystore.Service{
		Store:  st,
		Authz:  &authz.Evaluator{Policies: st},
		Engine: eng,
	}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	loc := memorystore.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/instances"

	body := `{"tier":"BASIC","memorySizeGb":1,"authEnabled":true,"authString":"lab-redis-secret"}`
	req := httptest.NewRequest(http.MethodPost, base+"?instanceId=auth-redis", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &createOp)
	if createOp["done"] != true {
		t.Fatalf("create expected done Operation: %#v", createOp)
	}
	inst, _ := createOp["response"].(map[string]any)
	if inst == nil {
		t.Fatalf("create missing response: %#v", createOp)
	}
	if inst["authEnabled"] != true {
		t.Fatalf("authEnabled=%v", inst["authEnabled"])
	}
	if inst["authString"] != "lab-redis-secret" {
		t.Fatalf("authString=%v", inst["authString"])
	}
	if eng.lastAuthPass != "lab-redis-secret" {
		t.Fatalf("nested auth password=%q", eng.lastAuthPass)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/auth-redis", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if inst["authEnabled"] != true || inst["authString"] != "lab-redis-secret" {
		t.Fatalf("get auth fields=%#v", inst)
	}

	body = `{"tier":"BASIC","memorySizeGb":1,"authEnabled":true}`
	req = httptest.NewRequest(http.MethodPost, base+"?instanceId=auth-gen", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gen create status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &createOp)
	inst, _ = createOp["response"].(map[string]any)
	if inst == nil {
		t.Fatalf("gen create missing response: %#v", createOp)
	}
	gen, _ := inst["authString"].(string)
	if gen == "" || len(gen) < 8 {
		t.Fatalf("expected generated authString, got %q", gen)
	}
	if eng.lastAuthPass != gen {
		t.Fatalf("nested pass=%q want %q", eng.lastAuthPass, gen)
	}

	body = `{"tier":"BASIC","memorySizeGb":1}`
	req = httptest.NewRequest(http.MethodPost, base+"?instanceId=no-auth", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("no-auth create status=%d body=%s", rec.Code, rec.Body.String())
	}
	createOp = map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &createOp)
	inst, _ = createOp["response"].(map[string]any)
	if inst == nil {
		t.Fatalf("no-auth missing response: %#v", createOp)
	}
	if inst["authEnabled"] != false {
		t.Fatalf("default authEnabled=%v", inst["authEnabled"])
	}
	if _, ok := inst["authString"]; ok {
		t.Fatalf("authString should be omitted when disabled: %#v", inst)
	}
	if eng.lastAuthPass != "" {
		t.Fatalf("nested should get empty password, got %q", eng.lastAuthPass)
	}
}

func TestMemorystoreNilAuthzFailClosed(t *testing.T) {
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
	svc := &memorystore.Service{Store: st, Authz: nil}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/locations/us-central1/instances", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nil Authz expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
