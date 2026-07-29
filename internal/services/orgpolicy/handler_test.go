package orgpolicy_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/orgpolicy"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type harness struct {
	mux   *http.ServeMux
	store *store.Store
	who   authn.Principal
}

func open(t *testing.T) *harness {
	t.Helper()
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
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	h := &harness{store: st, who: authn.Principal{Email: root, IsRoot: true}}
	mux := http.NewServeMux()
	(&orgpolicy.Handler{
		Store: st,
		Authz: &authz.Evaluator{Policies: st, Roles: st, Parents: st},
		Principal: func(r *http.Request) (authn.Principal, bool) {
			if h.who.Email == "" && !h.who.IsRoot {
				return authn.Principal{}, false
			}
			return h.who, true
		},
	}).Mount(mux)
	h.mux = mux
	return h
}

func TestOrgPolicySetListGetEffective(t *testing.T) {
	h := open(t)
	const project = "noctaxris-gcp-local"
	constraint := store.ConstraintDisableServiceAccountKeyCreation
	body := `{"name":"projects/` + project + `/policies/` + constraint + `","spec":{"rules":[{"enforce":true}]}}`

	req := httptest.NewRequest(http.MethodPost, "/v2/projects/"+project+"/policies?constraint="+constraint, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/projects/"+project+"/policies", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}
	var list struct {
		Policies []map[string]any `json:"policies"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Policies) != 1 {
		t.Fatalf("policies=%d", len(list.Policies))
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/projects/"+project+"/policies/"+constraint+":getEffectivePolicy", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("effective status=%d body=%s", rec.Code, rec.Body.String())
	}
	var eff map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &eff); err != nil {
		t.Fatal(err)
	}
	spec, _ := eff["spec"].(map[string]any)
	rules, _ := spec["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("rules=%v", rules)
	}
	rule, _ := rules[0].(map[string]any)
	if rule["enforce"] != true {
		t.Fatalf("enforce=%v", rule["enforce"])
	}
}

func TestOrgPolicyListConstraints(t *testing.T) {
	h := open(t)
	req := httptest.NewRequest(http.MethodGet, "/v2/organizations/"+store.DefaultOrganizationID+"/constraints", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Constraints []map[string]any `json:"constraints"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Constraints) < 2 {
		t.Fatalf("constraints=%d", len(out.Constraints))
	}
}

func TestOrgPolicyUnauthenticated(t *testing.T) {
	h := open(t)
	h.who = authn.Principal{}
	req := httptest.NewRequest(http.MethodGet, "/v2/projects/noctaxris-gcp-local/policies", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}
