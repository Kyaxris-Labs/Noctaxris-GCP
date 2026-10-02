package authz_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

func TestEvaluateAllUsersAndAllowPrincipalOrAllUsers(t *testing.T) {
	const resource = "projects/noctaxris-gcp-local/buckets/public"
	e := &authz.Evaluator{Policies: memPolicies{
		resource: mustPolicy(t, "roles/storage.objectViewer", "allUsers"),
	}}

	ok, err := e.EvaluateAllUsers("storage.objects.get", resource)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("allUsers should allow storage.objects.get")
	}
	ok, err = e.EvaluateAllUsers("storage.objects.create", resource)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("allUsers objectViewer must not grant create")
	}
	ok, err = e.EvaluateAllUsersAny("storage.objects.get", "", resource)
	if err != nil || !ok {
		t.Fatalf("EvaluateAllUsersAny ok=%v err=%v", ok, err)
	}

	ok, err = e.AllowPrincipalOrAllUsers("", false, false, "storage.objects.get", resource)
	if err != nil || !ok {
		t.Fatalf("anonymous allUsers allow ok=%v err=%v", ok, err)
	}
	// Authenticated non-toolkit principals inherit allUsers via memberIn.
	ok, err = e.AllowPrincipalOrAllUsers("lab@example.com", false, true, "storage.objects.get", resource)
	if err != nil || !ok {
		t.Fatalf("authenticated principal should inherit allUsers ok=%v err=%v", ok, err)
	}
	// Toolkit user: principals must not inherit allUsers.
	ok, err = e.AllowPrincipalOrAllUsers("user:toolkit-uid", false, true, "storage.objects.get", resource)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("toolkit principal must not inherit allUsers")
	}
	ok, err = e.AllowPrincipalOrAllUsers("root@noctaxris-gcp-local.iam.gserviceaccount.com", true, true, "storage.objects.get", resource)
	if err != nil || !ok {
		t.Fatalf("root principal allow ok=%v err=%v", ok, err)
	}

	nilEval := (*authz.Evaluator)(nil)
	ok, err = nilEval.EvaluateAllUsers("storage.objects.get", resource)
	if err != nil || ok {
		t.Fatalf("nil evaluator ok=%v err=%v", ok, err)
	}
	ok, err = nilEval.EvaluateAllUsersAny("storage.objects.get", resource)
	if err != nil || ok {
		t.Fatalf("nil EvaluateAllUsersAny ok=%v err=%v", ok, err)
	}
}
