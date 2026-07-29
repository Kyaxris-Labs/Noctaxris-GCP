package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func openOrgPolicyStore(t *testing.T) *store.Store {
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
	return st
}

func TestOrgPolicySetGetListDelete(t *testing.T) {
	st := openOrgPolicyStore(t)
	parent := "projects/noctaxris-gcp-local"
	constraint := store.ConstraintDisableServiceAccountKeyCreation

	p, err := st.SetOrgPolicy(parent, constraint, `{"rules":[{"enforce":true}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != parent+"/policies/"+constraint {
		t.Fatalf("name: %q", p.Name)
	}

	got, ok, err := st.GetOrgPolicy(parent, constraint)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.SpecJSON == "" {
		t.Fatal("empty spec")
	}

	list, err := st.ListOrgPolicies(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len=%d", len(list))
	}

	deleted, err := st.DeleteOrgPolicy(parent, constraint)
	if err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	_, ok, err = st.GetOrgPolicy(parent, constraint)
	if err != nil || ok {
		t.Fatalf("after delete ok=%v err=%v", ok, err)
	}
}

func TestOrgPolicyConstraintEnforcedAncestry(t *testing.T) {
	st := openOrgPolicyStore(t)
	const project = "noctaxris-gcp-local"
	constraint := store.ConstraintDisableServiceAccountKeyCreation

	enforced, err := st.IsOrgPolicyConstraintEnforced("projects/"+project, constraint)
	if err != nil {
		t.Fatal(err)
	}
	if enforced {
		t.Fatal("default must not enforce")
	}

	if _, err := st.SetOrgPolicy(store.DefaultOrganizationName, constraint, `{"rules":[{"enforce":true}]}`); err != nil {
		t.Fatal(err)
	}
	enforced, err = st.IsOrgPolicyConstraintEnforced("projects/"+project, constraint)
	if err != nil {
		t.Fatal(err)
	}
	if !enforced {
		t.Fatal("org enforce should apply to project via CRMParent")
	}

	if _, err := st.SetOrgPolicy("projects/"+project, constraint, `{"rules":[{"enforce":false}]}`); err != nil {
		t.Fatal(err)
	}
	enforced, err = st.IsOrgPolicyConstraintEnforced("projects/"+project, constraint)
	if err != nil {
		t.Fatal(err)
	}
	if enforced {
		t.Fatal("nearest project policy must override org")
	}
}

func TestOrgPolicyUnknownConstraintRejected(t *testing.T) {
	st := openOrgPolicyStore(t)
	_, err := st.SetOrgPolicy("projects/noctaxris-gcp-local", "compute.unknownConstraint", `{"rules":[{"enforce":true}]}`)
	if err == nil {
		t.Fatal("expected unknown constraint error")
	}
}
