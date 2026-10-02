package restlab_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type memPolicies map[string][]byte

func (m memPolicies) GetIAMPolicyJSON(resource string) ([]byte, bool, error) {
	b, ok := m[resource]
	return b, ok, nil
}

func mustPolicy(t *testing.T, role, member string) []byte {
	t.Helper()
	b, err := json.Marshal(authz.Policy{Bindings: []authz.Binding{{Role: role, Members: []string{member}}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWrapUnauthenticatedAndOK(t *testing.T) {
	h := restlab.Wrap(func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	}, func(http.ResponseWriter, *http.Request, authn.Principal) {})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}

	called := false
	h = restlab.Wrap(func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "sa@p.iam.gserviceaccount.com", IsRoot: true}, true
	}, func(http.ResponseWriter, *http.Request, authn.Principal) { called = true })
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("handler not called")
	}
}

func TestHandleFuncOnceIdempotent(t *testing.T) {
	mux := http.NewServeMux()
	var n int
	restlab.HandleFuncOnce(mux, "GET /lab/once", func(http.ResponseWriter, *http.Request) { n++ })
	restlab.HandleFuncOnce(mux, "GET /lab/once", func(http.ResponseWriter, *http.Request) { n += 10 })
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/lab/once", nil))
	if n != 1 {
		t.Fatalf("n=%d want 1", n)
	}
}

func TestEvaluateRequireAndWriteAuthzErr(t *testing.T) {
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	ev := &authz.Evaluator{Policies: memPolicies{
		"projects/p": mustPolicy(t, "roles/viewer", "serviceAccount:"+email),
	}}
	p := authn.Principal{Email: email}
	if err := restlab.Require(ev, p, "resourcemanager.projects.get", "p"); err != nil {
		t.Fatal(err)
	}
	if err := restlab.Require(ev, p, "storage.buckets.create", "p"); !errors.Is(err, restlab.ErrDenied) {
		t.Fatalf("want ErrDenied got %v", err)
	}

	rec := httptest.NewRecorder()
	restlab.WriteAuthzErr(rec, restlab.ErrDenied)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	restlab.WriteAuthzErr(rec, errors.New("boom"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	restlab.WriteJSON(rec, http.StatusCreated, map[string]any{"ok": true})
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("json=%s", rec.Body.String())
	}
}

type suStore struct {
	enabled bool
	err     error
}

func (s suStore) IsServiceEnabled(string, string) (bool, error) {
	return s.enabled, s.err
}

func TestRequireServiceEnabledBranches(t *testing.T) {
	rec := httptest.NewRecorder()
	if !restlab.RequireServiceEnabled(rec, suStore{enabled: true}, "p", "run.googleapis.com") {
		t.Fatal("enabled must continue")
	}
	rec = httptest.NewRecorder()
	if restlab.RequireServiceEnabled(rec, suStore{enabled: false}, "p", "run.googleapis.com") {
		t.Fatal("disabled must stop")
	}
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "disabled") {
		t.Fatalf("body=%s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	if restlab.RequireServiceEnabled(rec, suStore{err: errors.New("lookup")}, "p", "run.googleapis.com") {
		t.Fatal("lookup error must stop")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", rec.Code)
	}
	if msg := restlab.ServiceDisabledMessage("run.googleapis.com"); !strings.Contains(msg, "run.googleapis.com") {
		t.Fatalf("msg=%s", msg)
	}
	if err := restlab.CheckServiceEnabled(suStore{enabled: false}, "p", "run.googleapis.com"); !errors.Is(err, store.ErrServiceDisabled) {
		t.Fatalf("err=%v", err)
	}
}

type vpcStore struct {
	deny error
}

func (v vpcStore) VPCSCDenyCrossPerimeter(_, _, _ string) error { return v.deny }

func (v vpcStore) ProjectIDFromPrincipalEmail(string) (string, error) {
	return "from-proj", nil
}

func TestRequireVPCSCBranches(t *testing.T) {
	if err := restlab.CheckVPCSC(nil, "a", "b", "run.googleapis.com"); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if !restlab.RequireVPCSC(rec, vpcStore{}, "a", "b", "run.googleapis.com") {
		t.Fatal("allow must continue")
	}
	rec = httptest.NewRecorder()
	if restlab.RequireVPCSC(rec, vpcStore{deny: store.ErrVPCSCPerimeter}, "a", "b", "run.googleapis.com") {
		t.Fatal("perimeter deny must stop")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	if restlab.RequireVPCSC(rec, vpcStore{deny: errors.New("boom")}, "a", "b", "run.googleapis.com") {
		t.Fatal("other error must stop")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", rec.Code)
	}

	root := authn.Principal{IsRoot: true, Email: "root@x"}
	rec = httptest.NewRecorder()
	if !restlab.RequireVPCSCPrincipal(rec, vpcStore{deny: store.ErrVPCSCPerimeter}, root, "to", "run.googleapis.com") {
		t.Fatal("root must skip")
	}
}
