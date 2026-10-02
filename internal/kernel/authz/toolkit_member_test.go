package authz_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

func TestToolkitPrincipalDoesNotMatchAllAuthenticatedUsers(t *testing.T) {
	const resource = "projects/noctaxris-gcp-local"
	e := &authz.Evaluator{Policies: memPolicies{
		resource: mustPolicy(t, "roles/viewer", "allAuthenticatedUsers"),
	}}
	ok, err := e.Evaluate("user:toolkit-uid", false, "resourcemanager.projects.get", resource)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("toolkit principal must not satisfy allAuthenticatedUsers")
	}
	ok, err = e.Evaluate("sa@noctaxris-gcp-local.iam.gserviceaccount.com", false, "resourcemanager.projects.get", resource)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("service account should satisfy allAuthenticatedUsers")
	}
}

func TestToolkitPrincipalExactMemberBinding(t *testing.T) {
	const resource = "projects/noctaxris-gcp-local"
	e := &authz.Evaluator{Policies: memPolicies{
		resource: mustPolicy(t, "roles/viewer", "user:toolkit-uid"),
	}}
	ok, err := e.Evaluate("user:toolkit-uid", false, "resourcemanager.projects.get", resource)
	if err != nil || !ok {
		t.Fatalf("exact toolkit binding must allow: ok=%v err=%v", ok, err)
	}
}
