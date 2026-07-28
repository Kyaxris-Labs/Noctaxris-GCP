package authz_test

import (
	"encoding/json"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

type memPolicies map[string][]byte

func (m memPolicies) GetIAMPolicyJSON(resource string) ([]byte, bool, error) {
	b, ok := m[resource]
	return b, ok, nil
}

func mustPolicy(t *testing.T, role, member string) []byte {
	t.Helper()
	p := authz.Policy{
		Bindings: []authz.Binding{{Role: role, Members: []string{member}}},
		Etag:     "etag1",
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEvaluateRootBypass(t *testing.T) {
	e := &authz.Evaluator{Policies: memPolicies{}}
	ok, err := e.Evaluate("root@noctaxris-gcp-local.iam.gserviceaccount.com", true, "storage.buckets.create", "projects/noctaxris-gcp-local")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("root must bypass")
	}
}

func TestEvaluateAllowDeny(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, "roles/owner", "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, "storage.buckets.create", resource)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("owner binding should allow")
	}
	ok, err = e.Evaluate("other@noctaxris-gcp-local.iam.gserviceaccount.com", false, "storage.buckets.create", resource)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("unbound principal should deny")
	}
}

func TestEvaluateDenyByDefaultMissingPolicy(t *testing.T) {
	e := &authz.Evaluator{Policies: memPolicies{}}
	ok, err := e.Evaluate("sa@noctaxris-gcp-local.iam.gserviceaccount.com", false, "storage.buckets.create", "projects/missing")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("missing policy must deny")
	}
}

func TestTestIamPermissions(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "viewer@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, "roles/viewer", "serviceAccount:"+email),
		},
	}
	got, err := e.TestIamPermissions(email, false, resource, []string{
		"resourcemanager.projects.get",
		"storage.buckets.create",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "resourcemanager.projects.get" {
		t.Fatalf("got %v", got)
	}
}
